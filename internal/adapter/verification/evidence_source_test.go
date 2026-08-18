package verification

import (
	"context"
	"strings"
	"testing"
	"time"

	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

type fakeEvidenceInspector struct {
	pages []evidenceapp.InspectResult
	calls []evidenceapp.InspectRequest
}

func (f *fakeEvidenceInspector) Inspect(_ context.Context, req evidenceapp.InspectRequest) (evidenceapp.InspectResult, error) {
	f.calls = append(f.calls, req)
	idx := len(f.calls) - 1
	if idx >= len(f.pages) {
		return evidenceapp.InspectResult{SchemaVersion: 1, Status: evidenceapp.InspectAvailable}, nil
	}
	return f.pages[idx], nil
}

func candidateRecord(idChar byte, kind evidence.VerificationKind, result evidence.Result) evidence.Record {
	return evidence.Record{
		SchemaVersion: evidence.SchemaVersion, EvidenceID: "ev_" + strings.Repeat(string(idChar), 64),
		OperationID: "op-" + string(idChar), SessionID: "session-" + string(idChar), WorkspaceID: "ws_01K00000000000000000000000",
		VerificationKind: kind, ContractDigest: strings.Repeat("b", 64), ReceiptDigest: strings.Repeat("c", 64), Result: result,
		Source:      evidence.SourceBinding{WorkspaceID: "ws_01K00000000000000000000000", PreGeneration: "gen_" + strings.Repeat("1", 64), PostGeneration: "gen_" + strings.Repeat("2", 64), ObservationQuality: evidence.SourceQualityFast},
		CompletedAt: time.Unix(10, 0).UTC(),
	}
}

func inspectView(record evidence.Record, freshness evidence.Freshness) evidenceapp.InspectRecord {
	return evidenceapp.InspectRecord{Record: record, Validity: evidence.Validity{Freshness: freshness}, CurrentSource: evidence.CurrentSource{WorkspaceID: record.WorkspaceID, Generation: record.Source.PostGeneration, Quality: evidence.SourceQualityFast}}
}

func sourceQuery() verificationapp.CandidateQuery {
	return verificationapp.CandidateQuery{WorkspaceID: "ws_01K00000000000000000000000", MaxRecords: 128}
}

func TestEvidenceSourceMapsLiteralResultAndFreshness(t *testing.T) {
	cases := []struct {
		name       string
		result     evidence.Result
		fresh      evidence.Freshness
		wantResult core.CandidateResult
		wantFresh  core.CandidateFreshness
	}{
		{"pass_current", evidence.ResultPass, evidence.FreshnessCurrent, core.CandidatePass, core.CandidateCurrent},
		{"pass_stale", evidence.ResultPass, evidence.FreshnessStale, core.CandidatePass, core.CandidateStale},
		{"pass_unknown", evidence.ResultPass, evidence.FreshnessUnknown, core.CandidatePass, core.CandidateUnknown},
		{"fail", evidence.ResultFail, evidence.FreshnessCurrent, core.CandidateFail, core.CandidateCurrent},
		{"incomplete", evidence.ResultIncomplete, evidence.FreshnessCurrent, core.CandidateIncomplete, core.CandidateCurrent},
		{"ambiguous", evidence.ResultAmbiguous, evidence.FreshnessCurrent, core.CandidateAmbiguous, core.CandidateCurrent},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := candidateRecord(byte('a'+i), evidence.VerificationTest, tc.result)
			inspector := &fakeEvidenceInspector{pages: []evidenceapp.InspectResult{{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, Records: []evidenceapp.InspectRecord{inspectView(r, tc.fresh)}}}}
			got, err := NewEvidenceSource(inspector).Candidates(context.Background(), sourceQuery())
			if err != nil || len(got.Candidates) != 1 {
				t.Fatalf("got=%#v err=%v", got, err)
			}
			candidate := got.Candidates[0]
			if candidate.Result != tc.wantResult || candidate.Freshness != tc.wantFresh || candidate.VerificationKind != evidence.VerificationTest || candidate.SourceGeneration != r.Source.PostGeneration || candidate.SemanticContractDigest != r.ContractDigest {
				t.Fatalf("candidate=%#v", candidate)
			}
		})
	}
}

func TestRawEvidenceKindDoesNotElevateProviderClass(t *testing.T) {
	records := []evidenceapp.InspectRecord{
		inspectView(candidateRecord('a', evidence.VerificationTest, evidence.ResultPass), evidence.FreshnessCurrent),
		inspectView(candidateRecord('b', evidence.VerificationBuild, evidence.ResultPass), evidence.FreshnessCurrent),
	}
	inspector := &fakeEvidenceInspector{pages: []evidenceapp.InspectResult{{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, Records: records}}}
	got, err := NewEvidenceSource(inspector).Candidates(context.Background(), sourceQuery())
	if err != nil || len(got.Candidates) != 2 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	for _, candidate := range got.Candidates {
		if candidate.ProviderClassKnown || candidate.ProviderClass != "" || candidate.AuthorityKnown || candidate.Authority != "" {
			t.Fatalf("raw evidence self-promoted: %#v", candidate)
		}
	}
}

func TestEvidenceSourceQualifiesOnlyFrozenTypedProjectCommandAuthority(t *testing.T) {
	typed := candidateRecord('a', evidence.VerificationTest, evidence.ResultPass)
	typed.Command.ProjectCommandID = "check"
	typed.Command.ProjectBindingDigest = strings.Repeat("d", 64)
	typed.Command.ManifestDigest = strings.Repeat("e", 64)
	partial := candidateRecord('b', evidence.VerificationTest, evidence.ResultPass)
	partial.Command.ProjectCommandID = "check"
	invalid := candidateRecord('c', evidence.VerificationTest, evidence.ResultPass)
	invalid.Command.ProjectCommandID = "../check"
	invalid.Command.ProjectBindingDigest = strings.Repeat("d", 64)
	inspector := &fakeEvidenceInspector{pages: []evidenceapp.InspectResult{{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, Records: []evidenceapp.InspectRecord{
		inspectView(typed, evidence.FreshnessCurrent), inspectView(partial, evidence.FreshnessCurrent), inspectView(invalid, evidence.FreshnessCurrent),
	}}}}
	got, err := NewEvidenceSource(inspector).Candidates(context.Background(), sourceQuery())
	if err != nil || len(got.Candidates) != 3 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if c := got.Candidates[0]; !c.ProviderClassKnown || c.ProviderClass != core.ProviderProjectCommand || !c.AuthorityKnown || c.Authority != core.AuthorityMechanical || c.ProjectCommandID != "check" || c.ProjectBindingDigest != typed.Command.ProjectBindingDigest || c.ManifestDigest != typed.Command.ManifestDigest {
		t.Fatalf("typed authority not preserved: %#v", c)
	}
	for _, c := range got.Candidates[1:] {
		if c.ProviderClassKnown || c.AuthorityKnown {
			t.Fatalf("partial/invalid command authority elevated: %#v", c)
		}
	}
}

func TestEvidenceSourceCopiesOnlyRecordedEnvironmentBinding(t *testing.T) {
	withEnv := candidateRecord('a', evidence.VerificationTest, evidence.ResultPass)
	withEnv.EnvironmentBinding = &environment.Binding{
		SnapshotID: "env_" + strings.Repeat("a", 64), EnvironmentFingerprint: strings.Repeat("b", 64), EnvironmentFingerprintVersion: environment.FingerprintVersion,
		ToolchainFingerprint: strings.Repeat("c", 64), ToolchainFingerprintVersion: environment.ToolchainFingerprintVersion, CapturedAt: time.Unix(5, 0).UTC(),
	}
	withoutEnv := candidateRecord('b', evidence.VerificationTest, evidence.ResultPass)
	inspector := &fakeEvidenceInspector{pages: []evidenceapp.InspectResult{{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, Records: []evidenceapp.InspectRecord{inspectView(withEnv, evidence.FreshnessCurrent), inspectView(withoutEnv, evidence.FreshnessCurrent)}}}}
	got, err := NewEvidenceSource(inspector).Candidates(context.Background(), sourceQuery())
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Candidates[0]; c.EnvironmentFingerprint != withEnv.EnvironmentBinding.EnvironmentFingerprint || c.EnvironmentFingerprintVersion != environment.FingerprintVersion || c.ToolchainFingerprint != withEnv.EnvironmentBinding.ToolchainFingerprint || c.ToolchainFingerprintVersion != environment.ToolchainFingerprintVersion {
		t.Fatalf("environment binding lost: %#v", c)
	}
	if c := got.Candidates[1]; c.EnvironmentFingerprint != "" || c.EnvironmentFingerprintVersion != 0 || c.ToolchainFingerprint != "" || c.ToolchainFingerprintVersion != 0 {
		t.Fatalf("environment invented: %#v", c)
	}
}

func TestEvidenceSourceBoundsLedgerPaginationAndLocalCommandFiltering(t *testing.T) {
	pages := make([]evidenceapp.InspectResult, 4)
	for i := range pages {
		r := candidateRecord(byte('a'+i), evidence.VerificationTest, evidence.ResultPass)
		if i%2 == 0 {
			r.Command.ProjectCommandID = "wanted"
		} else {
			r.Command.ProjectCommandID = "other"
		}
		pages[i] = evidenceapp.InspectResult{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, Records: []evidenceapp.InspectRecord{inspectView(r, evidence.FreshnessCurrent)}, Continuation: "next"}
	}
	inspector := &fakeEvidenceInspector{pages: pages}
	query := sourceQuery()
	query.ProjectCommandIDs = []string{"wanted"}
	got, err := NewEvidenceSource(inspector).Candidates(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspector.calls) != 4 || got.Coverage != core.CoverageBounded || len(got.Candidates) != 2 {
		t.Fatalf("calls=%d got=%#v", len(inspector.calls), got)
	}
	for i, call := range inspector.calls {
		if call.MaxRecords != evidence.MaxInspectRecords || call.Filter.WorkspaceID != query.WorkspaceID || call.Filter.ProjectCommandID != "" {
			t.Fatalf("call %d=%#v", i, call)
		}
	}
}

func TestEvidenceSourceOverallCandidateCapMarksBoundedHistory(t *testing.T) {
	a := candidateRecord('a', evidence.VerificationTest, evidence.ResultPass)
	b := candidateRecord('b', evidence.VerificationTest, evidence.ResultFail)
	inspector := &fakeEvidenceInspector{pages: []evidenceapp.InspectResult{{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, Records: []evidenceapp.InspectRecord{inspectView(a, evidence.FreshnessCurrent), inspectView(b, evidence.FreshnessCurrent)}}}}
	query := sourceQuery()
	query.MaxRecords = 1
	got, err := NewEvidenceSource(inspector).Candidates(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 || got.Coverage != core.CoverageBounded {
		t.Fatalf("got=%#v", got)
	}
}

func TestEvidenceSourceFallsBackToPreGenerationWhenPostUnavailable(t *testing.T) {
	record := candidateRecord('a', evidence.VerificationTest, evidence.ResultPass)
	record.Source.PostGeneration = ""
	inspector := &fakeEvidenceInspector{pages: []evidenceapp.InspectResult{{SchemaVersion: 1, Status: evidenceapp.InspectAvailable, Records: []evidenceapp.InspectRecord{inspectView(record, evidence.FreshnessUnknown)}}}}
	got, err := NewEvidenceSource(inspector).Candidates(context.Background(), sourceQuery())
	if err != nil || len(got.Candidates) != 1 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if got.Candidates[0].SourceGeneration != record.Source.PreGeneration {
		t.Fatalf("source generation=%q want pre=%q", got.Candidates[0].SourceGeneration, record.Source.PreGeneration)
	}
}
