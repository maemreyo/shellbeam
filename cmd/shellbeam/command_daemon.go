package main

import (
	"context"
	"fmt"
	"time"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	activityapp "github.com/maemreyo/shellbeam/internal/app/activity"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	projectcore "github.com/maemreyo/shellbeam/internal/core/project"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
	"github.com/oklog/ulid/v2"
)

func runDaemon(ctx context.Context, args []string) error {
	return runDaemonWithCodeProvider(ctx, args, nil, nil)
}

func runDaemonWithCodeProvider(ctx context.Context, args []string, providerFactory codeProviderFactory, providerResolver codeProviderResolver) error {
	cfg, paths, err := loadCommon("daemon", args)
	if err != nil {
		return err
	}
	limits := storeadapter.Limits{
		MaxSessions: cfg.MaxConcurrentSessions, MaxSessionOutput: cfg.MaxSessionOutputBytes,
		MaxTotalState: cfg.MaxTotalStateBytes, ControlReserve: cfg.ControlReserveSessionBytes,
		MaxTelemetrySamples: telemetryMaxSamples, MaxTelemetryBytes: telemetryMetadataBytes,
		MaxTelemetryKeys: telemetryMaxKeys, MaxTelemetryKeysPerRepository: telemetryMaxKeysPerRepository,
		MaxTelemetrySamplesPerKey: telemetryMaxSamplesPerKey, MaxTelemetryAge: telemetryRetentionAge,
	}
	store, err := storeadapter.Open(paths.StateDir, limits)
	if err != nil {
		return err
	}
	incarnation := ulid.Make().String()
	catalog := daemonCatalog(capability.Limits{
		CommandBytes: cfg.MaxCommandBytes, ResponseBytes: cfg.MaxResponseOutputBytes,
		SessionOutputBytes: cfg.MaxSessionOutputBytes, RuntimeMS: cfg.MaxTimeoutMS,
		LiveSessions: cfg.MaxConcurrentSessions, ActivityHistory: activitycore.MaxOperationHistory,
	})
	gitRepo := gitadapter.New()
	workspaceSvc := workspaceapp.New(store, gitRepo)
	workspaceObserver := workspaceapp.NewObserver(store, gitRepo)
	coherence := workspaceapp.NewCoherenceTracker(incarnation)
	deltaSampler := workspaceapp.NewDeltaSampler(store, gitRepo, coherence)
	activitySvc := activityapp.New(store, deltaSampler, activitycore.MaxOperationHistory)
	var codeRuntime *codeIntelligenceRuntime
	if providerFactory == nil && providerResolver == nil {
		codeRuntime, err = newCodeIntelligenceRuntime(workspaceSvc, deltaSampler, activitySvc, coherence)
	} else {
		codeRuntime, err = newCodeIntelligenceRuntimeWithProvider(workspaceSvc, deltaSampler, activitySvc, coherence, providerFactory, providerResolver)
	}
	if err != nil {
		return err
	}
	defer codeRuntime.Close()
	structuredScheduler := &structuredWorkerProxy{}
	telemetryScheduler := &telemetryWorkerProxy{}
	svc := daemonapp.NewServiceWithExecutionContextAndCoherence(store, processadapter.Owner{}, workspaceSvc, workspaceObserver, activitySvc, daemonCoherenceAdapter{tracker: coherence}, daemonapp.Options{
		Incarnation: incarnation, Shell: cfg.Shell,
		MaxQueuedInputBytes: cfg.MaxQueuedInputSessionBytes,
		TerminationGrace:    time.Duration(cfg.TerminationGraceMS) * time.Millisecond,
		Capabilities:        catalog,
		StructuredWorker:    structuredScheduler,
		TelemetryWorker:     telemetryScheduler,
	})
	projectSvc := projectapp.New(store, projectadapter.NewLoader(), store)
	actions := &daemonActions{Actions: svc, workspace: workspaceSvc, activity: activitySvc, project: projectSvc, code: codeRuntime.Service}
	server, err := ipcadapter.ListenPending(paths.RuntimeDir, actions)
	if err != nil {
		return err
	}
	defer server.Close()
	startupCtx, cancelStartup := context.WithCancel(ctx)
	defer cancelStartup()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve()
		cancelStartup()
	}()
	if err = store.AbandonUnresolved(startupCtx, incarnation); err != nil {
		select {
		case serveErr := <-serveDone:
			return serveErr
		default:
			return err
		}
	}
	telemetryRuntime, err := newExecutionTelemetryRuntime(store)
	if err != nil {
		return err
	}
	observationRuntime, err := newExecutionObservationRuntime(startupCtx, store)
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Duration(cfg.TerminationGraceMS)*time.Millisecond)
		defer cancel()
		_ = telemetryRuntime.shutdown(shutdownCtx)
		return err
	}
	structuredScheduler.bind(observationRuntime.worker)
	telemetryScheduler.bind(telemetryRuntime.worker)
	actions.events = observationRuntime.events
	actions.structured = observationRuntime.structured
	actions.telemetry = telemetryRuntime.service
	observationRuntime.startMaterialization(ctx)
	server.MarkReady()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Duration(cfg.TerminationGraceMS)*time.Millisecond)
		defer cancel()
		_ = observationRuntime.shutdown(shutdownCtx)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Duration(cfg.TerminationGraceMS)*time.Millisecond)
		defer cancel()
		_ = telemetryRuntime.shutdown(shutdownCtx)
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Duration(cfg.TerminationGraceMS)*time.Millisecond)
		defer cancel()
		_ = svc.Shutdown(shutdownCtx)
		_ = server.Close()
	}()
	return <-serveDone
}

type daemonCoherenceAdapter struct {
	tracker *workspaceapp.CoherenceTracker
}

func (a daemonCoherenceAdapter) BeginManagedShell() daemonapp.ManagedShellLease {
	return a.tracker.BeginManagedShell()
}

func (a daemonCoherenceAdapter) CaptureBarrier() workspacecore.CoherenceBarrier {
	return a.tracker.CaptureBarrier()
}

type daemonActions struct {
	ipcadapter.Actions
	workspace  *workspaceapp.Service
	activity   *activityapp.Service
	project    *projectapp.Service
	events     *observationapp.Service
	structured *structuredapp.Inspector
	telemetry  *telemetryapp.Service
	code       daemonCodeInspector
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

func (a *daemonActions) InspectEvents(ctx context.Context, request observationapp.InspectRequest) (observationapp.InspectResult, error) {
	if a.events == nil {
		return observationapp.InspectResult{}, fmt.Errorf("event observation unavailable")
	}
	return a.events.Inspect(ctx, request)
}

func (a *daemonActions) InspectStructured(ctx context.Context, request structuredapp.InspectRequest) (structuredapp.InspectResult, error) {
	if a.structured == nil {
		return structuredapp.InspectResult{}, fmt.Errorf("structured observation unavailable")
	}
	return a.structured.Inspect(ctx, request)
}

func (a *daemonActions) InspectTelemetry(ctx context.Context, request telemetryapp.InspectRequest) (telemetryapp.InspectResult, error) {
	if a.telemetry == nil {
		return telemetryapp.InspectResult{}, fmt.Errorf("execution telemetry unavailable")
	}
	return a.telemetry.Inspect(ctx, request)
}

func daemonCatalog(limits capability.Limits) capability.Catalog {
	return capability.Baseline(limits).
		WithEventJournal(observationapp.MaxInspectEvents, observationapp.MaxEventCursorBytes, observationcore.MaxSnapshotFacts, true).
		WithStructuredResults(
			[]string{"go-test-json", "go-vet-json"},
			[]string{"diagnostic", "test_case", "test_suite", "artifact_result"},
			structuredapp.MaxListRecords, true,
		).
		WithExecutionTelemetry(
			telemetryMaxSamples, telemetryMetadataBytes, telemetryMaxKeys, telemetryMaxKeysPerRepository,
			telemetryMaxSamplesPerKey, telemetryRetentionAge.Milliseconds(), telemetryapp.MaxInspectSamples,
		).
		WithCodeIntelligence()
}
