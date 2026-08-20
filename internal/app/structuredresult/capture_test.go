package structuredresult

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type fakeCaptureAuthorityRepository struct {
	mu      sync.Mutex
	records map[operation.ID]CaptureAuthorityRecord
	marks   []string
	order   *[]string
}

func newFakeCaptureAuthorityRepository() *fakeCaptureAuthorityRepository {
	return &fakeCaptureAuthorityRepository{records: map[operation.ID]CaptureAuthorityRecord{}}
}

func (f *fakeCaptureAuthorityRepository) MarkCaptureAuthorityState(_ context.Context, id operation.ID, state CaptureAuthorityState) (CaptureAuthorityRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[id]
	if !ok {
		return CaptureAuthorityRecord{}, fmt.Errorf("missing capture authority %s", id)
	}
	if record.State == state {
		return record, nil
	}
	if record.State != CaptureAuthorityPrepared {
		return record, fmt.Errorf("capture authority state conflict")
	}
	record.State = state
	f.records[id] = record
	f.marks = append(f.marks, string(id)+":"+string(state))
	return record, nil
}

func (f *fakeCaptureAuthorityRepository) addPrepared(id operation.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[id] = CaptureAuthorityRecord{SchemaVersion: CaptureAuthorityRecordSchemaV1, State: CaptureAuthorityPrepared}
}

func (f *fakeCaptureAuthorityRepository) state(id operation.ID) CaptureAuthorityState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[id].State
}

type fakeArtifactPathAuthority struct {
	mu     sync.Mutex
	path   string
	digest string
	closed bool
}

func (f *fakeArtifactPathAuthority) NormalizedWorkspacePath() string { return f.path }
func (f *fakeArtifactPathAuthority) FinalName() string               { return "junit.xml" }
func (f *fakeArtifactPathAuthority) BaselineDigest() string {
	if f.digest != "" {
		return f.digest
	}
	return "baseline"
}
func (f *fakeArtifactPathAuthority) OpenArtifactSource(context.Context, string, int64) (ArtifactSourceHandle, ArtifactSourceIdentity, error) {
	return nil, ArtifactSourceIdentity{}, ErrArtifactCaptureUnavailable
}
func (f *fakeArtifactPathAuthority) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}
func (f *fakeArtifactPathAuthority) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func TestArtifactPathAuthorityCapacityIsFiniteAndReleased(t *testing.T) {
	repo := newFakeCaptureAuthorityRepository()
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	coordinator := newCaptureCoordinatorWithRegistry(repo, registry)
	var slots []*ArtifactPathAuthoritySlot
	for i := 0; i < MaxActiveArtifactPathAuthoritiesGlobal; i++ {
		slot, err := coordinator.AcquirePathAuthoritySlot(context.Background())
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		slots = append(slots, slot)
	}
	if _, err := coordinator.AcquirePathAuthoritySlot(context.Background()); !errors.Is(err, ErrArtifactPathAuthorityCapacity) {
		t.Fatalf("fifth slot err=%v", err)
	}
	if err := slots[0].Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.AcquirePathAuthoritySlot(context.Background()); err != nil {
		t.Fatalf("slot not reusable after release: %v", err)
	}
	for _, slot := range slots[1:] {
		_ = slot.Release()
	}
}

func TestManagedPathCollisionDurablyInvalidatesEveryOverlappingClaimant(t *testing.T) {
	repo := newFakeCaptureAuthorityRepository()
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	coordinator := newCaptureCoordinatorWithRegistry(repo, registry)

	register := func(id, workspaceID, path string) (*ManagedArtifactPathClaim, *fakeArtifactPathAuthority, bool) {
		t.Helper()
		opID := operation.ID(id)
		repo.addPrepared(opID)
		slot, err := coordinator.AcquirePathAuthoritySlot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		authority := &fakeArtifactPathAuthority{path: path}
		claim, collided, err := coordinator.RegisterManagedPathClaim(context.Background(), slot, opID, workspaceID, authority)
		if err != nil {
			t.Fatal(err)
		}
		return claim, authority, collided
	}

	first, firstAuthority, collided := register("collision-first", "ws-a", "reports/junit.xml")
	if collided || !first.AllowsMechanicalCapture() {
		t.Fatalf("first collided=%v qualified=%v", collided, first.AllowsMechanicalCapture())
	}
	second, secondAuthority, collided := register("collision-second", "ws-a", "reports/junit.xml")
	if !collided || first.AllowsMechanicalCapture() || second.AllowsMechanicalCapture() {
		t.Fatalf("overlap collided=%v first=%v second=%v", collided, first.AllowsMechanicalCapture(), second.AllowsMechanicalCapture())
	}
	if repo.state(operation.ID("collision-first")) != CaptureAuthorityManagedPathCollision || repo.state(operation.ID("collision-second")) != CaptureAuthorityManagedPathCollision {
		t.Fatalf("durable states first=%q second=%q", repo.state(operation.ID("collision-first")), repo.state(operation.ID("collision-second")))
	}
	if !firstAuthority.isClosed() || !secondAuthority.isClosed() {
		t.Fatalf("collided authorities not released: first=%v second=%v", firstAuthority.isClosed(), secondAuthority.isClosed())
	}

	third, _, collided := register("collision-third", "ws-a", "reports/junit.xml")
	if !collided || third.AllowsMechanicalCapture() || repo.state(operation.ID("collision-third")) != CaptureAuthorityManagedPathCollision {
		t.Fatalf("third collided=%v qualified=%v state=%q", collided, third.AllowsMechanicalCapture(), repo.state(operation.ID("collision-third")))
	}

	otherWorkspace, _, collided := register("collision-other-workspace", "ws-b", "reports/junit.xml")
	if collided || !otherWorkspace.AllowsMechanicalCapture() {
		t.Fatalf("workspace-scoped key collided=%v qualified=%v", collided, otherWorkspace.AllowsMechanicalCapture())
	}
	otherPath, _, collided := register("collision-other-path", "ws-a", "reports/other.xml")
	if collided || !otherPath.AllowsMechanicalCapture() {
		t.Fatalf("path-scoped key collided=%v qualified=%v", collided, otherPath.AllowsMechanicalCapture())
	}

	for _, claim := range []*ManagedArtifactPathClaim{first, second, third, otherWorkspace, otherPath} {
		if err := claim.Release(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagedPathClaimReleaseKeepsCollisionAssociationUntilExecutionEnds(t *testing.T) {
	repo := newFakeCaptureAuthorityRepository()
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	coordinator := newCaptureCoordinatorWithRegistry(repo, registry)
	register := func(id string) (*ManagedArtifactPathClaim, bool) {
		opID := operation.ID(id)
		repo.addPrepared(opID)
		slot, err := coordinator.AcquirePathAuthoritySlot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		claim, collided, err := coordinator.RegisterManagedPathClaim(context.Background(), slot, opID, "ws-a", &fakeArtifactPathAuthority{path: "same.xml"})
		if err != nil {
			t.Fatal(err)
		}
		return claim, collided
	}
	first, _ := register("active-first")
	second, collided := register("active-second")
	if !collided {
		t.Fatal("second did not collide")
	}
	third, collided := register("active-third")
	if !collided {
		t.Fatal("third escaped overlap while collided claimants remained active")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if err := third.Release(); err != nil {
		t.Fatal(err)
	}
}

type fakeCaptureBaselineQualifier struct {
	mu       sync.Mutex
	order    *[]string
	calls    int
	fail     bool
	panicUse bool
	created  []*fakeArtifactPathAuthority
}

func (f *fakeCaptureBaselineQualifier) QualifyAbsent(_ context.Context, _ string, path string) (ArtifactPathAuthority, CaptureBaselineIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panicUse {
		panic("baseline re-observed")
	}
	f.calls++
	if f.order != nil {
		*f.order = append(*f.order, "baseline")
	}
	if f.fail {
		return nil, CaptureBaselineIdentity{}, errors.New("baseline unavailable")
	}
	digest := strings.Repeat("a", 64)
	a := &fakeArtifactPathAuthority{path: path, digest: digest}
	f.created = append(f.created, a)
	return a, CaptureBaselineIdentity{SchemaVersion: CaptureBaselineSchemaV1, State: CaptureBaselineAbsent, AuthorityDigest: digest}, nil
}

type orderedPresenceObserver struct {
	order    *[]string
	panicUse bool
}

func (o orderedPresenceObserver) ObserveEnvironmentPresence(_ context.Context, execution environment.ExecutionContext, name string) (EnvironmentPresenceFact, error) {
	if o.panicUse {
		panic("environment re-observed")
	}
	if o.order != nil {
		*o.order = append(*o.order, "qualify")
	}
	return NewEnvironmentPresenceFact(execution, name, false)
}

type fakeCaptureOperationReserver struct {
	mu    sync.Mutex
	order *[]string
	fail  bool
	calls []string
}

func (f *fakeCaptureOperationReserver) ReserveCaptureOperation(_ context.Context, id operation.ID, digest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, string(id)+":"+digest)
	if f.order != nil {
		*f.order = append(*f.order, "reserve")
	}
	if f.fail {
		return errors.New("reservation failed")
	}
	return nil
}

type fakeCaptureSpawner struct {
	mu    sync.Mutex
	order *[]string
	fail  bool
	calls []operation.ID
	check func()
}

func (f *fakeCaptureSpawner) SpawnCaptureOperation(_ context.Context, id operation.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.order != nil {
		*f.order = append(*f.order, "spawn")
	}
	if f.check != nil {
		f.check()
	}
	f.calls = append(f.calls, id)
	if f.fail {
		return errors.New("spawn failed")
	}
	return nil
}

func (f *fakeCaptureAuthorityRepository) ReserveCaptureAuthority(_ context.Context, authority CaptureAuthority) (CaptureAuthorityRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.order != nil {
		*f.order = append(*f.order, "persist")
	}
	record, err := NewCaptureAuthorityRecord(authority)
	if err != nil {
		return CaptureAuthorityRecord{}, false, err
	}
	id := operation.ID(authority.Intent.OperationID)
	if current, ok := f.records[id]; ok {
		if current.StructuredCaptureDigest != record.StructuredCaptureDigest {
			return current, false, errors.New("capture authority conflict")
		}
		return current, false, nil
	}
	f.records[id] = record
	return record, true, nil
}

func (f *fakeCaptureAuthorityRepository) FindCaptureAuthority(_ context.Context, id operation.ID) (CaptureAuthorityRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, ok := f.records[id]
	if !ok {
		return CaptureAuthorityRecord{}, ErrCaptureAuthorityNotFound
	}
	return current, nil
}

func preSpawnCaptureRequest(root, operationID string) PreSpawnCaptureRequest {
	return PreSpawnCaptureRequest{
		OperationID: operation.ID(operationID), SessionID: operation.SessionID(operationID + "-session"),
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		WorkspaceRoot: root, MaxBlobBytes: DefaultMaxArtifactBlobBytes,
		Invocation: PytestInvocationRequest{
			Argv:        []string{"pytest", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
			ResolvedCWD: root, WorkspaceRoot: root, Execution: environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"},
		},
	}
}

func TestPreSpawnCaptureOrdersQualificationAuthorityReservationClaimAndSpawn(t *testing.T) {
	root := t.TempDir()
	var order []string
	repo := newFakeCaptureAuthorityRepository()
	repo.order = &order
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	baseline := &fakeCaptureBaselineQualifier{order: &order}
	reserver := &fakeCaptureOperationReserver{order: &order}
	spawner := &fakeCaptureSpawner{order: &order}
	spawner.check = func() {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		if len(registry.claims) != 1 {
			t.Fatalf("spawn before managed path claim: %#v", registry.claims)
		}
	}
	preparer := newCapturePreparerWithRegistry(repo, baseline, orderedPresenceObserver{order: &order}, reserver, spawner, registry)
	result, err := preparer.PrepareAndSpawn(context.Background(), preSpawnCaptureRequest(root, "capture-order"))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Claim.Release()
	if !result.Qualified || result.Replayed || result.Collision || result.Record == nil || result.Claim == nil || result.StructuredCaptureDigest == "" {
		t.Fatalf("result=%#v", result)
	}
	if got := strings.Join(order, ","); got != "qualify,baseline,persist,reserve,spawn" {
		t.Fatalf("order=%s", got)
	}
	if baseline.calls != 1 || registry.activeSlots != 1 {
		t.Fatalf("baseline calls=%d active slots=%d", baseline.calls, registry.activeSlots)
	}
}

func TestPreSpawnCaptureReplayUsesFrozenDurableAuthorityWithoutReobservation(t *testing.T) {
	root := t.TempDir()
	repo := newFakeCaptureAuthorityRepository()
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	baseline := &fakeCaptureBaselineQualifier{}
	reserver := &fakeCaptureOperationReserver{}
	spawner := &fakeCaptureSpawner{}
	first := newCapturePreparerWithRegistry(repo, baseline, orderedPresenceObserver{}, reserver, spawner, registry)
	request := preSpawnCaptureRequest(root, "capture-replay")
	initial, err := first.PrepareAndSpawn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	digest := initial.StructuredCaptureDigest
	if err := initial.Claim.Release(); err != nil {
		t.Fatal(err)
	}

	replayBaseline := &fakeCaptureBaselineQualifier{panicUse: true}
	replay := newCapturePreparerWithRegistry(repo, replayBaseline, orderedPresenceObserver{panicUse: true}, reserver, spawner, registry)
	request.Invocation.Argv = []string{"pytest", "--junitxml=different.xml"}
	got, err := replay.PrepareAndSpawn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Claim.Release()
	if !got.Replayed || got.Qualified || got.StructuredCaptureDigest != digest || got.Claim == nil || got.Claim.AllowsMechanicalCapture() {
		t.Fatalf("replay result=%#v", got)
	}
	if replayBaseline.calls != 0 {
		t.Fatalf("replay baseline calls=%d", replayBaseline.calls)
	}
}

func TestPreSpawnCaptureReservationAndSpawnFailuresAbandonPreparedAuthority(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reserveFail bool
		spawnFail   bool
	}{
		{"reservation", true, false},
		{"spawn", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			repo := newFakeCaptureAuthorityRepository()
			registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
			baseline := &fakeCaptureBaselineQualifier{}
			reserver := &fakeCaptureOperationReserver{fail: tc.reserveFail}
			spawner := &fakeCaptureSpawner{fail: tc.spawnFail}
			preparer := newCapturePreparerWithRegistry(repo, baseline, orderedPresenceObserver{}, reserver, spawner, registry)
			request := preSpawnCaptureRequest(root, "capture-failure-"+tc.name)
			result, err := preparer.PrepareAndSpawn(context.Background(), request)
			if err == nil {
				t.Fatal("failure returned nil error")
			}
			if result.Claim != nil {
				t.Fatal("failed start retained managed claim")
			}
			if repo.state(request.OperationID) != CaptureAuthorityAbandoned {
				t.Fatalf("state=%q", repo.state(request.OperationID))
			}
			if registry.activeSlots != 0 || len(registry.claims) != 0 {
				t.Fatalf("registry leaked slots=%d claims=%d", registry.activeSlots, len(registry.claims))
			}
			if len(baseline.created) != 1 || !baseline.created[0].isClosed() {
				t.Fatalf("path authority not closed: %#v", baseline.created)
			}
			if tc.reserveFail && len(spawner.calls) != 0 {
				t.Fatalf("spawn called after reservation failure: %v", spawner.calls)
			}
		})
	}
}

func TestPreSpawnManagedCollisionStillSpawnsAndInvalidatesPriorClaim(t *testing.T) {
	root := t.TempDir()
	repo := newFakeCaptureAuthorityRepository()
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	baseline := &fakeCaptureBaselineQualifier{}
	reserver := &fakeCaptureOperationReserver{}
	spawner := &fakeCaptureSpawner{}
	preparer := newCapturePreparerWithRegistry(repo, baseline, orderedPresenceObserver{}, reserver, spawner, registry)
	first, err := preparer.PrepareAndSpawn(context.Background(), preSpawnCaptureRequest(root, "capture-overlap-first"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Claim.Release()
	second, err := preparer.PrepareAndSpawn(context.Background(), preSpawnCaptureRequest(root, "capture-overlap-second"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Claim.Release()
	if !second.Collision || first.Claim.AllowsMechanicalCapture() || second.Claim.AllowsMechanicalCapture() || len(spawner.calls) != 2 {
		t.Fatalf("first=%#v second=%#v spawn calls=%v", first, second, spawner.calls)
	}
	if repo.state(operation.ID("capture-overlap-first")) != CaptureAuthorityManagedPathCollision || repo.state(operation.ID("capture-overlap-second")) != CaptureAuthorityManagedPathCollision {
		t.Fatalf("collision states first=%q second=%q", repo.state(operation.ID("capture-overlap-first")), repo.state(operation.ID("capture-overlap-second")))
	}
}

func TestManagedPathConcurrentOverlapInvalidatesAllClaimants(t *testing.T) {
	repo := newFakeCaptureAuthorityRepository()
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	coordinator := newCaptureCoordinatorWithRegistry(repo, registry)
	const claimants = MaxActiveArtifactPathAuthoritiesGlobal
	type outcome struct {
		claim *ManagedArtifactPathClaim
		err   error
	}
	start := make(chan struct{})
	results := make(chan outcome, claimants)
	for i := 0; i < claimants; i++ {
		id := operation.ID(fmt.Sprintf("concurrent-collision-%d", i))
		repo.addPrepared(id)
		slot, err := coordinator.AcquirePathAuthoritySlot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		go func(id operation.ID, slot *ArtifactPathAuthoritySlot) {
			<-start
			claim, _, err := coordinator.RegisterManagedPathClaim(context.Background(), slot, id, "ws-concurrent", &fakeArtifactPathAuthority{path: "reports/junit.xml"})
			results <- outcome{claim: claim, err: err}
		}(id, slot)
	}
	close(start)
	var claims []*ManagedArtifactPathClaim
	for i := 0; i < claimants; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("register: %v", result.err)
		}
		claims = append(claims, result.claim)
	}
	for i, claim := range claims {
		if claim == nil || claim.AllowsMechanicalCapture() {
			t.Fatalf("claim %d remained mechanically qualified: %#v", i, claim)
		}
	}
	for i := 0; i < claimants; i++ {
		id := operation.ID(fmt.Sprintf("concurrent-collision-%d", i))
		if got := repo.state(id); got != CaptureAuthorityManagedPathCollision {
			t.Fatalf("state %s=%q", id, got)
		}
	}
	for _, claim := range claims {
		if err := claim.Release(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreSpawnCaptureReplayRejectsIncompatibleFrozenIntentMetadataWithoutReobservation(t *testing.T) {
	root := t.TempDir()
	repo := newFakeCaptureAuthorityRepository()
	registry := newArtifactPathRegistry(MaxActiveArtifactPathAuthoritiesGlobal)
	reserver := &fakeCaptureOperationReserver{}
	spawner := &fakeCaptureSpawner{}
	first := newCapturePreparerWithRegistry(repo, &fakeCaptureBaselineQualifier{}, orderedPresenceObserver{}, reserver, spawner, registry)
	base := preSpawnCaptureRequest(root, "capture-replay-conflict")
	initial, err := first.PrepareAndSpawn(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Claim.Release(); err != nil {
		t.Fatal(err)
	}
	baseReserveCalls := len(reserver.calls)
	baseSpawnCalls := len(spawner.calls)

	cases := []struct {
		name   string
		mutate func(*PreSpawnCaptureRequest)
	}{
		{"session", func(r *PreSpawnCaptureRequest) { r.SessionID = operation.SessionID("different-session") }},
		{"repository", func(r *PreSpawnCaptureRequest) { r.RepositoryID = "repo_01M09A27JCSE71BXSP477EKN35" }},
		{"workspace", func(r *PreSpawnCaptureRequest) { r.WorkspaceID = "ws_01M0CJB0KMBXWM7C7YDFYHBT2R" }},
		{"blob policy", func(r *PreSpawnCaptureRequest) { r.MaxBlobBytes = DefaultMaxArtifactBlobBytes / 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			tc.mutate(&request)
			replay := newCapturePreparerWithRegistry(repo, &fakeCaptureBaselineQualifier{panicUse: true}, orderedPresenceObserver{panicUse: true}, reserver, spawner, registry)
			if _, err := replay.PrepareAndSpawn(context.Background(), request); err == nil {
				t.Fatal("incompatible durable replay metadata accepted")
			}
		})
	}
	if len(reserver.calls) != baseReserveCalls || len(spawner.calls) != baseSpawnCalls {
		t.Fatalf("incompatible replay reached execution ports reserve=%d/%d spawn=%d/%d", len(reserver.calls), baseReserveCalls, len(spawner.calls), baseSpawnCalls)
	}
}
