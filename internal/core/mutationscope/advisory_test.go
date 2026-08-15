package mutationscope

import (
	"reflect"
	"strings"
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const testWorkspace = workspace.WorkspaceID("ws_01K00000000000000000000000")

func testScope(id, activity string, mode Mode, paths ...string) Scope {
	return Scope{
		SchemaVersion: SchemaVersion, ScopeID: id, ActivityID: activity, WorkspaceID: testWorkspace,
		Mode: mode, Paths: paths, DeclaredAt: time.Unix(100, 0).UTC(), ExpiresAt: time.Unix(1000, 0).UTC(), RevisionID: "rev-" + id,
	}
}

func TestScopeValidateAcceptsCanonicalRecord(t *testing.T) {
	scope := testScope("scope-a", "activity-a", ModeMutate, "src/auth/**", "tests/auth/**")
	if err := scope.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestScopeValidateRejectsInvalidModeOrderingAndDuration(t *testing.T) {
	cases := []Scope{
		testScope("scope-a", "activity-a", Mode("write"), "src/**"),
		testScope("scope-a", "activity-a", ModeMutate, "z/**", "a/**"),
	}
	expired := testScope("scope-a", "activity-a", ModeMutate, "src/**")
	expired.ExpiresAt = expired.DeclaredAt
	cases = append(cases, expired)
	for i, scope := range cases {
		if err := scope.Validate(); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestBuildAdvisoryModeMatrixAndDeterminism(t *testing.T) {
	readA := testScope("a", "same-activity", ModeRead, "src/**")
	readB := testScope("b", "same-activity", ModeRead, "src/auth/**")
	if _, ok := BuildAdvisory(readA, readB, MaxOverlapExamples); ok {
		t.Fatal("read/read advised")
	}

	mutB := readB
	mutB.Mode = ModeMutate
	first, ok := BuildAdvisory(readA, mutB, MaxOverlapExamples)
	if !ok || first.ConflictKind != ConflictReadMutate {
		t.Fatalf("read/mutate=%#v ok=%v", first, ok)
	}
	reverse, ok := BuildAdvisory(mutB, readA, MaxOverlapExamples)
	if !ok || !reflect.DeepEqual(first, reverse) {
		t.Fatalf("order changed advisory first=%#v reverse=%#v", first, reverse)
	}
	if first.CauseFingerprint == "" || len(first.ScopeIDs) != 2 || first.ScopeIDs[0] != "a" || first.ScopeIDs[1] != "b" {
		t.Fatalf("bad advisory %#v", first)
	}

	mutA := readA
	mutA.Mode = ModeMutate
	mm, ok := BuildAdvisory(mutA, mutB, MaxOverlapExamples)
	if !ok || mm.ConflictKind != ConflictMutateMutate {
		t.Fatalf("mutate/mutate=%#v ok=%v", mm, ok)
	}

	disjoint := mutB
	disjoint.Paths = []string{"docs/**"}
	if _, ok := BuildAdvisory(mutA, disjoint, MaxOverlapExamples); ok {
		t.Fatal("disjoint scopes advised")
	}
}

func TestBuildAdvisoryFingerprintChangesWithRevision(t *testing.T) {
	a := testScope("a", "activity-a", ModeMutate, "src/**")
	b := testScope("b", "activity-b", ModeMutate, "src/auth/**")
	first, ok := BuildAdvisory(a, b, MaxOverlapExamples)
	if !ok {
		t.Fatal("missing advisory")
	}
	b.RevisionID = "new-revision"
	second, ok := BuildAdvisory(a, b, MaxOverlapExamples)
	if !ok {
		t.Fatal("missing advisory after revision")
	}
	if first.CauseFingerprint == second.CauseFingerprint {
		t.Fatal("revision did not change cause fingerprint")
	}
}

func TestBuildAdvisoryBoundsOverlapExamples(t *testing.T) {
	aPaths := make([]string, 0, MaxPathsPerScope)
	bPaths := make([]string, 0, MaxPathsPerScope)
	for i := 0; i < MaxPathsPerScope; i++ {
		aPaths = append(aPaths, "src/"+strings.Repeat("a", i+1)+"/**")
		bPaths = append(bPaths, "src/**")
	}
	a := testScope("a", "activity-a", ModeMutate, aPaths...)
	b := testScope("b", "activity-b", ModeMutate, bPaths...)
	got, ok := BuildAdvisory(a, b, 2)
	if !ok {
		t.Fatal("missing advisory")
	}
	if len(got.OverlapExamples) != 2 || !got.OverlapExamplesTruncated {
		t.Fatalf("examples=%d truncated=%v", len(got.OverlapExamples), got.OverlapExamplesTruncated)
	}
}

func TestMutationReceiptValidatesExactlyOnceShape(t *testing.T) {
	receipt := MutationReceipt{SchemaVersion: SchemaVersion, MutationID: "mutation-1", RequestFingerprint: strings.Repeat("a", 64), Result: ResultSet, ScopeID: "scope-a", CommittedAt: time.Unix(200, 0).UTC(), ExpiresAt: time.Unix(300, 0).UTC()}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	receipt.RequestFingerprint = "not-a-digest"
	if err := receipt.Validate(); err == nil {
		t.Fatal("invalid digest accepted")
	}
}

func TestScopeValidateRejectsPathLikeIDs(t *testing.T) {
	for _, id := range []string{".", "..", "scope/name", "scope name"} {
		scope := testScope(id, "activity-a", ModeMutate, "src/**")
		if err := scope.Validate(); err == nil {
			t.Fatalf("path-like scope id accepted: %q", id)
		}
	}
}
