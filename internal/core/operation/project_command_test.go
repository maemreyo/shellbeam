package operation

import (
	"strings"
	"testing"
	"time"
)

func TestTypedRequestIntentFingerprintIsCallerStableAndMapOrderIndependent(t *testing.T) {
	base := TypedRequestIntent{
		WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test_package",
		Params: map[string]string{"package": "./internal/app", "count": "+003"}, TTY: true, TimeoutMS: 5000,
	}
	first, err := base.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.Params = map[string]string{"count": "+003", "package": "./internal/app"}
	second, err := reordered.Fingerprint()
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("first=%q second=%q err=%v", first, second, err)
	}
	nilParams := base
	nilParams.Params = nil
	emptyParams := base
	emptyParams.Params = map[string]string{}
	a, err := nilParams.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	b, err := emptyParams.Fingerprint()
	if err != nil || a != b {
		t.Fatalf("nil/empty params differ a=%q b=%q err=%v", a, b, err)
	}
	mutations := []func(*TypedRequestIntent){
		func(v *TypedRequestIntent) { v.WorkspaceID = "ws_01K00000000000000000000001" },
		func(v *TypedRequestIntent) { v.ProjectCommandID = "test_other" },
		func(v *TypedRequestIntent) { v.Params["package"] = "./other" },
		func(v *TypedRequestIntent) { v.TTY = false },
		func(v *TypedRequestIntent) { v.TimeoutMS = 6000 },
	}
	for i, mutate := range mutations {
		changed := base
		changed.Params = map[string]string{"package": "./internal/app", "count": "+003"}
		mutate(&changed)
		got, err := changed.Fingerprint()
		if err != nil {
			t.Fatalf("mutation %d: %v", i, err)
		}
		if got == first {
			t.Fatalf("mutation %d did not change fingerprint", i)
		}
	}
}

func TestTypedRequestIntentRejectsUnboundedOrInvalidCallerFacts(t *testing.T) {
	valid := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test", Params: map[string]string{"name": "ok"}}
	cases := []TypedRequestIntent{
		{WorkspaceID: "bad", ProjectCommandID: "test"},
		{WorkspaceID: valid.WorkspaceID, ProjectCommandID: "../test"},
		{WorkspaceID: valid.WorkspaceID, ProjectCommandID: "Test"},
		{WorkspaceID: valid.WorkspaceID, ProjectCommandID: "test", TimeoutMS: -1},
		{WorkspaceID: valid.WorkspaceID, ProjectCommandID: "test", Params: map[string]string{"BAD": "ok"}},
		{WorkspaceID: valid.WorkspaceID, ProjectCommandID: "test", Params: map[string]string{"name": "line\nbreak"}},
		{WorkspaceID: valid.WorkspaceID, ProjectCommandID: "test", Params: map[string]string{"name": strings.Repeat("x", 1025)}},
	}
	tooMany := valid
	tooMany.Params = make(map[string]string, 33)
	for i := 0; i < 33; i++ {
		tooMany.Params[string(rune('a'+i%26))+strings.Repeat("x", i/26)] = "v"
	}
	cases = append(cases, tooMany)
	for _, tc := range cases {
		if got, err := tc.Fingerprint(); err == nil {
			t.Fatalf("invalid intent accepted %#v fingerprint=%q", tc, got)
		}
	}
}

func TestTypedIntentClaimValidatesImmutableFingerprint(t *testing.T) {
	intent := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test", Params: map[string]string{"name": "ok"}}
	fingerprint, err := intent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	claim := TypedIntentClaim{
		SchemaVersion: TypedIntentClaimSchemaVersion, OperationID: "typed-op", RequestFingerprint: fingerprint,
		Intent: intent, CreatedAt: time.Date(2026, 8, 15, 1, 0, 0, 0, time.UTC),
	}
	if err := claim.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*TypedIntentClaim){
		"schema":      func(v *TypedIntentClaim) { v.SchemaVersion++ },
		"operation":   func(v *TypedIntentClaim) { v.OperationID = "../bad" },
		"fingerprint": func(v *TypedIntentClaim) { v.RequestFingerprint = strings.Repeat("f", 64) },
		"created":     func(v *TypedIntentClaim) { v.CreatedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			got := claim
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid claim accepted: %#v", got)
			}
		})
	}
}
