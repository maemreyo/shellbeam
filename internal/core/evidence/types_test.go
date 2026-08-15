package evidence

import (
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestContractValidateAndDigest(t *testing.T) {
	base := Contract{
		VerificationKind: VerificationTest,
		SourceScope:      SourceScopeFull,
		ExpectedOutputs:  []project.Output{{Path: "dist/app", Kind: "file", Digest: "sha256", Required: true}},
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	d1, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if len(d1) != 64 {
		t.Fatalf("digest=%q", d1)
	}
	copy := base
	copy.ExpectedOutputs = append([]project.Output(nil), base.ExpectedOutputs...)
	copy.ExpectedOutputs[0].Path = "dist/other"
	d2, err := copy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Fatal("contract mutation did not change digest")
	}

	invalid := []Contract{
		{},
		{VerificationKind: "lint"},
		{VerificationKind: VerificationTest, SourceScope: "paths"},
		{VerificationKind: VerificationArtifact},
	}
	for _, c := range invalid {
		if err := c.Validate(); err == nil {
			t.Fatalf("invalid contract accepted: %#v", c)
		}
	}
}

func TestDeriveResultUsesReceiptAndRequiredArtifactsOnly(t *testing.T) {
	currentRequired := ArtifactObservation{Path: "dist/app", Required: true, Status: ArtifactCurrent, Quality: ObservationComplete}
	missingRequired := ArtifactObservation{Path: "dist/app", Required: true, Status: ArtifactMissing, Quality: ObservationComplete}
	missingOptional := ArtifactObservation{Path: "dist/map", Required: false, Status: ArtifactMissing, Quality: ObservationComplete}
	unavailableRequired := ArtifactObservation{Path: "dist/app", Required: true, Status: ArtifactUnavailable, Quality: ObservationUnavailable}

	tests := []struct {
		name      string
		terminal  TerminalResult
		artifacts []ArtifactObservation
		want      Result
	}{
		{"success", TerminalResult{Authoritative: true, Outcome: session.Success}, []ArtifactObservation{currentRequired}, ResultPass},
		{"optional missing", TerminalResult{Authoritative: true, Outcome: session.Success}, []ArtifactObservation{currentRequired, missingOptional}, ResultPass},
		{"required missing", TerminalResult{Authoritative: true, Outcome: session.Success}, []ArtifactObservation{missingRequired}, ResultFail},
		{"required unavailable", TerminalResult{Authoritative: true, Outcome: session.Success}, []ArtifactObservation{unavailableRequired}, ResultIncomplete},
		{"child failure", TerminalResult{Authoritative: true, Outcome: session.Failure}, nil, ResultFail},
		{"timeout", TerminalResult{Authoritative: true, Outcome: session.Timeout}, nil, ResultFail},
		{"killed", TerminalResult{Authoritative: true, Outcome: session.KilledOutcome}, nil, ResultFail},
		{"ambiguous", TerminalResult{Authoritative: true, Outcome: session.Ambiguous}, nil, ResultAmbiguous},
		{"not authoritative", TerminalResult{Authoritative: false, Outcome: session.Success}, nil, ResultIncomplete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveResult(tt.terminal, tt.artifacts); got != tt.want {
				t.Fatalf("got=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestValidityNeverUpgradesFastFactsToExact(t *testing.T) {
	record := SourceBinding{WorkspaceID: "ws", PostGeneration: "gen_" + strings.Repeat("a", 64), ObservationQuality: SourceQualityFast}
	currentSame := CurrentSource{WorkspaceID: "ws", Generation: record.PostGeneration, Quality: SourceQualityFast}
	v := DeriveSourceValidity(record, currentSame)
	if v.SourceMatch != SourceMatchFast || v.Freshness != FreshnessCurrent {
		t.Fatalf("validity=%#v", v)
	}

	currentChanged := currentSame
	currentChanged.Generation = "gen_" + strings.Repeat("b", 64)
	v = DeriveSourceValidity(record, currentChanged)
	if v.SourceMatch != SourceMatchMismatch || v.Freshness != FreshnessStale {
		t.Fatalf("changed=%#v", v)
	}

	unknown := DeriveSourceValidity(record, CurrentSource{})
	if unknown.SourceMatch != SourceMatchUnknown || unknown.Freshness != FreshnessUnknown {
		t.Fatalf("unknown=%#v", unknown)
	}

	exactRecord := record
	exactRecord.SourceContentDigest = strings.Repeat("c", 64)
	exactRecord.VCSStateDigest = strings.Repeat("d", 64)
	exactRecord.ObservationQuality = SourceQualityExact
	exactCurrent := CurrentSource{WorkspaceID: "ws", Generation: record.PostGeneration, SourceContentDigest: exactRecord.SourceContentDigest, VCSStateDigest: exactRecord.VCSStateDigest, Quality: SourceQualityExact}
	v = DeriveSourceValidity(exactRecord, exactCurrent)
	if v.SourceMatch != SourceMatchExact || v.Freshness != FreshnessCurrent {
		t.Fatalf("exact=%#v", v)
	}
}

func TestRecordValidateRejectsUnprovenExactAndMalformedArtifacts(t *testing.T) {
	now := time.Now().UTC()
	r := Record{
		SchemaVersion: 1, EvidenceID: "ev_" + strings.Repeat("a", 64), OperationID: "op", SessionID: "sid",
		VerificationKind: VerificationTest, ContractDigest: strings.Repeat("b", 64), ReceiptDigest: strings.Repeat("c", 64),
		Result: ResultPass, CompletedAt: now,
		Source: SourceBinding{ObservationQuality: SourceQualityFast, SourceContentDigest: strings.Repeat("d", 64)},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("fast source accepted exact digest")
	}
	r.Source.SourceContentDigest = ""
	if err := r.Validate(); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
}

func TestEvidenceIDDeterministicFromReceiptAndContractAuthority(t *testing.T) {
	receiptDigest := strings.Repeat("a", 64)
	contractDigest := strings.Repeat("b", 64)
	first, err := EvidenceID(receiptDigest, contractDigest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EvidenceID(receiptDigest, contractDigest)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "ev_") || len(first) != 67 {
		t.Fatalf("ids=%q %q", first, second)
	}
	changed, err := EvidenceID(strings.Repeat("c", 64), contractDigest)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("receipt authority did not affect evidence id")
	}
	if _, err := EvidenceID("bad", contractDigest); err == nil {
		t.Fatal("invalid receipt digest accepted")
	}
}

func TestRecordValidateRejectsUnsafeAuthorityIDs(t *testing.T) {
	now := time.Now().UTC()
	base := Record{
		SchemaVersion: SchemaVersion, EvidenceID: "ev_" + strings.Repeat("a", 64),
		OperationID: "op-safe", SessionID: "session-safe", VerificationKind: VerificationTest,
		ContractDigest: strings.Repeat("b", 64), ReceiptDigest: strings.Repeat("c", 64),
		Terminal: TerminalResult{Authoritative: true, Outcome: session.Success}, Result: ResultPass, CompletedAt: now,
	}
	for name, mutate := range map[string]func(*Record){
		"operation": func(r *Record) { r.OperationID = "../escape" },
		"session":   func(r *Record) { r.SessionID = "../escape" },
		"workspace": func(r *Record) { r.WorkspaceID = "../escape" },
	} {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("unsafe record accepted: %#v", got)
			}
		})
	}
}
