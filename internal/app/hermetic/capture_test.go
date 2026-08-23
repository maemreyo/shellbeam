package hermetic

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeWorkspaceSource struct {
	contexts []WorkspaceContext
	err      error
	calls    []string
}

func (f *fakeWorkspaceSource) ResolveFresh(_ context.Context, workspaceID string) (WorkspaceContext, error) {
	f.calls = append(f.calls, workspaceID)
	if f.err != nil {
		return WorkspaceContext{}, f.err
	}
	if len(f.contexts) == 0 {
		return WorkspaceContext{}, errors.New("no workspace context")
	}
	got := f.contexts[0]
	if len(f.contexts) > 1 {
		f.contexts = f.contexts[1:]
	}
	return got, nil
}

type fakeCaptureProvider struct {
	view      CapturedView
	err       error
	requests  []ProviderCaptureRequest
	discarded []CapturedView
}

func (f *fakeCaptureProvider) Capture(_ context.Context, req ProviderCaptureRequest) (CapturedView, error) {
	f.requests = append(f.requests, req)
	return f.view, f.err
}

func (f *fakeCaptureProvider) Discard(_ context.Context, view CapturedView) error {
	f.discarded = append(f.discarded, view)
	return nil
}

func TestCaptureServiceBindsOneFreshGenerationAndCanonicalScope(t *testing.T) {
	ctx := validWorkspaceContext("a")
	workspace := &fakeWorkspaceSource{contexts: []WorkspaceContext{ctx, ctx}}
	provider := &fakeCaptureProvider{view: validCapturedView(t, ctx, []string{"go.mod", "internal/**"})}
	svc := NewCaptureService(workspace, provider, core.DefaultCaptureLimits())
	req := validBoundaryRequest([]string{"internal/**", "go.mod"})

	got, err := svc.Capture(context.Background(), string(ctx.WorkspaceID), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 || !reflect.DeepEqual(provider.requests[0].Selectors, []string{"go.mod", "internal/**"}) {
		t.Fatalf("provider requests=%#v", provider.requests)
	}
	if provider.requests[0].SourceGeneration != ctx.SourceGeneration || provider.requests[0].Root != ctx.Root {
		t.Fatalf("provider request lost source binding: %#v", provider.requests[0])
	}
	if len(workspace.calls) != 2 || len(provider.discarded) != 0 || got.Manifest.SourceGeneration != ctx.SourceGeneration {
		t.Fatalf("workspace=%v discarded=%d got=%#v", workspace.calls, len(provider.discarded), got)
	}
}

func TestCaptureServiceRejectsGenerationChangeAndDiscardsView(t *testing.T) {
	before := validWorkspaceContext("a")
	after := validWorkspaceContext("b")
	workspace := &fakeWorkspaceSource{contexts: []WorkspaceContext{before, after}}
	provider := &fakeCaptureProvider{view: validCapturedView(t, before, []string{"go.mod"})}
	svc := NewCaptureService(workspace, provider, core.DefaultCaptureLimits())

	if _, err := svc.Capture(context.Background(), string(before.WorkspaceID), validBoundaryRequest([]string{"go.mod"})); err == nil {
		t.Fatal("capture accepted source generation change")
	}
	if len(provider.discarded) != 1 {
		t.Fatalf("discarded=%d want 1", len(provider.discarded))
	}
}

func TestCaptureServiceRejectsProviderOverclaimAndDiscardsView(t *testing.T) {
	ctx := validWorkspaceContext("a")
	cases := []struct {
		name   string
		mutate func(*CapturedView)
	}{
		{"generation", func(v *CapturedView) { v.Manifest.SourceGeneration = "gen_" + strings.Repeat("b", 64) }},
		{"workspace", func(v *CapturedView) {
			v.Manifest.WorkspaceID = workspacecore.WorkspaceID("ws_01K00000000000000000000099")
		}},
		{"selectors", func(v *CapturedView) {
			v.Manifest.Selectors = []string{"other.txt"}
			v.Manifest.Entries[0].Path = "other.txt"
		}},
		{"private_root_relative", func(v *CapturedView) { v.PrivateRoot = "relative" }},
		{"capture_id_unsafe", func(v *CapturedView) { v.CaptureID = "../escape" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := validCapturedView(t, ctx, []string{"go.mod"})
			tc.mutate(&view)
			workspace := &fakeWorkspaceSource{contexts: []WorkspaceContext{ctx, ctx}}
			provider := &fakeCaptureProvider{view: view}
			svc := NewCaptureService(workspace, provider, core.DefaultCaptureLimits())
			if _, err := svc.Capture(context.Background(), string(ctx.WorkspaceID), validBoundaryRequest([]string{"go.mod"})); err == nil {
				t.Fatal("provider overclaim accepted")
			}
			if len(provider.discarded) != 1 {
				t.Fatalf("discarded=%d", len(provider.discarded))
			}
		})
	}
}

func TestCaptureServiceRejectsUnsafeWorkspaceBeforeProviderWork(t *testing.T) {
	ctx := validWorkspaceContext("a")
	ctx.Root = "relative"
	workspace := &fakeWorkspaceSource{contexts: []WorkspaceContext{ctx}}
	provider := &fakeCaptureProvider{}
	svc := NewCaptureService(workspace, provider, core.DefaultCaptureLimits())
	if _, err := svc.Capture(context.Background(), string(ctx.WorkspaceID), validBoundaryRequest([]string{"go.mod"})); err == nil {
		t.Fatal("unsafe workspace binding accepted")
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider called before safe workspace binding: %#v", provider.requests)
	}
}

func validWorkspaceContext(hexDigit string) WorkspaceContext {
	return WorkspaceContext{
		WorkspaceID:  workspacecore.WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID: workspacecore.RepositoryID("repo_01K00000000000000000000000"),
		Root:         "/repo", SourceGeneration: "gen_" + strings.Repeat(hexDigit, 64),
	}
}

func validBoundaryRequest(selectors []string) core.Request {
	return core.Request{
		Version: core.RequestVersionV1, Mode: core.ModeRequired, RepoInputs: selectors,
		Network: core.NetworkOff, Environment: core.EnvironmentFixedAllowlist,
		Stdin: core.StdinClosed, Writes: core.WritesEphemeralDiscard,
	}
}

func validCapturedView(t *testing.T, ctx WorkspaceContext, selectors []string) CapturedView {
	t.Helper()
	canonical, err := validBoundaryRequest(selectors).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	entries := []core.CaptureEntry{{Path: "go.mod", Size: 1, SHA256: strings.Repeat("c", 64)}}
	if len(canonical.RepoInputs) == 2 {
		entries = append(entries, core.CaptureEntry{Path: "internal/x.go", Size: 2, SHA256: strings.Repeat("d", 64)})
	}
	manifest := core.CaptureManifest{
		SchemaVersion: core.CaptureManifestSchemaVersion, WorkspaceID: ctx.WorkspaceID,
		SourceGeneration: ctx.SourceGeneration, Selectors: canonical.RepoInputs, Entries: entries, TotalBytes: 1,
	}
	if len(entries) == 2 {
		manifest.TotalBytes = 3
	}
	manifest, err = manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return CapturedView{CaptureID: "hcap_01K00000000000000000000000", PrivateRoot: filepath.Join("/private", "hcap_01K00000000000000000000000"), Manifest: manifest}
}

func TestCaptureServiceDiscardDelegatesOnlyValidatedOwnedView(t *testing.T) {
	ctx := validWorkspaceContext("a")
	view := validCapturedView(t, ctx, []string{"go.mod"})
	provider := &fakeCaptureProvider{}
	svc := NewCaptureService(&fakeWorkspaceSource{}, provider, core.DefaultCaptureLimits())
	if err := svc.Discard(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	if len(provider.discarded) != 1 || provider.discarded[0].CaptureID != view.CaptureID {
		t.Fatalf("discarded=%#v", provider.discarded)
	}
	unsafe := view
	unsafe.CaptureID = "../escape"
	if err := svc.Discard(context.Background(), unsafe); err == nil {
		t.Fatal("unsafe capture discard accepted")
	}
	if len(provider.discarded) != 1 {
		t.Fatalf("unsafe view reached provider: %#v", provider.discarded)
	}
}
