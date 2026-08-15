package evidence

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type inspectRepoFake struct {
	high             observation.ChangeSeq
	obligations      []observation.ObservationObligation
	records          map[string]core.Record
	byOperation      map[operation.ID]string
	validities       map[string]core.ValidityObservation
	lastListLimit    int
	putValidityCalls int
}

func (r *inspectRepoFake) ObservationHighWatermark(context.Context) (observation.ChangeSeq, error) {
	return r.high, nil
}
func (r *inspectRepoFake) ListEvidenceIndexObligations(_ context.Context, after, cut observation.ChangeSeq, limit int) ([]observation.ObservationObligation, error) {
	r.lastListLimit = limit
	var out []observation.ObservationObligation
	for _, v := range r.obligations {
		if v.ChangeSeq > after && v.ChangeSeq <= cut {
			out = append(out, v)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
func (r *inspectRepoFake) FindEvidenceByID(_ context.Context, id string) (core.Record, bool, error) {
	v, ok := r.records[id]
	return v, ok, nil
}
func (r *inspectRepoFake) FindEvidenceByOperation(_ context.Context, id operation.ID) (core.Record, bool, error) {
	eid, ok := r.byOperation[id]
	if !ok {
		return core.Record{}, false, nil
	}
	v, ok := r.records[eid]
	return v, ok, nil
}
func (r *inspectRepoFake) LoadEvidenceValidity(_ context.Context, id string) (core.ValidityObservation, bool, error) {
	v, ok := r.validities[id]
	return v, ok, nil
}
func (r *inspectRepoFake) PutEvidenceValidity(_ context.Context, v core.ValidityObservation) (bool, error) {
	r.putValidityCalls++
	old, ok := r.validities[v.EvidenceID]
	r.validities[v.EvidenceID] = v
	return ok && old.Validity != v.Validity, nil
}

type currentStateFake struct {
	states map[string]CurrentState
	calls  int
}

func (p *currentStateFake) ObserveCurrent(_ context.Context, record core.Record) CurrentState {
	p.calls++
	return p.states[record.WorkspaceID]
}

type inspectObserverFake struct {
	observations []core.ArtifactObservation
	calls        int
}

func (o *inspectObserverFake) Observe(context.Context, string, []project.Output) ([]core.ArtifactObservation, error) {
	o.calls++
	return append([]core.ArtifactObservation(nil), o.observations...), nil
}

func TestInspectEvidencePaginationFreezesIndexCutAndBindsFilter(t *testing.T) {
	repo := newInspectRepoFake()
	a := inspectRecord("a", "op-a", "test", core.ResultPass)
	b := inspectRecord("b", "op-b", "test", core.ResultPass)
	c := inspectRecord("c", "op-c", "test", core.ResultPass)
	repo.addEvidence(2, a)
	repo.addEvidence(3, b)
	codec := inspectCursorCodec(t)
	svc := NewInspector(repo, nil, nil, codec)
	req := InspectRequest{Filter: InspectFilter{VerificationKind: core.VerificationTest}, MaxRecords: 1}
	first, err := svc.Inspect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != InspectAvailable || len(first.Records) != 1 || first.Records[0].Record.EvidenceID != a.EvidenceID || first.Continuation == "" || first.IndexGeneration != 3 {
		t.Fatalf("first=%#v", first)
	}
	if repo.lastListLimit > core.MaxInspectScanRecords {
		t.Fatalf("list limit=%d", repo.lastListLimit)
	}
	repo.addEvidence(4, c)
	req.Continuation = first.Continuation
	second, err := svc.Inspect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Records[0].Record.EvidenceID != b.EvidenceID || second.Continuation != "" || second.IndexGeneration != 3 {
		t.Fatalf("second=%#v", second)
	}
	req.Filter.VerificationKind = core.VerificationBuild
	if _, err := svc.Inspect(context.Background(), req); !errors.Is(err, failure.EvidenceCursorInvalid) {
		t.Fatalf("cursor binding err=%v", err)
	}
}

func TestInspectEvidenceExactOperationNeverRunHasNoSyntheticRecord(t *testing.T) {
	repo := newInspectRepoFake()
	svc := NewInspector(repo, nil, nil, inspectCursorCodec(t))
	got, err := svc.Inspect(context.Background(), InspectRequest{Filter: InspectFilter{OperationID: "never-run"}, MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != InspectNeverRun || len(got.Records) != 0 || got.Continuation != "" {
		t.Fatalf("got=%#v", got)
	}
}

func TestInspectEvidenceDerivesFastValidityWithoutArtifactRescanOnRead(t *testing.T) {
	repo := newInspectRepoFake()
	record := inspectRecord("fast", "op-fast", "test", core.ResultPass)
	record.WorkspaceID = "ws_01K00000000000000000000000"
	record.Source = core.SourceBinding{WorkspaceID: record.WorkspaceID, PostGeneration: "gen_" + strings.Repeat("d", 64), ObservationQuality: core.SourceQualityFast}
	repo.addEvidence(1, record)
	provider := &currentStateFake{states: map[string]CurrentState{record.WorkspaceID: {Source: core.CurrentSource{WorkspaceID: record.WorkspaceID, Generation: record.Source.PostGeneration, Quality: core.SourceQualityFast}, WorkspaceRoot: "/repo"}}}
	observer := &inspectObserverFake{}
	svc := NewInspector(repo, provider, observer, inspectCursorCodec(t))
	got, err := svc.Inspect(context.Background(), InspectRequest{Filter: InspectFilter{OperationID: record.OperationID}, MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 1 || got.Records[0].Validity.SourceMatch != core.SourceMatchFast || got.Records[0].Validity.Freshness != core.FreshnessCurrent || got.Records[0].Validity.ArtifactMatch != core.ArtifactMatchNotRequired {
		t.Fatalf("got=%#v", got)
	}
	if observer.calls != 0 || repo.putValidityCalls != 0 {
		t.Fatalf("read caused artifact/persistence work: observer=%d put=%d", observer.calls, repo.putValidityCalls)
	}
}

func TestInspectEvidenceExplicitArtifactRevalidationPersistsChangedValidity(t *testing.T) {
	repo := newInspectRepoFake()
	record := inspectRecord("artifact", "op-artifact", "build", core.ResultPass)
	record.WorkspaceID = "ws_01K00000000000000000000000"
	record.Source = core.SourceBinding{WorkspaceID: record.WorkspaceID, PostGeneration: "gen_" + strings.Repeat("d", 64), ObservationQuality: core.SourceQualityFast}
	record.Artifacts = []core.ArtifactObservation{{SchemaVersion: core.ArtifactSchemaVersion, Path: "dist/app", DeclaredKind: "file", Required: true, Exists: true, ObservedKind: "file", DigestMode: "sha256", Digest: strings.Repeat("a", 64), Status: core.ArtifactCurrent, Quality: core.ObservationComplete, ObservedAt: time.Now().UTC()}}
	repo.addEvidence(1, record)
	provider := &currentStateFake{states: map[string]CurrentState{record.WorkspaceID: {Source: core.CurrentSource{WorkspaceID: record.WorkspaceID, Generation: record.Source.PostGeneration, Quality: core.SourceQualityFast}, WorkspaceRoot: "/repo"}}}
	observer := &inspectObserverFake{observations: []core.ArtifactObservation{{SchemaVersion: core.ArtifactSchemaVersion, Path: "dist/app", DeclaredKind: "file", Required: true, Exists: true, ObservedKind: "file", DigestMode: "sha256", Digest: strings.Repeat("b", 64), Status: core.ArtifactCurrent, Quality: core.ObservationComplete, ObservedAt: time.Now().UTC()}}}
	svc := NewInspector(repo, provider, observer, inspectCursorCodec(t))
	got, err := svc.Inspect(context.Background(), InspectRequest{Filter: InspectFilter{OperationID: record.OperationID, RevalidateArtifacts: true}, MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 1 || got.Records[0].Validity.ArtifactMatch != core.ArtifactMatchChanged || observer.calls != 1 || repo.putValidityCalls != 1 || got.Records[0].LastRevalidation == nil {
		t.Fatalf("got=%#v observer=%d put=%d", got, observer.calls, repo.putValidityCalls)
	}
	if _, err := svc.Inspect(context.Background(), InspectRequest{Filter: InspectFilter{RevalidateArtifacts: true}, MaxRecords: core.MaxRevalidateRecords + 1}); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("unbounded revalidation err=%v", err)
	}
}

func newInspectRepoFake() *inspectRepoFake {
	return &inspectRepoFake{records: map[string]core.Record{}, byOperation: map[operation.ID]string{}, validities: map[string]core.ValidityObservation{}}
}
func (r *inspectRepoFake) addEvidence(seq observation.ChangeSeq, record core.Record) {
	r.records[record.EvidenceID] = record
	r.byOperation[operation.ID(record.OperationID)] = record.EvidenceID
	r.obligations = append(r.obligations, observation.ObservationObligation{ChangeSeq: seq, Kind: observation.EventEvidenceRecorded, State: observation.ObligationCommitted, SubjectRef: "evidence:" + record.EvidenceID})
	if seq > r.high {
		r.high = seq
	}
}
func inspectRecord(seed, op string, kind core.VerificationKind, result core.Result) core.Record {
	sum := strings.Repeat(seed[:1], 64)
	zero := 0
	_ = zero
	return core.Record{SchemaVersion: core.SchemaVersion, EvidenceID: "ev_" + sum, OperationID: op, SessionID: op + "-session", VerificationKind: kind, ContractDigest: strings.Repeat("b", 64), ReceiptDigest: strings.Repeat("c", 64), Terminal: core.TerminalResult{Authoritative: true, Outcome: session.Success}, Result: result, Source: core.SourceBinding{ObservationQuality: core.SourceQualityUnknown}, CompletedAt: time.Now().UTC()}
}
func inspectCursorCodec(t *testing.T) *CursorCodec {
	t.Helper()
	codec, err := NewCursorCodec(observation.CursorKeyMaterial{StateRootEpoch: "epoch_11111111111111111111111111111111", Generation: "key_22222222222222222222222222222222", Secret: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return codec
}
