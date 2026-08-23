package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestContextExecLeasePersistsOpaqueExactIdentityAndReleases(t *testing.T) {
	r, binding, _, _ := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "context-lease")
	fingerprint := strings.Repeat("a", 64)
	lease, created, result := r.AcquireContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch, "ctxexec_lease_one", fingerprint)
	if result.Err != nil || !created {
		t.Fatalf("acquire lease=%#v created=%v result=%#v", lease, created, result)
	}
	if err := lease.Validate(); err != nil {
		t.Fatal(err)
	}
	path := r.contextExecLeasePath(operation.SessionID(binding.SessionID), binding.AuthorityEpoch)
	if strings.Contains(path, binding.SessionID) {
		t.Fatalf("lease path leaked raw session id: %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lease path missing: %v", err)
	}
	replay, created, result := r.AcquireContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch, lease.ContextExecID, fingerprint)
	if result.Err != nil || created || replay != lease {
		t.Fatalf("lease replay=%#v created=%v result=%#v", replay, created, result)
	}
	if _, _, result := r.AcquireContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch, "ctxexec_lease_other", fingerprint); !errors.Is(result.Err, failure.OperationConflict) {
		t.Fatalf("changed lease err=%v want operation_conflict", result.Err)
	}
	found, ok, err := r.FindContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch)
	if err != nil || !ok || found != lease {
		t.Fatalf("find lease=%#v found=%v err=%v", found, ok, err)
	}
	if result := r.ReleaseContextExecLease(context.Background(), lease); result.Err != nil {
		t.Fatalf("release=%#v", result)
	}
	if _, ok, err := r.FindContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch); err != nil || ok {
		t.Fatalf("lease survived release found=%v err=%v", ok, err)
	}
}

func TestHandoffReserveRefusesEpochRotationWhileContextExecLeaseExists(t *testing.T) {
	r, binding, req, initial := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "lease-block")
	fingerprint := strings.Repeat("b", 64)
	lease, created, result := r.AcquireContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch, "ctxexec_lease_block", fingerprint)
	if result.Err != nil || !created {
		t.Fatalf("acquire lease=%#v created=%v result=%#v", lease, created, result)
	}
	if _, _, result := r.ReserveHandoff(context.Background(), req, initial); !errors.Is(result.Err, failure.HandoffConflict) {
		t.Fatalf("handoff with active lease err=%v want handoff_conflict", result.Err)
	}
	current, err := r.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || current.AuthorityEpoch != binding.AuthorityEpoch || current.DesiredOwner != binding.DesiredOwner {
		t.Fatalf("binding rotated under lease: %#v err=%v", current, err)
	}
	found, ok, err := r.FindContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch)
	if err != nil || !ok || found != lease {
		t.Fatalf("lease lost after blocked handoff=%#v found=%v err=%v", found, ok, err)
	}
}

func TestContextExecLeaseAndHandoffEpochRotationHaveSingleAtomicWinner(t *testing.T) {
	for i := 0; i < 20; i++ {
		r, binding, req, initial := h2HandoffFixture(t, filepath.Join(t.TempDir(), "state"), "lease-race")
		fingerprint := strings.Repeat("c", 64)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var leaseCreated, handoffCreated bool
		var leaseErr, handoffErr error
		go func() {
			defer wg.Done()
			<-start
			_, created, result := r.AcquireContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch, "ctxexec_lease_race", fingerprint)
			leaseCreated, leaseErr = created, result.Err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, created, result := r.ReserveHandoff(context.Background(), req, initial)
			handoffCreated, handoffErr = created, result.Err
		}()
		close(start)
		wg.Wait()
		leaseWon := leaseCreated && leaseErr == nil
		handoffWon := handoffCreated && handoffErr == nil
		if leaseWon == handoffWon {
			t.Fatalf("iteration %d lease(created=%v err=%v) handoff(created=%v err=%v)", i, leaseCreated, leaseErr, handoffCreated, handoffErr)
		}
		current, err := r.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
		if err != nil {
			t.Fatal(err)
		}
		_, leaseFound, err := r.FindContextExecLease(context.Background(), operation.SessionID(binding.SessionID), binding.AuthorityEpoch)
		if err != nil {
			t.Fatal(err)
		}
		if leaseWon {
			if handoffErr == nil || current.AuthorityEpoch != binding.AuthorityEpoch || !leaseFound {
				t.Fatalf("lease winner did not fence handoff: current=%#v leaseFound=%v handoffErr=%v", current, leaseFound, handoffErr)
			}
		} else if leaseErr == nil || current.AuthorityEpoch != initial.AuthorityEpoch || leaseFound {
			t.Fatalf("handoff winner did not fence lease: current=%#v leaseFound=%v leaseErr=%v", current, leaseFound, leaseErr)
		}
	}
}
