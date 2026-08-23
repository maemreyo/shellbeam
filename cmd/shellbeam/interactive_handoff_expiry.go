//go:build linux || darwin

package main

import (
	"context"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

const (
	handoffPendingTTL            = 15 * time.Minute
	handoffExpirySweepBound      = 64
	handoffExpirySweepInterval   = 30 * time.Second
	handoffExpiryBacklogInterval = time.Second
)

type handoffExpiryStore interface {
	ListExpiredHandoffs(context.Context, time.Time, int) ([]string, bool, error)
}

type handoffExpiryService interface {
	ExpireHandoff(context.Context, string) (handoff.State, error)
}

func sweepHandoffExpiry(ctx context.Context, store handoffExpiryStore, svc handoffExpiryService, now time.Time) bool {
	ids, more, err := store.ListExpiredHandoffs(ctx, now.UTC().Add(-handoffPendingTTL), handoffExpirySweepBound)
	if err != nil {
		return false
	}
	failed := false
	for _, id := range ids {
		if _, err := svc.ExpireHandoff(ctx, id); err != nil {
			failed = true
		}
	}
	return more || failed
}

func startHandoffExpiry(ctx context.Context, store *storeadapter.Repository, svc *daemonapp.Service) {
	if store == nil || svc == nil {
		return
	}
	catalog := svc.CapabilityCatalog()
	if catalog.Features[capability.FeatureInteractiveHandoff] != capability.Available || catalog.InteractiveHandoff == nil {
		return
	}
	go func() {
		for {
			remaining := sweepHandoffExpiry(ctx, store, svc, time.Now().UTC())
			delay := handoffExpirySweepInterval
			if remaining {
				delay = handoffExpiryBacklogInterval
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
		}
	}()
}
