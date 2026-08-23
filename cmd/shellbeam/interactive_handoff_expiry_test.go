//go:build linux || darwin

package main

import (
	"context"
	"testing"
	"time"

	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type handoffExpiryStoreFake struct {
	ids    []string
	more   bool
	cutoff time.Time
	limit  int
}

func (f *handoffExpiryStoreFake) ListExpiredHandoffs(_ context.Context, cutoff time.Time, limit int) ([]string, bool, error) {
	f.cutoff, f.limit = cutoff, limit
	return append([]string(nil), f.ids...), f.more, nil
}

type handoffExpiryServiceFake struct{ ids []string }

func (f *handoffExpiryServiceFake) ExpireHandoff(_ context.Context, id string) (handoff.State, error) {
	f.ids = append(f.ids, id)
	return handoff.State{HandoffID: id}, nil
}

func TestSweepHandoffExpiryUsesOneBoundedBatchAndCreatedLifetimeCutoff(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	store := &handoffExpiryStoreFake{ids: []string{"handoff-a", "handoff-b"}, more: true}
	svc := &handoffExpiryServiceFake{}
	remaining := sweepHandoffExpiry(t.Context(), store, svc, now)
	if !remaining {
		t.Fatal("backlog was not preserved")
	}
	if !store.cutoff.Equal(now.Add(-handoffPendingTTL)) || store.limit != handoffExpirySweepBound {
		t.Fatalf("cutoff=%s limit=%d", store.cutoff, store.limit)
	}
	if len(svc.ids) != 2 || svc.ids[0] != "handoff-a" || svc.ids[1] != "handoff-b" {
		t.Fatalf("expired ids=%v", svc.ids)
	}
}
