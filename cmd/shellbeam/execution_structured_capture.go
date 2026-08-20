package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/maemreyo/shellbeam/internal/adapter/localfs"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type executionStructuredCaptureRuntime struct {
	store        *storeadapter.Repository
	preparer     *structuredapp.CapturePreparer
	acquirer     *structuredapp.TerminalCaptureAcquirer
	materializer *structuredapp.Materializer
	worker       *structuredapp.Worker

	mu       sync.Mutex
	prepared map[operation.ID]preparedStructuredCapture
}

type preparedStructuredCapture struct {
	sessionID operation.SessionID
	result    structuredapp.PreSpawnCaptureResult
}

func newExecutionStructuredCaptureRuntime(store *storeadapter.Repository, worker *structuredapp.Worker) (*executionStructuredCaptureRuntime, error) {
	if store == nil || worker == nil {
		return nil, fmt.Errorf("structured capture runtime unavailable")
	}
	preparer := structuredapp.NewCapturePreparer(store, localfs.ArtifactBaselineProvider{}, hostPytestPresenceObserver{}, nil, nil)
	return &executionStructuredCaptureRuntime{
		store: store, preparer: preparer,
		acquirer:     structuredapp.NewTerminalCaptureAcquirer(structuredapp.DefaultTerminalCaptureLimits(), store),
		materializer: structuredapp.NewMaterializer(store), worker: worker,
		prepared: map[operation.ID]preparedStructuredCapture{},
	}, nil
}

func (r *executionStructuredCaptureRuntime) PrepareStructuredCapture(ctx context.Context, req daemonapp.StructuredCapturePrepareRequest) (daemonapp.StructuredCapturePreparation, error) {
	if err := ctx.Err(); err != nil {
		return daemonapp.StructuredCapturePreparation{}, err
	}
	if r == nil || r.store == nil || r.preparer == nil || req.OperationID == "" || req.SessionID == "" {
		return daemonapp.StructuredCapturePreparation{}, fmt.Errorf("structured capture runtime unavailable")
	}
	explicit := req.StructuredAdapter == structuredapp.PytestJUnitAdapterID
	candidate := structuredapp.PytestCandidateArgv(req.Argv)
	if !explicit && !candidate {
		return daemonapp.StructuredCapturePreparation{}, nil
	}
	if req.ExecutionMode != operation.ExecutionModeArgv || req.WorkspaceID == "" {
		if explicit {
			return daemonapp.StructuredCapturePreparation{}, fmt.Errorf("pytest structured capture requires resolved argv workspace")
		}
		return daemonapp.StructuredCapturePreparation{}, nil
	}

	r.mu.Lock()
	if current, ok := r.prepared[req.OperationID]; ok {
		r.mu.Unlock()
		if current.sessionID != req.SessionID {
			return daemonapp.StructuredCapturePreparation{}, fmt.Errorf("structured capture operation already preparing")
		}
		return daemonPreparation(current.result, false), nil
	}
	r.mu.Unlock()

	workspace, err := r.workspace(ctx, req.WorkspaceID)
	if err != nil {
		if explicit {
			return daemonapp.StructuredCapturePreparation{}, err
		}
		return daemonapp.StructuredCapturePreparation{}, nil
	}
	result, err := r.preparer.Prepare(ctx, structuredapp.PreSpawnCaptureRequest{
		OperationID: req.OperationID, SessionID: req.SessionID,
		RepositoryID: string(workspace.RepositoryID), WorkspaceID: string(workspace.ID), WorkspaceRoot: workspace.Root,
		MaxBlobBytes: structuredapp.DefaultMaxArtifactBlobBytes,
		Producer: structuredapp.PytestCaptureRequest{Invocation: structuredapp.PytestInvocationRequest{
			Argv: append([]string(nil), req.Argv...), ResolvedCWD: req.CWD, WorkspaceRoot: workspace.Root,
			Execution: environmentcore.ExecutionContext{Mode: string(req.ExecutionMode), Identity: req.Executable},
		}},
	})
	if err != nil {
		return daemonapp.StructuredCapturePreparation{}, err
	}
	if !result.InvocationQualified {
		if explicit {
			return daemonapp.StructuredCapturePreparation{}, fmt.Errorf("pytest invocation authority unqualified")
		}
		return daemonapp.StructuredCapturePreparation{}, nil
	}
	owned := result.Record != nil || result.Claim != nil
	if owned {
		r.mu.Lock()
		if _, exists := r.prepared[req.OperationID]; exists {
			r.mu.Unlock()
			if result.Claim != nil {
				_ = result.Claim.Release()
			}
			return daemonapp.StructuredCapturePreparation{}, fmt.Errorf("structured capture operation already preparing")
		}
		r.prepared[req.OperationID] = preparedStructuredCapture{sessionID: req.SessionID, result: result}
		r.mu.Unlock()
	}
	preparation := daemonPreparation(result, owned)
	// Invocation authority may be fully qualified while the filesystem baseline
	// is unavailable. Keep the selected adapter, but leave CaptureDigest empty so
	// terminal handling never falls back to the raw-output parser.
	if result.Record == nil {
		preparation.CaptureDigest = ""
	}
	return preparation, nil
}

func daemonPreparation(result structuredapp.PreSpawnCaptureResult, owned bool) daemonapp.StructuredCapturePreparation {
	return daemonapp.StructuredCapturePreparation{AdapterID: structuredapp.PytestJUnitAdapterID, CaptureDigest: result.StructuredCaptureDigest, Owned: owned}
}

func (r *executionStructuredCaptureRuntime) AbortStructuredCapture(ctx context.Context, id operation.ID, sessionID operation.SessionID) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	prepared, ok := r.prepared[id]
	if ok && prepared.sessionID == sessionID {
		delete(r.prepared, id)
	}
	r.mu.Unlock()
	if !ok || prepared.sessionID != sessionID {
		return nil
	}
	var err error
	if prepared.result.Claim != nil {
		err = prepared.result.Claim.Release()
	}
	if prepared.result.Record != nil && prepared.result.Record.State == structuredapp.CaptureAuthorityPrepared {
		if _, markErr := r.store.MarkCaptureAuthorityState(ctx, id, structuredapp.CaptureAuthorityAbandoned); markErr != nil {
			err = errorsJoin(err, markErr)
		}
	}
	return err
}

func (r *executionStructuredCaptureRuntime) AcquireTerminal(ctx context.Context, reservation operation.Reservation) structuredapp.TerminalCaptureResult {
	if r == nil || reservation.StructuredCaptureDigest == "" {
		return structuredapp.TerminalCaptureResult{}
	}
	r.mu.Lock()
	prepared, ok := r.prepared[reservation.OperationID]
	if ok && prepared.sessionID == reservation.SessionID {
		delete(r.prepared, reservation.OperationID)
	}
	r.mu.Unlock()
	if !ok || prepared.sessionID != reservation.SessionID || prepared.result.Claim == nil || prepared.result.StructuredCaptureDigest != reservation.StructuredCaptureDigest {
		return unavailableTerminalCapture(reservation.StructuredCaptureDigest)
	}
	opener, err := prepared.result.Claim.TakeArtifactSourceOpener()
	if err != nil {
		_ = prepared.result.Claim.Release()
		return unavailableTerminalCapture(reservation.StructuredCaptureDigest)
	}
	maxBytes := structuredapp.DefaultMaxArtifactBlobBytes
	if prepared.result.Record != nil {
		maxBytes = prepared.result.Record.Authority.Intent.MaxBlobBytes
	}
	return r.acquirer.Acquire(ctx, structuredapp.TerminalCaptureRequest{CaptureAuthorityID: reservation.StructuredCaptureDigest, MaxBlobBytes: maxBytes, Opener: opener})
}

func (r *executionStructuredCaptureRuntime) ScheduleTerminal(ctx context.Context, rec receipt.Receipt, capture structuredapp.TerminalCaptureResult) error {
	if r == nil {
		_ = capture.Close()
		return fmt.Errorf("structured capture runtime unavailable")
	}
	if capture.State != structuredapp.TerminalCaptureAcquired {
		return capture.Close()
	}
	ref, err := r.materializer.Materialize(ctx, capture, rec)
	if err != nil {
		return err
	}
	record, err := r.store.FindCaptureAuthority(ctx, operation.ID(rec.OperationID))
	if err != nil {
		return err
	}
	return r.worker.ScheduleArtifact(ctx, ref, record)
}

func (r *executionStructuredCaptureRuntime) workspace(ctx context.Context, id string) (workspacecore.Workspace, error) {
	workspaces, err := r.store.ListWorkspaces(ctx)
	if err != nil {
		return workspacecore.Workspace{}, err
	}
	for _, workspace := range workspaces {
		if string(workspace.ID) == id {
			if workspace.RepositoryID == "" || workspace.Root == "" {
				break
			}
			return workspace, nil
		}
	}
	return workspacecore.Workspace{}, fmt.Errorf("workspace capture authority unavailable")
}

type hostPytestPresenceObserver struct{}

func (hostPytestPresenceObserver) ObserveEnvironmentPresence(_ context.Context, execution environmentcore.ExecutionContext, name string) (structuredapp.EnvironmentPresenceFact, error) {
	_, present := os.LookupEnv(name)
	return structuredapp.NewEnvironmentPresenceFact(execution, name, present)
}

func unavailableTerminalCapture(digest string) structuredapp.TerminalCaptureResult {
	return structuredapp.TerminalCaptureResult{State: structuredapp.TerminalCaptureUnavailable, CaptureAuthorityID: digest, DiagnosticCode: "artifact_capture_unavailable"}
}

func errorsJoin(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return fmt.Errorf("%v; %w", a, b)
}
