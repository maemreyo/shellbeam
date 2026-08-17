package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type countingMediaResolver struct{ calls atomic.Int32 }

func (r *countingMediaResolver) ResolveAddress(context.Context, workspace.Address) (workspace.ResolvedAddress, error) {
	r.calls.Add(1)
	return workspace.ResolvedAddress{WorkspaceID: "ws_01K00000000000000000000000", CWD: "/resolved"}, nil
}

func TestMediaAdmissionPrecedesResolutionAndReaderEntry(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	reader := &fakeMediaReader{entered: entered, release: release, file: testMediaFile()}
	resolver := &countingMediaResolver{}
	svc := NewServiceWithWorkspaceResolver(nil, nil, resolver, Options{MediaReader: reader})
	req := MediaRequest{WorkspaceID: "ws_01K00000000000000000000000", Path: "a.png"}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := svc.ReadMedia(context.Background(), req); done <- err }()
	}
	<-entered
	<-entered
	if resolver.calls.Load() != 2 || reader.calls.Load() != 2 {
		t.Fatalf("resolver=%d reader=%d", resolver.calls.Load(), reader.calls.Load())
	}
	if _, err := svc.ReadMedia(context.Background(), req); !errors.Is(err, failure.CapacityExceeded) {
		t.Fatalf("third err=%v", err)
	}
	if resolver.calls.Load() != 2 || reader.calls.Load() != 2 {
		t.Fatalf("capacity refusal crossed admission: resolver=%d reader=%d", resolver.calls.Load(), reader.calls.Load())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMediaTimeoutDoesNotReleaseSlotUntilBlockedWorkersActuallyReturn(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	reader := &fakeMediaReader{entered: entered, release: release, file: testMediaFile()}
	svc := NewService(nil, nil, Options{MediaReader: reader})
	svc.mediaWorkerDone = make(chan struct{}, 2)
	ticks := make(chan time.Time, 2)
	svc.mediaAfter = func(time.Duration) <-chan time.Time { return ticks }
	done := make(chan error, 2)
	for _, p := range []string{"a.png", "b.png"} {
		p := p
		go func() { _, err := svc.ReadMedia(context.Background(), MediaRequest{CWD: "/tmp", Path: p}); done <- err }()
	}
	<-entered
	<-entered
	ticks <- time.Now()
	ticks <- time.Now()
	for i := 0; i < 2; i++ {
		if err := <-done; !errors.Is(err, failure.MediaReadTimeout) {
			t.Fatalf("timeout err=%v", err)
		}
	}
	if _, err := svc.ReadMedia(context.Background(), MediaRequest{CWD: "/tmp", Path: "c.png"}); !errors.Is(err, failure.CapacityExceeded) {
		t.Fatalf("timed-out workers released slots early: %v", err)
	}
	if reader.calls.Load() != 2 {
		t.Fatalf("replacement worker started: %d", reader.calls.Load())
	}
	close(release)
	// Deterministically wait for worker-owned slot releases via service hook channel.
	for i := 0; i < 2; i++ {
		<-svc.mediaWorkerDone
	}
	if _, err := svc.ReadMedia(context.Background(), MediaRequest{CWD: "/tmp", Path: "d.png"}); err != nil {
		t.Fatalf("slot not reusable after worker return: %v", err)
	}
}

func TestMediaCapacityFailureIsRetryableButTimeoutIsNot(t *testing.T) {
	capFailure := failure.Public(failure.New(failure.CapacityExceeded, map[string]string{"active": "2", "limit": "2"}, nil))
	if !capFailure.Retryable {
		t.Fatal("capacity must be retryable")
	}
	timeout := failure.Public(failure.New(failure.MediaReadTimeout, nil, nil))
	if timeout.Retryable {
		t.Fatal("media timeout must be non-retryable")
	}
	_ = media.MaxConcurrentReads
}
