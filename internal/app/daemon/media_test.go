package daemon

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeMediaResolver struct {
	calls atomic.Int32
	base  string
	err   error
}

func (r *fakeMediaResolver) ResolveAddress(context.Context, workspace.Address) (workspace.ResolvedAddress, error) {
	r.calls.Add(1)
	if r.err != nil {
		return workspace.ResolvedAddress{}, r.err
	}
	return workspace.ResolvedAddress{WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000"), LogicalCWD: ".", CWD: r.base}, nil
}

type fakeMediaReader struct {
	calls          atomic.Int32
	mu             sync.Mutex
	bases          []string
	paths          []media.LogicalPath
	file           media.File
	err            error
	entered        chan struct{}
	release        <-chan struct{}
	observeContext bool
}

func (r *fakeMediaReader) Read(ctx context.Context, base string, path media.LogicalPath, _ media.Limits) (media.File, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.bases = append(r.bases, base)
	r.paths = append(r.paths, path)
	r.mu.Unlock()
	if r.entered != nil {
		r.entered <- struct{}{}
	}
	if r.release != nil {
		if r.observeContext {
			select {
			case <-r.release:
			case <-ctx.Done():
				return media.File{}, ctx.Err()
			}
		} else {
			<-r.release
		}
	}
	if r.err != nil {
		return media.File{}, r.err
	}
	return r.file, nil
}

func testMediaFile() media.File {
	return media.File{MIMEType: "image/png", Format: "png", Width: 2, Height: 3, Data: []byte{1, 2, 3, 4}}
}

func TestReadMediaWorkspaceAndCWDKeepExactDisplayAddress(t *testing.T) {
	reader := &fakeMediaReader{file: testMediaFile()}
	resolver := &fakeMediaResolver{base: "/resolved/worktree"}
	svc := NewServiceWithWorkspaceResolver(nil, nil, resolver, Options{MediaReader: reader})

	wsReq := MediaRequest{WorkspaceID: "ws_01K00000000000000000000000", Path: "artifacts/settings.png"}
	ws, err := svc.ReadMedia(context.Background(), wsReq)
	if err != nil {
		t.Fatal(err)
	}
	wantWS := media.DisplayAddress{AddressKind: media.AddressWorkspace, WorkspaceID: wsReq.WorkspaceID, Path: wsReq.Path}
	if ws.DisplayAddress != wantWS || ws.Kind != "media" || ws.SchemaVersion != 1 || ws.ByteSize != 4 {
		t.Fatalf("workspace result=%#v", ws)
	}
	if resolver.calls.Load() != 1 {
		t.Fatalf("resolver calls=%d", resolver.calls.Load())
	}
	reader.mu.Lock()
	if reader.bases[0] != "/resolved/worktree" || reader.paths[0].Raw != wsReq.Path {
		t.Fatalf("reader bases=%v paths=%#v", reader.bases, reader.paths)
	}
	reader.mu.Unlock()

	cwdReq := MediaRequest{CWD: "/tmp/../tmp", Path: "settings.png"}
	cwd, err := svc.ReadMedia(context.Background(), cwdReq)
	if err != nil {
		t.Fatal(err)
	}
	wantCWD := media.DisplayAddress{AddressKind: media.AddressCWD, CWD: cwdReq.CWD, Path: cwdReq.Path}
	if cwd.DisplayAddress != wantCWD {
		t.Fatalf("cwd result=%#v", cwd)
	}
	if resolver.calls.Load() != 1 {
		t.Fatalf("cwd unexpectedly used resolver: %d", resolver.calls.Load())
	}
	reader.mu.Lock()
	if reader.bases[1] != cwdReq.CWD {
		t.Fatalf("cwd normalized/substituted: %q", reader.bases[1])
	}
	reader.mu.Unlock()
}

func TestReadMediaRejectsInvalidOrUnavailableBeforeResolver(t *testing.T) {
	resolver := &fakeMediaResolver{base: "/resolved"}
	svc := NewServiceWithWorkspaceResolver(nil, nil, resolver, Options{})
	if _, err := svc.ReadMedia(context.Background(), MediaRequest{CWD: "/tmp", Path: "a.png"}); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("unavailable err=%v", err)
	}
	if resolver.calls.Load() != 0 {
		t.Fatalf("resolver calls=%d", resolver.calls.Load())
	}

	svc = NewServiceWithWorkspaceResolver(nil, nil, resolver, Options{MediaReader: &fakeMediaReader{file: testMediaFile()}})
	invalid := []MediaRequest{
		{Path: "a.png"},
		{WorkspaceID: "ws_01K00000000000000000000000", CWD: "/tmp", Path: "a.png"},
		{CWD: "/tmp", Path: "../a.png"},
	}
	for _, req := range invalid {
		if _, err := svc.ReadMedia(context.Background(), req); !errors.Is(err, failure.InvalidInput) {
			t.Fatalf("req=%#v err=%v", req, err)
		}
	}
	if resolver.calls.Load() != 0 {
		t.Fatalf("invalid input resolved workspace: %d", resolver.calls.Load())
	}
}

func TestReadMediaPropagatesResolverAndReaderFailures(t *testing.T) {
	resolver := &fakeMediaResolver{err: failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": "ws_01K00000000000000000000000"}, nil)}
	svc := NewServiceWithWorkspaceResolver(nil, nil, resolver, Options{MediaReader: &fakeMediaReader{file: testMediaFile()}})
	if _, err := svc.ReadMedia(context.Background(), MediaRequest{WorkspaceID: "ws_01K00000000000000000000000", Path: "a.png"}); !errors.Is(err, failure.WorkspaceNotFound) {
		t.Fatalf("resolver err=%v", err)
	}
	reader := &fakeMediaReader{err: failure.New(failure.MediaReadFailed, nil, nil)}
	svc = NewService(nil, nil, Options{MediaReader: reader})
	if _, err := svc.ReadMedia(context.Background(), MediaRequest{CWD: "/tmp", Path: "a.png"}); !errors.Is(err, failure.MediaReadFailed) {
		t.Fatalf("reader err=%v", err)
	}
}

func TestReadMediaCancellationReleasesSlotOnlyWhenWorkerReturns(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	reader := &fakeMediaReader{entered: entered, release: release, observeContext: true}
	svc := NewService(nil, nil, Options{MediaReader: reader})
	svc.mediaWorkerDone = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := svc.ReadMedia(ctx, MediaRequest{CWD: "/tmp", Path: "a.png"}); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	<-svc.mediaWorkerDone
	close(release)
	if _, err := svc.ReadMedia(context.Background(), MediaRequest{CWD: "/tmp", Path: "b.png"}); err != nil {
		t.Fatalf("slot not released after cooperative worker return: %v", err)
	}
}

func TestReadMediaResultCopiesReaderData(t *testing.T) {
	data := []byte{1, 2, 3}
	reader := &fakeMediaReader{file: media.File{MIMEType: "image/png", Format: "png", Width: 1, Height: 1, Data: data}}
	svc := NewService(nil, nil, Options{MediaReader: reader})
	got, err := svc.ReadMedia(context.Background(), MediaRequest{CWD: "/tmp", Path: "a.png"})
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 9
	if got.Data[0] != 1 {
		t.Fatal("result aliases reader buffer")
	}
}

func TestMediaReadBudgetDefaultsToFiveSeconds(t *testing.T) {
	svc := NewService(nil, nil, Options{MediaReader: &fakeMediaReader{file: testMediaFile()}})
	if svc.mediaReadBudget != media.AcquisitionBudget {
		t.Fatalf("budget=%s acquisition=%s", svc.mediaReadBudget, media.AcquisitionBudget)
	}
	if svc.mediaReadBudget != 5*time.Second {
		t.Fatalf("budget=%s", svc.mediaReadBudget)
	}
}
