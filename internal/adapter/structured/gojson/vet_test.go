package gojson

import (
	"context"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/source"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestGoVetJSONMapsNativeDiagnosticsAndNormalizesProviderPaths(t *testing.T) {
	input := `{
  "example/pkg": {
    "printf": [{"posn":"/repo/main.go:5:27","end":"/repo/main.go:5:29","message":"bad printf"}],
    "copylocks": [{"posn":"/deps/mod/x.go:2:3","end":"/deps/mod/x.go:2:4","message":"copy lock"}],
    "stdversion": [{"posn":"/toolchain/src/f.go:7:1","end":"/toolchain/src/f.go:7:2","message":"toolchain issue"}],
    "unreachable": [{"posn":"/private/user/secret.go:9:4","end":"/private/user/secret.go:9:8","message":"external issue"}]
  }
}`
	reader, ref := newMemoryReader(input)
	reader.input.RepositoryRoot = "/repo"
	reader.input.DependencyRoots = []string{"/deps"}
	reader.input.ToolchainRoots = []string{"/toolchain"}
	result, err := (VetAdapter{}).Parse(context.Background(), ref, reader, generousLimits())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != core.ParseComplete || len(result.Records) != 4 {
		t.Fatalf("result=%#v", result)
	}
	wantCodes := []string{"copylocks", "printf", "stdversion", "unreachable"}
	wantOrigins := []source.Origin{source.OriginDependency, source.OriginRepository, source.OriginToolchain, source.OriginExternal}
	for i, record := range result.Records {
		if record.Authority != core.AuthorityMechanical || record.Diagnostic == nil || record.Diagnostic.Code != wantCodes[i] {
			t.Fatalf("record[%d]=%#v", i, record)
		}
		loc := record.Diagnostic.Location
		if loc.Kind != source.LocationProviderReported || loc.ProviderReported == nil || loc.ProviderReported.Origin != wantOrigins[i] {
			t.Fatalf("location[%d]=%#v", i, loc)
		}
		if wantOrigins[i] == source.OriginExternal && loc.ProviderReported.SanitizedLogicalPath != "secret.go" {
			t.Fatalf("external path leaked/not sanitized: %#v", loc.ProviderReported)
		}
	}
}

func TestGoVetJSONMalformedAndStringBudgetAreTyped(t *testing.T) {
	cases := []struct {
		name, input string
		outcome     core.ParseOutcome
	}{
		{"malformed", `{"example/pkg": {"printf": [@]}}`, core.ParseMalformed},
		{"message budget", `{"example/pkg":{"printf":[{"posn":"/repo/main.go:1:1","message":"` + strings.Repeat("x", 33) + `"}]}}`, core.ParseBudgetExceeded},
		{"invalid end", `{"example/pkg":{"printf":[{"posn":"/repo/main.go:1:1","end":"not-a-position","message":"bad"}]}}`, core.ParseMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader, ref := newMemoryReader(tc.input)
			reader.input.RepositoryRoot = "/repo"
			result, err := (VetAdapter{}).Parse(context.Background(), ref, reader, limitsWith(10, 32))
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != tc.outcome {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestAuthorityDowngradesWhenAnySemanticFieldIsHeuristic(t *testing.T) {
	if got := authorityForMethods(core.DerivationNativeFieldMapping, core.DerivationDeterministicNormalize); got != core.AuthorityMechanical {
		t.Fatalf("mechanical methods -> %s", got)
	}
	if got := authorityForMethods(core.DerivationNativeFieldMapping, core.DerivationHeuristicExtraction); got != core.AuthorityAdvisory {
		t.Fatalf("heuristic method -> %s", got)
	}
}
func TestGoJSONAdapterIdentityIsStable(t *testing.T) {
	if (TestAdapter{}).ID() != "go-test-json" || (TestAdapter{}).Version() != 1 {
		t.Fatalf("test adapter id=%q version=%d", (TestAdapter{}).ID(), (TestAdapter{}).Version())
	}
	if (VetAdapter{}).ID() != "go-vet-json" || (VetAdapter{}).Version() != 1 {
		t.Fatalf("vet adapter id=%q version=%d", (VetAdapter{}).ID(), (VetAdapter{}).Version())
	}
}
