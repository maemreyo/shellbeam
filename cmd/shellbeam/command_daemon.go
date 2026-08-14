package main

import (
	"context"
	"time"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	activityapp "github.com/maemreyo/shellbeam/internal/app/activity"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	projectcore "github.com/maemreyo/shellbeam/internal/core/project"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
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
		ActivityHistory:    activitycore.MaxOperationHistory,
	})
	gitRepo := gitadapter.New()
	workspaceSvc := workspaceapp.New(store, gitRepo)
	workspaceObserver := workspaceapp.NewObserver(store, gitRepo)
	activitySvc := activityapp.New(store, nil, activitycore.MaxOperationHistory)
	svc := daemonapp.NewServiceWithExecutionContext(store, processadapter.Owner{}, workspaceSvc, workspaceObserver, activitySvc, daemonapp.Options{
		Incarnation: incarnation, Shell: cfg.Shell,
		MaxQueuedInputBytes: cfg.MaxQueuedInputSessionBytes,
		TerminationGrace:    time.Duration(cfg.TerminationGraceMS) * time.Millisecond,
		Capabilities:        catalog,
	})
	projectSvc := projectapp.New(store, projectadapter.NewLoader(), store)
	server, err := ipcadapter.Listen(paths.RuntimeDir, daemonActions{
		Actions: svc, workspace: workspaceSvc, activity: activitySvc, project: projectSvc,
	})
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

type daemonActions struct {
	ipcadapter.Actions
	workspace *workspaceapp.Service
	activity  *activityapp.Service
	project   *projectapp.Service
}

func (a daemonActions) InspectWorkspace(ctx context.Context, workspaceID string) (workspacecore.Workspace, error) {
	return a.workspace.Inspect(ctx, workspaceID)
}

func (a daemonActions) InspectActivity(ctx context.Context, activityID string) (activitycore.Activity, error) {
	return a.activity.Inspect(ctx, activityID)
}

func (a daemonActions) InspectProject(ctx context.Context, workspaceID string) (projectcore.Inspection, error) {
	return a.project.Inspect(ctx, workspaceID)
}
