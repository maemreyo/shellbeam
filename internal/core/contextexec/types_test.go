package contextexec

import (
	"encoding/json"
	"strings"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

func validRequest() Request {
	return Request{
		ContextExecID:  "ctxexec_01",
		SessionID:      "session_01",
		AuthorityEpoch: delegated.AuthorityEpoch(3),
		Argv:           []string{"go", "test", "./internal/core/..."},
		TimeoutMS:      30_000,
		MaxOutputBytes: 1 << 20,
	}
}

func TestRequestV1IsArgvOnlyBoundedAndCloneSafe(t *testing.T) {
	req := validRequest()
	if err := req.Validate(); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	clone := req.Clone()
	clone.Argv[0] = "changed"
	if req.Argv[0] != "go" {
		t.Fatal("Clone shared argv backing storage")
	}

	wire, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"command", "cwd", "environment", "stdin", "tty"} {
		if strings.Contains(string(wire), `"`+forbidden+`"`) {
			t.Fatalf("argv-only request serialized forbidden field %q: %s", forbidden, wire)
		}
	}

	cases := map[string]func(*Request){
		"context id":   func(v *Request) { v.ContextExecID = "" },
		"session id":   func(v *Request) { v.SessionID = "" },
		"epoch":        func(v *Request) { v.AuthorityEpoch = 0 },
		"argv":         func(v *Request) { v.Argv = nil },
		"argv0":        func(v *Request) { v.Argv[0] = "" },
		"argv control": func(v *Request) { v.Argv[1] = "bad\x00arg" },
		"argv count": func(v *Request) {
			v.Argv = make([]string, MaxArgCount+1)
			for i := range v.Argv {
				v.Argv[i] = "x"
			}
		},
		"argv bytes":       func(v *Request) { v.Argv = []string{strings.Repeat("x", MaxArgBytes+1)} },
		"timeout negative": func(v *Request) { v.TimeoutMS = -1 },
		"timeout max":      func(v *Request) { v.TimeoutMS = MaxTimeoutMS + 1 },
		"output zero":      func(v *Request) { v.MaxOutputBytes = 0 },
		"output max":       func(v *Request) { v.MaxOutputBytes = MaxOutputBytes + 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := validRequest()
			mutate(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatalf("invalid request accepted: %#v", bad)
			}
		})
	}
}

func TestContextAndHelperBindingsCarryNoEnvironmentOrBearerMaterial(t *testing.T) {
	ctx := ContextBinding{SessionID: "session_01", AuthorityEpoch: 3, ShellIdentity: "fish:runtime_01", BoundaryQuality: "shell_prompt", CWDObserved: "/tmp/project", PrivacyState: "standard"}
	if err := ctx.Validate(); err != nil {
		t.Fatalf("context binding: %v", err)
	}
	helper := HelperBinding{OpaqueLaunchID: "launch_01", Generation: "helper_gen_01", RequestFingerprint: strings.Repeat("a", 64), ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	if err := helper.Validate(); err != nil {
		t.Fatalf("helper binding: %v", err)
	}
	wire, err := json.Marshal(struct {
		Context ContextBinding `json:"context"`
		Helper  HelperBinding  `json:"helper"`
	}{ctx, helper})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"environment", "env_hash", "claim", "bearer", "token", "secret"} {
		if strings.Contains(strings.ToLower(string(wire)), forbidden) {
			t.Fatalf("binding wire contains forbidden %q: %s", forbidden, wire)
		}
	}

	badCtx := ctx
	badCtx.BoundaryQuality = "human_attested"
	if err := badCtx.Validate(); err == nil {
		t.Fatal("human-attested boundary accepted for context exec")
	}
	badCtx = ctx
	badCtx.PrivacyState = "private"
	if err := badCtx.Validate(); err == nil {
		t.Fatal("private context binding accepted")
	}
	badHelper := helper
	badHelper.RequestFingerprint = "not-a-digest"
	if err := badHelper.Validate(); err == nil {
		t.Fatal("invalid helper request fingerprint accepted")
	}
}
