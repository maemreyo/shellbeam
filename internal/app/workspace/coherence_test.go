package workspace

import (
	"sync"
	"testing"
)

func TestCoherenceTrackerBeginEndAndInvalidate(t *testing.T) {
	tracker := NewCoherenceTracker("daemon-a")
	initial := tracker.CaptureBarrier()
	if initial.Epoch != 0 || initial.ActiveManagedShellOperations != 0 || initial.DaemonIncarnation != "daemon-a" {
		t.Fatalf("initial=%#v", initial)
	}

	lease := tracker.BeginManagedShell()
	begun := tracker.CaptureBarrier()
	if begun.Epoch != 1 || begun.ActiveManagedShellOperations != 1 {
		t.Fatalf("begun=%#v", begun)
	}

	tracker.Invalidate("explicit_source_mutation")
	invalidated := tracker.CaptureBarrier()
	if invalidated.Epoch != 2 || invalidated.ActiveManagedShellOperations != 1 {
		t.Fatalf("invalidated=%#v", invalidated)
	}

	lease.End()
	ended := tracker.CaptureBarrier()
	if ended.Epoch != 3 || ended.ActiveManagedShellOperations != 0 {
		t.Fatalf("ended=%#v", ended)
	}
	lease.End()
	if got := tracker.CaptureBarrier(); got != ended {
		t.Fatalf("second End changed barrier: before=%#v after=%#v", ended, got)
	}
}

func TestCoherenceTrackerLeaseEndIsExactlyOnceConcurrently(t *testing.T) {
	tracker := NewCoherenceTracker("daemon-a")
	lease := tracker.BeginManagedShell()
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease.End()
		}()
	}
	wg.Wait()
	got := tracker.CaptureBarrier()
	if got.Epoch != 2 || got.ActiveManagedShellOperations != 0 {
		t.Fatalf("barrier=%#v", got)
	}
}

func TestCoherenceTrackerConcurrentManagedShellsReturnToZero(t *testing.T) {
	tracker := NewCoherenceTracker("daemon-a")
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease := tracker.BeginManagedShell()
			lease.End()
		}()
	}
	wg.Wait()
	got := tracker.CaptureBarrier()
	if got.ActiveManagedShellOperations != 0 || got.Epoch != 200 {
		t.Fatalf("barrier=%#v", got)
	}
}

func TestCoherenceTrackerNewIncarnationDoesNotInheritLeases(t *testing.T) {
	old := NewCoherenceTracker("daemon-old")
	_ = old.BeginManagedShell()
	fresh := NewCoherenceTracker("daemon-new")
	got := fresh.CaptureBarrier()
	if got.DaemonIncarnation != "daemon-new" || got.Epoch != 0 || got.ActiveManagedShellOperations != 0 {
		t.Fatalf("fresh barrier=%#v", got)
	}
}
