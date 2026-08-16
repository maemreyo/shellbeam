package main

import (
	"context"
	"os"
	"time"

	supervisoradapter "github.com/maemreyo/shellbeam/internal/adapter/supervisor"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/config"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

type persistentBindingStoreAdapter struct {
	store daemonapp.PersistentSessionStore
}

func (a persistentBindingStoreAdapter) Find(ctx context.Context, sessionID operation.SessionID) (persistentcore.Binding, bool, error) {
	return a.store.FindPersistentBinding(ctx, sessionID)
}

func (a persistentBindingStoreAdapter) Reserve(ctx context.Context, binding persistentcore.Binding) (persistentcore.Binding, bool, error) {
	stored, created, result := a.store.ReservePersistentBinding(ctx, binding)
	return stored, created, result.Err
}

func (a persistentBindingStoreAdapter) Advance(ctx context.Context, binding persistentcore.Binding) error {
	return a.store.AdvancePersistentBinding(ctx, binding).Err
}

type daemonPersistentRuntime struct {
	service *persistentapp.Service
}

func (r daemonPersistentRuntime) Ensure(ctx context.Context, reservation operation.Reservation, spec operation.ExecutionSpec) (daemonapp.PersistentLaunch, error) {
	result, err := r.service.Ensure(ctx, reservation, spec)
	if err != nil {
		return daemonapp.PersistentLaunch{}, err
	}
	return daemonapp.PersistentLaunch{Handle: result.Attachment, Spawn: result.Status.Spawn, PID: result.Status.PID}, nil
}

func (r daemonPersistentRuntime) Reattach(ctx context.Context, binding persistentcore.Binding) (daemonapp.PersistentReattach, error) {
	result, err := r.service.Reattach(ctx, binding)
	if err != nil {
		return daemonapp.PersistentReattach{}, err
	}
	return daemonapp.PersistentReattach{Handle: result.Attachment, State: result.Status.State, Outcome: result.Status.Outcome, Spawn: result.Status.Spawn, PID: result.Status.PID}, nil
}

func composePersistentSessionRuntime(store daemonapp.PersistentSessionStore, runtimeRoot string, cfg config.Config) (daemonapp.PersistentRuntime, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	limits := persistentapp.Limits{
		MaxOutputBytes: cfg.MaxSessionOutputBytes, MaxQueuedInputBytes: cfg.MaxQueuedInputSessionBytes,
		MaxInputRecords: persistentcore.MaxInputRecords, MaxInputMetadataBytes: persistentcore.MaxInputRecordMetadataBytes,
		MaxKillRecords: persistentcore.MaxKillRecords, TerminationGrace: time.Duration(cfg.TerminationGraceMS) * time.Millisecond,
	}
	return newPersistentSessionRuntime(store, runtimeRoot, executable, limits)
}

func newPersistentSessionRuntime(store daemonapp.PersistentSessionStore, runtimeRoot, executable string, limits persistentapp.Limits) (daemonapp.PersistentRuntime, error) {
	launcher, err := supervisoradapter.NewLauncher(supervisoradapter.LauncherOptions{
		RuntimeRoot: runtimeRoot, Executable: executable,
		HandshakeTimeout: time.Duration(persistentcore.ReattachHandshakeTimeoutMS) * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}
	service := persistentapp.NewService(persistentBindingStoreAdapter{store: store}, launcher, persistentapp.Options{Limits: limits})
	return daemonPersistentRuntime{service: service}, nil
}

type persistentStartupStore interface {
	ListPersistentRecoveryCandidates(context.Context) ([]persistentcore.Binding, error)
}

func reconcilePersistentDaemonStartup(ctx context.Context, store persistentStartupStore, svc *daemonapp.Service) error {
	candidates, err := store.ListPersistentRecoveryCandidates(ctx)
	if err != nil {
		return err
	}
	return svc.ReconcilePersistentStartup(ctx, candidates, daemonapp.PersistentStartupOptions{})
}

func persistentSessionCatalog(base capability.Catalog, maxSessions int, maxSpoolBytes int64, maxQueuedInputBytes int) capability.Catalog {
	return base.WithNamedSessions(maxSessions, maxSpoolBytes, maxQueuedInputBytes)
}

var _ persistentapp.BindingStore = persistentBindingStoreAdapter{}
var _ daemonapp.PersistentRuntime = daemonPersistentRuntime{}
var _ daemonapp.PersistentReattachRuntime = daemonPersistentRuntime{}
