package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCapturePartialStateResumesWithoutChangingFinalizedOpaqueRefs(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	mustPrivateDir(t, state)
	mustPrivateDir(t, runtime)
	provider := New(state, runtime)
	refs := []string{"entry_AAAAAAAAAAAAAAAAAAAAAAAAAA", "entry_BBBBBBBBBBBBBBBBBBBBBBBBBB", "entry_CCCCCCCCCCCCCCCCCCCCCCCCCC"}
	refCalls := 0
	provider.newEntryRef = func() string { ref := refs[refCalls]; refCalls++; return ref }
	provider.afterEntry = func(finalized int) error {
		if finalized == 1 {
			return errors.New("injected partial capture")
		}
		return nil
	}
	request := captureRequest(workspace, []string{"a.txt", "b.txt"})
	if _, err := provider.Capture(context.Background(), request); err == nil {
		t.Fatal("injected partial capture succeeded")
	}
	manifest, err := provider.loadManifest(request.CheckpointID)
	if err != nil || manifest.Complete || len(manifest.Entries) != 1 || manifest.Entries[0].OpaqueRef != refs[0] {
		t.Fatalf("partial manifest=%#v err=%v", manifest, err)
	}

	provider.afterEntry = nil
	result, err := provider.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if refCalls != 2 || !reflect.DeepEqual(result.OpaqueEntryRefs, refs[:2]) {
		t.Fatalf("resume refs=%v calls=%d", result.OpaqueEntryRefs, refCalls)
	}
	manifest, err = provider.loadManifest(request.CheckpointID)
	if err != nil || !manifest.Complete || len(manifest.Entries) != 2 {
		t.Fatalf("complete manifest=%#v err=%v", manifest, err)
	}
	if _, err := provider.Capture(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if refCalls != 2 {
		t.Fatalf("complete replay allocated new refs: %d", refCalls)
	}
}

func TestCaptureCompleteManifestIsImmutableAcrossChangedIntentOrRoot(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	mustPrivateDir(t, state)
	mustPrivateDir(t, runtime)
	provider := New(state, runtime)
	request := captureRequest(workspace, []string{"a.txt"})
	if _, err := provider.Capture(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	moved := request
	moved.Root = t.TempDir()
	if _, err := provider.Capture(context.Background(), moved); err == nil {
		t.Fatal("changed root recaptured completed checkpoint")
	}
	changedPaths := request
	changedPaths.Paths = []string{"b.txt"}
	if _, err := provider.Capture(context.Background(), changedPaths); err == nil {
		t.Fatal("changed paths recaptured completed checkpoint")
	}
	changedGeneration := request
	changedGeneration.SourceGeneration = "gen_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := provider.Capture(context.Background(), changedGeneration); err == nil {
		t.Fatal("changed source generation recaptured completed checkpoint")
	}
}
