package evidence

import (
	"strings"
	"testing"
	"time"
)

func TestValidityObservationValidateSupportsFastOnlyWithoutExactOverclaim(t *testing.T) {
	now := time.Now().UTC()
	value := ValidityObservation{SchemaVersion: ValiditySchemaVersion, EvidenceID: "ev_" + strings.Repeat("a", 64), Validity: Validity{SourceMatch: SourceMatchFast, Freshness: FreshnessCurrent, ArtifactMatch: ArtifactMatchUnknown, PolicyMatch: PolicyMatchUnknown}, CurrentSource: CurrentSource{WorkspaceID: "ws_01K00000000000000000000000", Generation: "gen_" + strings.Repeat("b", 64), Quality: SourceQualityFast}, ObservedAt: now}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	value.CurrentSource.Quality = SourceQualityExact
	if err := value.Validate(); err == nil {
		t.Fatal("exact current source without exact digests accepted")
	}
}

func TestValidityObservationValidateRejectsMalformedDimensions(t *testing.T) {
	value := ValidityObservation{SchemaVersion: ValiditySchemaVersion, EvidenceID: "ev_" + strings.Repeat("a", 64), Validity: Validity{SourceMatch: SourceMatchFast, Freshness: FreshnessCurrent, ArtifactMatch: ArtifactMatchCurrent, PolicyMatch: PolicyMatchUnknown}, CurrentSource: CurrentSource{Quality: SourceQualityUnknown}, ObservedAt: time.Now().UTC()}
	value.Validity.Freshness = "freshish"
	if err := value.Validate(); err == nil {
		t.Fatal("invalid freshness accepted")
	}
}

func TestValidityAcceptsProvenScopeAsDistinctSourceMatch(t *testing.T) {
	value := ValidityObservation{SchemaVersion: ValiditySchemaVersion, EvidenceID: "ev_" + strings.Repeat("a", 64), Validity: Validity{SourceMatch: SourceMatchProvenScope, Freshness: FreshnessCurrent, ArtifactMatch: ArtifactMatchNotRequired, PolicyMatch: PolicyMatchUnknown}, CurrentSource: CurrentSource{WorkspaceID: "ws_01K00000000000000000000000", Generation: "gen_" + strings.Repeat("b", 64), Quality: SourceQualityFast}, ObservedAt: time.Now().UTC()}
	if err := value.Validate(); err != nil {
		t.Fatalf("proven-scope validity rejected: %v", err)
	}
}
