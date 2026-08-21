package main

import (
	"context"
	"fmt"
	"time"

	environmentadapter "github.com/maemreyo/shellbeam/internal/adapter/environment"
	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	verificationadapter "github.com/maemreyo/shellbeam/internal/adapter/verification"
	activityapp "github.com/maemreyo/shellbeam/internal/app/activity"
	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	inputtraceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	reproapp "github.com/maemreyo/shellbeam/internal/app/repro"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	telemetryapp "github.com/maemreyo/shellbeam/internal/app/telemetry"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/buildinfo"
	"github.com/maemreyo/shellbeam/internal/config"
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
	"github.com/maemreyo/shellbeam/internal/ownership"
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
	return runDaemonWithProviders(ctx, args, providerFactory, providerResolver, nil)
}

// State ownership must be claimed before opening the store: the runtime lease
// protects only the endpoint, while two daemons sharing durable state could
// otherwise each admit up to the session limit.
func runDaemonWithProviders(ctx context.Context, args []string, providerFactory codeProviderFactory, providerResolver codeProviderResolver, checkpointFactory checkpointProviderFactory) error {
	cfg, paths, err := loadCommon("daemon", args)
	if err != nil {
		return err
	}
	incarnation, startedAt, processIdentity := ulid.Make().String(), time.Now().UTC(), buildinfo.CaptureProcessIdentity()
	stateLease, store, err := openOwnedDaemonState(paths.StateDir, incarnation, cfg)
	if err != nil {
		return err
	}
	defer stateLease.Release()
	persistentRuntime, err := composePersistentSessionRuntime(store, paths.RuntimeDir, cfg)
	if err != nil {
		return err
	}
	mutationScopeSvc := daemonapp.NewMutationScopeService(store, nil)
	catalog := withDaemonRuntimeIdentity(daemonRuntimeCatalog(cfg, mutationScopeSvc != nil), incarnation, startedAt, processIdentity)
	gitRepo := gitadapter.New()
	workspaceSvc := workspaceapp.New(store, gitRepo)
	workspaceObserver := workspaceapp.NewObserver(store, gitRepo)
	coherence := workspaceapp.NewCoherenceTracker(incarnation)
	deltaSampler := workspaceapp.NewDeltaSampler(store, gitRepo, coherence)
	checkpointSvc, catalog := composeSafetyCheckpoints(cfg.ExperimentalCheckpoints, paths.StateDir, paths.RuntimeDir, store, newCheckpointWorkspaceSource(workspaceSvc, workspaceObserver, coherence), catalog, checkpointFactory)
	inputTraceRuntime, err := composeInputTracing(ctx, cfg.ExperimentalInputTracing, paths.StateDir, store, workspaceSvc, catalog)
	if err != nil {
		return err
	}
	defer inputTraceRuntime.Close(context.Background())
	catalog = inputTraceRuntime.Catalog
	processOwner, catalog := composeResourceEnforcement(catalog, nil)
	hermeticRuntime, catalog := composeHermeticBoundary(
		ctx, cfg.ExperimentalHermeticBoundary, paths.StateDir, paths.RuntimeDir,
		newHermeticWorkspaceSource(workspaceSvc, workspaceObserver), processOwner, catalog, nil,
	)
	activitySvc := activityapp.New(store, deltaSampler, activitycore.MaxOperationHistory)
	codeRuntime, err := composeCodeIntelligenceRuntime(workspaceSvc, deltaSampler, activitySvc, coherence, providerFactory, providerResolver)
	if err != nil {
		return err
	}
	defer codeRuntime.Close()
	structuredScheduler, structuredCapture, telemetryScheduler, evidenceScheduler := &structuredWorkerProxy{}, &structuredCaptureProxy{}, &telemetryWorkerProxy{}, &evidenceWorkerProxy{}
	projectLoader := projectadapter.NewLoader()
	projectBinder := projectapp.NewBinder(store, projectLoader, projectadapter.NewRepoPathValidator(), projectadapter.NewGoPackageValidator())
	hostReadiness := projectadapter.NewHostReadiness()
	projectSvc := projectapp.NewWithReadiness(
		store, projectLoader, store, projectapp.ReadinessObservers{Executable: hostReadiness, Environment: hostReadiness, Toolchain: hostReadiness},
		projectapp.ReadinessOptions{},
	)
	environmentSvc := environmentapp.NewService(
		environmentadapter.NewHost(), projectEnvironmentManifestProvider{project: projectSvc}, environmentadapter.NewHostProber(),
		environmentapp.Options{DefaultExecution: environmentcore.ExecutionContext{Mode: "shell", Identity: cfg.Shell}},
	)
	verificationRuntime := composeVerificationRuntime(store, workspaceSvc, workspaceObserver, deltaSampler, activitySvc, projectSvc, projectBinder, verificationadapter.NewEnvironmentSource(environmentSvc))
	svc := daemonapp.NewServiceWithExecutionContextAndCoherence(store, processOwner, workspaceSvc, workspaceObserver, activitySvc, daemonCoherenceAdapter{tracker: coherence}, daemonapp.Options{
		Incarnation: incarnation, Shell: cfg.Shell,
		DefaultTimeoutMS:          cfg.DefaultTimeoutMS,
		MaxTimeoutMS:              cfg.MaxTimeoutMS,
		MaxQueuedInputBytes:       cfg.MaxQueuedInputSessionBytes,
		TerminationGrace:          time.Duration(cfg.TerminationGraceMS) * time.Millisecond,
		Capabilities:              catalog,
		StructuredWorker:          structuredScheduler,
		StructuredCapturePreparer: structuredCapture,
		StructuredCaptureTerminal: structuredCapture,
		TelemetryWorker:           telemetryScheduler,
		EvidenceWorker:            evidenceScheduler,
		ProjectCommandBinder:      projectBinder,
		PersistentRuntime:         persistentRuntime,
		MediaReader:               daemonMediaReader(),
		InputTracePreparer:        inputTraceRuntime.Preparer,
		InputTraceWorker:          inputTraceRuntime.Worker,
		HermeticRuntime:           hermeticRuntime,
	})
	processSvc := processapp.NewService(
		processadapter.NewHostInspector(), svc, processapp.Options{Ports: processadapter.NewHostPortInspector()},
	)
	svc.SetEnvironmentBindingProvider(daemonEnvironmentBindingProvider{environment: environmentSvc})
	svc.SetObservationInspectors(environmentSvc, processSvc)
	actions := &daemonActions{Actions: svc, observation: svc, workspace: workspaceSvc, activity: activitySvc, project: projectSvc, code: codeRuntime.Service, mutationScopes: mutationScopeSvc, checkpoints: checkpointSvc, inputTrace: inputTraceRuntime.Inspector, verification: verificationRuntime}
	return serveDaemonRuntime(ctx, paths.RuntimeDir, stateLease, time.Duration(cfg.TerminationGraceMS)*time.Millisecond, newHousekeeping(cfg, paths), store, incarnation, svc, actions, workspaceObserver, structuredScheduler, structuredCapture, telemetryScheduler, evidenceScheduler)
}

// openOwnedDaemonState claims durable authority before opening the store. The
// runtime directory lease only protects the endpoint; without this second lease
// two daemons pointed at one state directory could each admit against the same
// durable state. A failed store open releases the just-acquired lease here so
// the caller only owns a lease paired with a usable store.
func openOwnedDaemonState(stateDir, incarnation string, cfg config.Config) (*ownership.Lease, *storeadapter.Repository, error) {
	stateLease, err := ownership.Acquire(stateDir, incarnation)
	if err != nil {
		return nil, nil, err
	}
	store, err := openDaemonStore(stateDir, cfg)
	if err != nil {
		_ = stateLease.Release()
		return nil, nil, err
	}
	return stateLease, store, nil
}

func serveDaemonRuntime(
	ctx context.Context,
	runtimeDir string,
	stateLease *ownership.Lease,
	terminationGrace time.Duration,
	keep housekeeping,
	store *storeadapter.Repository,
	incarnation string,
	svc *daemonapp.Service,
	actions *daemonActions,
	workspaceObserver *workspaceapp.Observer,
	structuredScheduler *structuredWorkerProxy,
	structuredCapture *structuredCaptureProxy,
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
	serveDone := startDaemonServer(server, cancelStartup)
	if err = store.AbandonUnresolved(startupCtx, incarnation); err != nil {
		select {
		case serveErr := <-serveDone:
			return serveErr
		default:
			return err
		}
	}
	if err = bindDaemonOutputView(startupCtx, store, actions); err != nil {
		return err
	}
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
	structuredCapture.bind(observationRuntime.capture)
	telemetryScheduler.bind(telemetryRuntime.worker)
	evidenceScheduler.bind(evidenceRuntime.worker)
	actions.events = observationRuntime.events
	actions.structured = observationRuntime.structured
	actions.telemetry = telemetryRuntime.service
	actions.evidence = evidenceRuntime.inspector
	verificationRuntime, ok := actions.verification.(*daemonVerificationRuntime)
	if !ok || verificationRuntime == nil {
		return fmt.Errorf("verification runtime unavailable")
	}
	if err = verificationRuntime.bindRuntimeEvaluationSources(store, evidenceRuntime, telemetryRuntime); err != nil {
		return err
	}
	if err = bindReadyDaemonDecisionRuntime(store, actions, workspaceObserver, evidenceRuntime); err != nil {
		return err
	}
	if err = prepareReadyDaemonState(ctx, startupCtx, store, svc, actions, observationRuntime, evidenceRuntime); err != nil {
		return err
	}
	server.MarkReady()
	startHousekeeping(ctx, store, keep)
	go shutdownDaemonOnContext(ctx, svc, server, terminationGrace)
	return <-serveDone
}

func prepareReadyDaemonState(ctx, startupCtx context.Context, store *storeadapter.Repository, svc *daemonapp.Service, actions *daemonActions, observationRuntime *executionObservationRuntime, evidenceRuntime *executionEvidenceRuntime) error {
	actions.repro = reproapp.New(store)
	observationRuntime.startMaterialization(ctx)
	evidenceRuntime.startRecovery(ctx)
	return reconcilePersistentDaemonStartup(startupCtx, store, svc)
}

func bindReadyDaemonDecisionRuntime(store *storeadapter.Repository, actions *daemonActions, workspaceObserver *workspaceapp.Observer, evidenceRuntime *executionEvidenceRuntime) error {
	decisionEvidence := verificationadapter.NewEvidenceSource(evidenceRuntime.inspector, store)
	return bindDecisionProtocolRuntime(store, actions, actions.workspace, workspaceObserver, actions, decisionEvidence)
}

func startDaemonServer(server *ipcadapter.Server, cancelStartup context.CancelFunc) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- server.Serve()
		cancelStartup()
	}()
	return done
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
	checkpoints    *checkpointapp.Service
	inputTrace     *inputtraceapp.Inspector
	verification   daemonVerificationCoordinator
	decision       *decisionProtocolRuntime
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
		WithVerificationSemantics(verificationSemanticsSupport()).
		WithProjectReadiness(projectapp.DefaultReadinessTTL.Milliseconds(), projectapp.DefaultReadinessMaxEntries).
		WithEvidence(
			coreevidence.MaxInspectRecords, coreevidence.MaxExpectedOutputs, coreevidence.MaxArtifactMetadataBytes,
			coreevidence.MaxArtifactDigestBytes, coreevidence.MaxTreeEntries, coreevidence.MaxCursorBytes,
		).
		WithEventJournal(observationapp.MaxInspectEvents, observationapp.MaxEventCursorBytes, observationcore.MaxSnapshotFacts, true).
		WithStructuredResults(
			[]string{"go-test-json", "go-vet-json", structuredapp.PytestJUnitAdapterID, structuredapp.JestJSONAdapterID},
			[]string{"diagnostic", "test_case", "test_suite", "artifact_result"},
			structuredapp.MaxListRecords, true,
		).
		WithStructuredArtifactInputs(structuredapp.DefaultMaxArtifactBlobBytes, structuredapp.DefaultPinnedArtifactHandlesGlobal, structuredapp.DefaultMaterializationQueueDepth, structuredapp.MaxTerminalAcquireDuration.Milliseconds()).
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
