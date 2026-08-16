package main

import (
	"context"
	"fmt"
	"time"

	environmentadapter "github.com/maemreyo/shellbeam/internal/adapter/environment"
	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	"github.com/maemreyo/shellbeam/internal/adapter/ownership"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	activityapp "github.com/maemreyo/shellbeam/internal/app/activity"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	projectcore "github.com/maemreyo/shellbeam/internal/core/project"
	reprocore "github.com/maemreyo/shellbeam/internal/core/repro"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
	"github.com/oklog/ulid/v2"
)

const (
	reproMaxCapsules   = 256
	reproMetadataBytes = 16 << 20
	reproRetentionAge  = 30 * 24 * time.Hour
)

func runDaemon(ctx context.Context, args []string) error {
	return runDaemonWithCodeProvider(ctx, args, nil, nil)
}

func runDaemonWithCodeProvider(ctx context.Context, args []string, providerFactory codeProviderFactory, providerResolver codeProviderResolver) error {
	cfg, paths, err := loadCommon("daemon", args)
	if err != nil {
		return err
	}
	incarnation := ulid.Make().String()
	// The runtime directory's lease protects the endpoint; this one protects
	// the durable authority. Two daemons pointed at one state directory would
	// each admit up to the session limit against the same store, so state
	// ownership has to be claimed before the store is opened -- not inferred
	// from whoever happens to hold the socket.
	stateLease, err := ownership.Acquire(paths.StateDir, incarnation)
	if err != nil {
		return err
	}
	defer stateLease.Release()
	store, err := openDaemonStore(paths.StateDir, cfg)
	if err != nil {
		return err
	}
	persistentRuntime, err := composePersistentSessionRuntime(store, paths.RuntimeDir, cfg)
	if err != nil {
		return err
	}
	mutationScopeSvc := daemonapp.NewMutationScopeService(store, nil)
	catalog := daemonCatalog(capability.Limits{
		CommandBytes: cfg.MaxCommandBytes, ResponseBytes: cfg.MaxResponseOutputBytes,
		SessionOutputBytes: cfg.MaxSessionOutputBytes, RuntimeMS: cfg.MaxTimeoutMS,
		LiveSessions: cfg.MaxConcurrentSessions, ActivityHistory: activitycore.MaxOperationHistory,
	})
	catalog = persistentSessionCatalog(catalog, cfg.MaxConcurrentSessions, cfg.MaxSessionOutputBytes, cfg.MaxQueuedInputSessionBytes)
	if mutationScopeSvc != nil {
		catalog = mutationScopeCatalog(catalog)
	}
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
	evidenceScheduler := &evidenceWorkerProxy{}
	projectLoader := projectadapter.NewLoader()
	projectBinder := projectapp.NewBinder(store, projectLoader, projectadapter.NewRepoPathValidator(), projectadapter.NewGoPackageValidator())
	svc := daemonapp.NewServiceWithExecutionContextAndCoherence(store, processadapter.Owner{}, workspaceSvc, workspaceObserver, activitySvc, daemonCoherenceAdapter{tracker: coherence}, daemonapp.Options{
		Incarnation: incarnation, Shell: cfg.Shell,
		DefaultTimeoutMS:     cfg.DefaultTimeoutMS,
		MaxTimeoutMS:         cfg.MaxTimeoutMS,
		MaxQueuedInputBytes:  cfg.MaxQueuedInputSessionBytes,
		TerminationGrace:     time.Duration(cfg.TerminationGraceMS) * time.Millisecond,
		Capabilities:         catalog,
		StructuredWorker:     structuredScheduler,
		TelemetryWorker:      telemetryScheduler,
		EvidenceWorker:       evidenceScheduler,
		ProjectCommandBinder: projectBinder,
		PersistentRuntime:    persistentRuntime,
	})
	hostReadiness := projectadapter.NewHostReadiness()
	projectSvc := projectapp.NewWithReadiness(
		store, projectLoader, store,
		projectapp.ReadinessObservers{Executable: hostReadiness, Environment: hostReadiness, Toolchain: hostReadiness},
		projectapp.ReadinessOptions{},
	)
	environmentSvc := environmentapp.NewService(
		environmentadapter.NewHost(), projectEnvironmentManifestProvider{project: projectSvc}, environmentadapter.NewHostProber(),
		environmentapp.Options{DefaultExecution: environmentcore.ExecutionContext{Mode: "shell", Identity: cfg.Shell}},
	)
	processSvc := processapp.NewService(
		processadapter.NewHostInspector(), svc, processapp.Options{Ports: processadapter.NewHostPortInspector()},
	)
	svc.SetEnvironmentBindingProvider(daemonEnvironmentBindingProvider{environment: environmentSvc})
	svc.SetObservationInspectors(environmentSvc, processSvc)
	actions := &daemonActions{Actions: svc, observation: svc, workspace: workspaceSvc, activity: activitySvc, project: projectSvc, code: codeRuntime.Service, mutationScopes: mutationScopeSvc}
	return serveDaemonRuntime(ctx, paths.RuntimeDir, stateLease, time.Duration(cfg.TerminationGraceMS)*time.Millisecond, store, incarnation, svc, actions, workspaceObserver, structuredScheduler, telemetryScheduler, evidenceScheduler)
}

func serveDaemonRuntime(
	ctx context.Context,
	runtimeDir string,
	stateLease *ownership.Lease,
	terminationGrace time.Duration,
	store *storeadapter.Repository,
	incarnation string,
	svc *daemonapp.Service,
	actions *daemonActions,
	workspaceObserver *workspaceapp.Observer,
	structuredScheduler *structuredWorkerProxy,
	telemetryScheduler *telemetryWorkerProxy,
	evidenceScheduler *evidenceWorkerProxy,
) error {
	server, err := ipcadapter.ListenPendingAs(runtimeDir, incarnation, actions, stateLease)
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
	outputKey, err := store.EventCursorKey(startupCtx)
	if err != nil {
		return err
	}
	outputCodec, err := outputview.NewCursorCodec(outputKey)
	if err != nil {
		return err
	}
	actions.output = outputview.NewWithCursor(store, outputCodec)
	telemetryRuntime, err := newExecutionTelemetryRuntime(store)
	if err != nil {
		return err
	}
	evidenceRuntime, err := newExecutionEvidenceRuntime(store, workspaceObserver)
	if err != nil {
		shutdownTelemetry(telemetryRuntime, terminationGrace)
		return err
	}
	observationRuntime, err := newExecutionObservationRuntime(startupCtx, store)
	if err != nil {
		shutdownEvidence(evidenceRuntime, terminationGrace)
		shutdownTelemetry(telemetryRuntime, terminationGrace)
		return err
	}
	defer shutdownObservation(observationRuntime, terminationGrace)
	defer shutdownTelemetry(telemetryRuntime, terminationGrace)
	defer shutdownEvidence(evidenceRuntime, terminationGrace)
	structuredScheduler.bind(observationRuntime.worker)
	telemetryScheduler.bind(telemetryRuntime.worker)
	evidenceScheduler.bind(evidenceRuntime.worker)
	actions.events = observationRuntime.events
	actions.structured = observationRuntime.structured
	actions.telemetry = telemetryRuntime.service
	actions.evidence = evidenceRuntime.inspector
	actions.repro = reproapp.New(store)
	observationRuntime.startMaterialization(ctx)
	evidenceRuntime.startRecovery(ctx)
	if err = reconcilePersistentDaemonStartup(startupCtx, store, svc); err != nil {
		return err
	}
	server.MarkReady()
	go shutdownDaemonOnContext(ctx, svc, server, terminationGrace)
	return <-serveDone
}

func shutdownObservation(runtime *executionObservationRuntime, grace time.Duration) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*grace)
	defer cancel()
	_ = runtime.shutdown(shutdownCtx)
}

func shutdownTelemetry(runtime *executionTelemetryRuntime, grace time.Duration) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*grace)
	defer cancel()
	_ = runtime.shutdown(shutdownCtx)
}

func shutdownEvidence(runtime *executionEvidenceRuntime, grace time.Duration) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*grace)
	defer cancel()
	_ = runtime.shutdown(shutdownCtx)
}

func shutdownDaemonOnContext(ctx context.Context, svc *daemonapp.Service, server *ipcadapter.Server, grace time.Duration) {
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*grace)
	defer cancel()
	_ = svc.Shutdown(shutdownCtx)
	_ = server.Close()
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
	observation    *daemonapp.Service
	workspace      *workspaceapp.Service
	activity       *activityapp.Service
	project        *projectapp.Service
	events         *observationapp.Service
	structured     *structuredapp.Inspector
	telemetry      *telemetryapp.Service
	evidence       *evidenceapp.Inspector
	repro          *reproapp.Service
	output         *outputview.Service
	code           daemonCodeInspector
	mutationScopes mutationScopeCoordinator
}

func (a daemonActions) InspectEnvironment(ctx context.Context, request ipcadapter.EnvironmentRequest) (ipcadapter.EnvironmentResponse, error) {
	if a.observation == nil {
		return environmentcore.Snapshot{}, fmt.Errorf("environment observation unavailable")
	}
	return a.observation.InspectEnvironment(ctx, request)
}

func (a daemonActions) InspectProcess(ctx context.Context, request ipcadapter.ProcessRequest) (ipcadapter.ProcessResponse, error) {
	if a.observation == nil {
		return processcore.Observation{}, fmt.Errorf("process observation unavailable")
	}
	return a.observation.InspectProcess(ctx, request)
}

func (a daemonActions) InspectWorkspace(ctx context.Context, workspaceID string) (workspacecore.Workspace, error) {
	return a.workspace.Inspect(ctx, workspaceID)
}

func (a daemonActions) InspectActivity(ctx context.Context, activityID string) (activitycore.Activity, error) {
	return a.activity.Inspect(ctx, activityID)
}

func (a *daemonActions) InspectSessions(ctx context.Context, request persistentcore.InspectRequest) (persistentcore.InspectPage, error) {
	if a.observation == nil {
		return persistentcore.InspectPage{}, fmt.Errorf("persistent session inspection unavailable")
	}
	return a.observation.InspectSessions(ctx, request)
}

func (a daemonActions) InspectProject(ctx context.Context, workspaceID string) (projectcore.Inspection, error) {
	return a.project.Inspect(ctx, workspaceID)
}

func (a daemonActions) InspectProjectReadiness(ctx context.Context, workspaceID string) (projectcore.Readiness, error) {
	return a.project.Readiness(ctx, workspaceID)
}

func (a *daemonActions) ReadOutputView(ctx context.Context, request outputview.Request) (outputview.Result, error) {
	if a.output == nil {
		return outputview.Result{}, fmt.Errorf("output view service unavailable")
	}
	return a.output.Read(ctx, request)
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

func (a *daemonActions) InspectEvidence(ctx context.Context, request evidenceapp.InspectRequest) (evidenceapp.InspectResult, error) {
	if a.evidence == nil {
		return evidenceapp.InspectResult{}, fmt.Errorf("execution evidence unavailable")
	}
	return a.evidence.Inspect(ctx, request)
}

func (a *daemonActions) CreateRepro(ctx context.Context, request reprocore.CreateRequest) (reprocore.Capsule, error) {
	if a.repro == nil {
		return reprocore.Capsule{}, fmt.Errorf("reproduction capsule service unavailable")
	}
	return a.repro.Create(ctx, request)
}

func (a *daemonActions) InspectRepro(ctx context.Context, reproID string) (reproapp.InspectResult, error) {
	if a.repro == nil {
		return reproapp.InspectResult{}, fmt.Errorf("reproduction capsule service unavailable")
	}
	return a.repro.Inspect(ctx, reproID)
}

func daemonCatalog(limits capability.Limits) capability.Catalog {
	return capability.Baseline(limits).
		WithProjectReadiness(projectapp.DefaultReadinessTTL.Milliseconds(), projectapp.DefaultReadinessMaxEntries).
		WithEvidence(
			coreevidence.MaxInspectRecords, coreevidence.MaxExpectedOutputs, coreevidence.MaxArtifactMetadataBytes,
			coreevidence.MaxArtifactDigestBytes, coreevidence.MaxTreeEntries, coreevidence.MaxCursorBytes,
		).
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
		WithReproductionCapsules(reproMaxCapsules, reprocore.MaxReferenceDescriptors, reproMetadataBytes).
		WithEnvironmentObservation(
			environmentcore.MaxRelevantVariables, 5, environmentcore.MaxToolchainObservations,
			environmentadapter.ProbeTimeout.Milliseconds(), environmentadapter.MaxProbeOutputBytes, environmentapp.DefaultMaxCacheEntries,
			[]string{"go", "node", "python", "java", "rust"},
		).
		WithProcessInspection(
			processcore.MaxDescendants, processcore.MaxTraversalDepth, processcore.MaxObservationBytes,
			processcore.MaxObservationDuration.Milliseconds(), processcore.MaxPortRecords, true,
		).
		WithCodeIntelligence().
		WithTypedProjectCommands([]string{"go"}).
		WithOutputViews(
			outputview.MaxReturnBytes, outputview.MaxWorkBytes, outputview.MaxLines,
			outputview.MaxMatches, outputview.MaxPatternBytes, outputview.MaxContinuationBytes,
		)
}
