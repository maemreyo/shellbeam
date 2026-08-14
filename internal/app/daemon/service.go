package daemon

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
	"github.com/oklog/ulid/v2"
)

type Service struct {
	store            Store
	owner            ProcessOwner
	options          Options
	observer         WorkspaceObserver
	resolver         WorkspaceResolver
	activityTracker  ActivityTracker
	coherence        WorkspaceCoherence
	contextMu        sync.Mutex
	contextLast      map[workspace.WorkspaceID]workspace.FastSnapshot
	contextLastOrder []workspace.WorkspaceID
	contextSeen      map[string]struct{}
	contextSeenOrder []string
	mu               sync.RWMutex
	live             map[string]*liveSession
}
type liveSession struct {
	mu             sync.Mutex
	operationID    string
	activityID     string
	sessionID      string
	reservation    operation.Reservation
	spec           operation.ExecutionSpec
	workspace      workspaceObservation
	state          session.State
	outcome        session.Outcome
	handle         ProcessHandle
	spawn          receipt.SpawnEvidence
	exit           receipt.ExitEvidence
	signal         receipt.SignalEvidence
	input          *session.InputLedger
	kills          *session.KillLedger
	accepted       int64
	delivered      int64
	eof            bool
	captureErr     error
	terminalTarget session.State
	outputBytes    int64
	changed        chan struct{}
	jobs           chan inputJob
	writerDone     chan struct{}
	done           chan struct{}
	doneOnce       sync.Once
	coherenceLease ManagedShellLease
}
type inputJob struct {
	data []byte
	eof  bool
}

func NewService(store Store, owner ProcessOwner, options Options) *Service {
	if options.MaxQueuedInputBytes < 1 {
		options.MaxQueuedInputBytes = 262144
	}
	if options.TerminationGrace <= 0 {
		options.TerminationGrace = 5 * time.Second
	}
	if options.Capabilities.ProtocolVersion == 0 {
		options.Capabilities = capability.Baseline(capability.Limits{})
	} else {
		options.Capabilities = options.Capabilities.Clone()
	}
	return &Service{store: store, owner: owner, options: options, contextLast: map[workspace.WorkspaceID]workspace.FastSnapshot{}, contextSeen: map[string]struct{}{}, live: map[string]*liveSession{}}
}

func NewServiceWithExecutionContext(store Store, owner ProcessOwner, resolver WorkspaceResolver, observer WorkspaceObserver, tracker ActivityTracker, options Options) *Service {
	service := NewService(store, owner, options)
	service.resolver = resolver
	service.observer = observer
	service.activityTracker = tracker
	return service
}

func (s *Service) CapabilityCatalog() capability.Catalog {
	return s.options.Capabilities.Clone()
}
func (s *Service) InspectServer(context.Context) (ServerInfo, error) {
	return ServerInfo{Capabilities: s.CapabilityCatalog()}, nil
}
func newSessionID() string                    { return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String() }
func (s *Service) get(id string) *liveSession { s.mu.RLock(); defer s.mu.RUnlock(); return s.live[id] }

func invalidIntentFailure(err error) error {
	field := "command"
	switch err.Error() {
	case "cwd must be absolute":
		field = "cwd"
	case "timeout must be non-negative":
		field = "timeout_ms"
	}
	return failure.New(failure.InvalidInput, map[string]string{"field": field}, err)
}

func (s *Service) put(v *liveSession) { s.mu.Lock(); s.live[v.sessionID] = v; s.mu.Unlock() }
func (l *liveSession) notify()        { close(l.changed); l.changed = make(chan struct{}) }

func (s *Service) Start(ctx context.Context, req StartRequest) (View, error) {
	if err := s.validateActivityRequest(req); err != nil {
		return View{}, err
	}
	id, err := operation.ParseID(req.OperationID)
	if err != nil {
		return View{}, failure.New(failure.InvalidInput, map[string]string{"field": "operation_id"}, err)
	}
	if err := validateStartMetadata(req); err != nil {
		return View{}, err
	}
	logicalIntent := operation.Intent{Command: req.Command, Argv: append([]string(nil), req.Argv...), WorkspaceID: req.WorkspaceID, CWD: req.CWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS}
	if view, handled, lookupErr := s.lookupV2Replay(ctx, req, id, logicalIntent); handled {
		return view, lookupErr
	}
	reservation, spec, err := s.prepareStartReservation(ctx, req, id)
	if err != nil {
		return View{}, err
	}
	sid := newSessionID()
	reservation.SessionID = operation.SessionID(sid)
	reservation.CreatedAt = time.Now().UTC()
	stored, created, result := s.store.ReserveOperation(ctx, reservation)
	if result.Err != nil {
		return View{}, failure.Normalize(result.Err)
	}
	sid = string(stored.SessionID)
	if !created {
		return s.waitReservedStart(ctx, req, stored, sid)
	}
	executionCWD := stored.CWD
	workspaceObservation := s.captureWorkspace(ctx, executionCWD)
	activityReq := req
	activityReq.CWD = executionCWD
	activityID := s.admitActivity(ctx, activityReq, sid, workspaceObservation.binding)
	spec.CWD = executionCWD
	live := &liveSession{operationID: req.OperationID, activityID: activityID, sessionID: sid, reservation: stored, spec: spec, workspace: workspaceObservation, state: session.Starting, input: session.NewInputLedger(s.options.MaxQueuedInputBytes, req.TTY), kills: session.NewKillLedger(), changed: make(chan struct{}), jobs: make(chan inputJob, s.options.MaxQueuedInputBytes+1), writerDone: make(chan struct{}), done: make(chan struct{})}
	processObservation := s.prepareProcessStartedObservation(req.OperationID, sid)
	if processObservation.Err != nil {
		return View{}, failure.Normalize(processObservation.Err)
	}
	s.activateLiveSession(live)
	h, spawn, spawnErr := s.owner.Start(context.Background(), live.spec, sessionSink{service: s, id: sid})
	live.mu.Lock()
	live.spawn = spawn
	if spawnErr == nil {
		live.handle = h
		live.state = session.Running
	}
	live.notify()
	live.mu.Unlock()
	if spawnErr != nil {
		s.resolveProcessStartedObservation(processObservation.ObservationSeq, false)
		s.finishSpawnFailure(live)
		view, viewErr := s.waitView(ctx, stored, sid, 0, 0, req.MaxOutputBytes)
		if viewErr == nil {
			s.enrichWorkspaceContext(&view, workspaceObservation.context, req.WorkspaceHint)
			s.enrichStructuredAdapterAvailability(&view, stored)
		}
		return view, viewErr
	}
	s.resolveProcessStartedObservation(processObservation.ObservationSeq, true)
	snap := session.Snapshot{SchemaVersion: 1, OperationID: req.OperationID, SessionID: sid, DaemonIncarnation: s.options.Incarnation, State: session.Running, OutputAvailable: true, UpdatedAt: time.Now().UTC()}
	if got := s.store.AdvanceSession(context.Background(), snap); got.Err != nil {
		h.Signal("TERM")
	}
	go s.writeLoop(live)
	go s.waitLoop(live)
	if req.TimeoutMS > 0 {
		go s.timeoutLoop(live, time.Duration(req.TimeoutMS)*time.Millisecond)
	}
	view, viewErr := s.waitView(ctx, stored, sid, 0, req.YieldMS, req.MaxOutputBytes)
	s.decorateNewStartView(&view, viewErr, stored, workspaceObservation, req.WorkspaceHint)
	return view, viewErr
}

func (s *Service) activateLiveSession(live *liveSession) {
	if s.coherence != nil {
		live.coherenceLease = s.coherence.BeginManagedShell()
	}
	s.put(live)
}

func (s *Service) prepareProcessStartedObservation(operationID, sessionID string) StoreResult {
	store, ok := s.store.(processObservationStore)
	if !ok {
		return StoreResult{}
	}
	return store.PrepareProcessStartedObservation(context.Background(), operationID, sessionID)
}

func (s *Service) resolveProcessStartedObservation(seq uint64, success bool) {
	if seq == 0 {
		return
	}
	store, ok := s.store.(processObservationStore)
	if !ok {
		return
	}
	if success {
		_ = store.CommitObservationSequence(context.Background(), seq)
		return
	}
	_ = store.AbortObservationSequence(context.Background(), seq, "spawn_failed")
}

type sessionSink struct {
	service *Service
	id      string
}

func (sink sessionSink) Append(ctx context.Context, b []byte) error {
	_, result := sink.service.store.AppendOutput(ctx, operation.SessionID(sink.id), b)
	if result.Err == nil {
		if l := sink.service.get(sink.id); l != nil {
			l.mu.Lock()
			l.outputBytes += int64(len(b))
			l.notify()
			l.mu.Unlock()
		}
	}
	return result.Err
}
func (sink sessionSink) CaptureFailed(err error) {
	if l := sink.service.get(sink.id); l != nil {
		l.mu.Lock()
		if l.captureErr == nil {
			l.captureErr = err
			if l.handle != nil {
				l.handle.Signal("TERM")
			}
		}
		l.notify()
		l.mu.Unlock()
	}
}

func (s *Service) finishSpawnFailure(l *liveSession) {
	l.mu.Lock()
	l.state = session.Finalizing
	l.outcome = session.Failure
	l.mu.Unlock()
	rec := s.receiptFor(l, session.Failed, session.Failure)
	rec.FailureReason = "spawn_failed"
	rec.Spawn = l.spawn
	s.endManagedShell(l)
	s.attachWorkspaceProvenance(&rec, l.workspace)
	s.publishUntilDurable(rec)
	s.scheduleStructuredTerminal(rec, l.reservation.StructuredAdapter)
	s.scheduleTelemetryTerminal(rec)
	l.mu.Lock()
	l.state = session.Failed
	l.notify()
	l.doneOnce.Do(func() { close(l.done) })
	l.mu.Unlock()
}

func (s *Service) waitLoop(l *liveSession) {
	exit := l.handle.Wait(context.Background())
	l.mu.Lock()
	l.exit = exit
	l.state = session.Finalizing
	l.notify()
	l.mu.Unlock()
	close(l.jobs)
	<-l.writerDone
	l.mu.Lock()
	accepted, delivered, eof, captureErr, target, signalEvidence, outputBytes := l.accepted, l.delivered, l.eof, l.captureErr, l.terminalTarget, l.signal, l.outputBytes
	outcome := session.Failure
	state := session.Failed
	reason := ""
	if target == session.TimedOut {
		state, outcome = session.TimedOut, session.Timeout
	} else if target == session.Killed {
		state, outcome = session.Killed, session.KilledOutcome
	} else if captureErr != nil {
		state, outcome = session.Killed, session.KilledOutcome
		reason = "output_capture_failed"
	} else if exit.Code != nil && *exit.Code == 0 && accepted == delivered {
		state = session.Completed
		outcome = session.Success
	} else if accepted != delivered {
		reason = "input_delivery_failed"
	}
	l.mu.Unlock()
	rec := s.receiptFor(l, state, outcome)
	rec.ExecutionMode = string(l.spec.Mode)
	rec.Executable = l.spec.Executable
	if l.spec.Mode == operation.ExecutionModeShell {
		rec.Shell = l.spec.Shell
	} else {
		rec.Shell = ""
	}
	rec.CWD = l.spec.CWD
	rec.TTY = l.spec.TTY
	rec.TimeoutMS = l.spec.TimeoutMS
	rec.OutputBytes = outputBytes
	rec.OutputComplete = captureErr == nil
	rec.InputAcceptedBytes = accepted
	rec.InputDeliveredBytes = delivered
	rec.StdinClosed = eof
	rec.FailureReason = reason
	rec.Spawn = l.spawn
	rec.Exit = exit
	rec.Signal = signalEvidence
	s.endManagedShell(l)
	s.attachWorkspaceProvenance(&rec, l.workspace)
	s.publishUntilDurable(rec)
	s.scheduleStructuredTerminal(rec, l.reservation.StructuredAdapter)
	s.scheduleTelemetryTerminal(rec)
	l.mu.Lock()
	l.state = state
	l.outcome = outcome
	l.notify()
	l.doneOnce.Do(func() { close(l.done) })
	l.mu.Unlock()
}

func (s *Service) endManagedShell(l *liveSession) {
	if l != nil && l.coherenceLease != nil {
		l.coherenceLease.End()
	}
}

func (s *Service) publishUntilDurable(rec receipt.Receipt) {
	delay := 100 * time.Millisecond
	for {
		if result := s.store.PublishTerminal(context.Background(), rec); result.Err == nil {
			return
		}
		time.Sleep(delay)
		delay *= 2
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
	}
}

func (s *Service) timeoutLoop(l *liveSession, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-l.done:
		return
	}
	l.mu.Lock()
	if l.state == session.Running {
		l.terminalTarget = session.TimedOut
		l.signal = l.handle.Signal("TERM")
		l.notify()
		go func() {
			time.Sleep(s.options.TerminationGrace)
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.state == session.Running || l.state == session.Finalizing {
				l.signal = l.handle.Signal("KILL")
				l.notify()
			}
		}()
	}
	l.mu.Unlock()
}

func (s *Service) writeLoop(l *liveSession) {
	defer close(l.writerDone)
	for job := range l.jobs {
		var err error
		if job.eof {
			err = l.handle.CloseStdin()
		} else {
			err = l.handle.Write(job.data)
		}
		l.mu.Lock()
		if err != nil {
			if l.captureErr == nil {
				l.captureErr = fmt.Errorf("input_delivery_failed")
			}
			l.handle.Signal("TERM")
			l.terminalTarget = session.Killed
		} else if !job.eof {
			l.delivered += int64(len(job.data))
			l.input.Delivered(len(job.data))
		}
		l.notify()
		l.mu.Unlock()
	}
}
