package main

import (
	"context"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/oklog/ulid/v2"
)

func runDaemon(ctx context.Context, args []string) error {
	cfg, paths, err := loadCommon("daemon", args)
	if err != nil {
		return err
	}
	limits := storeadapter.Limits{MaxSessions: cfg.MaxConcurrentSessions, MaxSessionOutput: cfg.MaxSessionOutputBytes, MaxTotalState: cfg.MaxTotalStateBytes, ControlReserve: cfg.ControlReserveSessionBytes}
	store, err := storeadapter.Open(paths.StateDir, limits)
	if err != nil {
		return err
	}
	incarnation := ulid.Make().String()
	catalog := capability.Baseline(capability.Limits{
		CommandBytes:       cfg.MaxCommandBytes,
		ResponseBytes:      cfg.MaxResponseOutputBytes,
		SessionOutputBytes: cfg.MaxSessionOutputBytes,
		RuntimeMS:          cfg.MaxTimeoutMS,
		LiveSessions:       cfg.MaxConcurrentSessions,
	})
	svc := daemonapp.NewService(store, processadapter.Owner{}, daemonapp.Options{
		Incarnation: incarnation, Shell: cfg.Shell,
		MaxQueuedInputBytes: cfg.MaxQueuedInputSessionBytes,
		TerminationGrace:    time.Duration(cfg.TerminationGraceMS) * time.Millisecond,
		Capabilities:        catalog,
	})
	server, err := ipcadapter.Listen(paths.RuntimeDir, svc)
	if err != nil {
		return err
	}
	defer server.Close()
	if err = store.AbandonUnresolved(ctx, incarnation); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Duration(cfg.TerminationGraceMS)*time.Millisecond)
		defer cancel()
		_ = svc.Shutdown(shutdownCtx)
		_ = server.Close()
	}()
	return server.Serve()
}
