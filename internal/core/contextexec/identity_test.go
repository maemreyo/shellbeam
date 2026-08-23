package contextexec

import "testing"

func TestRequestFingerprintBindsEveryPublicIdentityField(t *testing.T) {
	base := validRequest()
	want, err := base.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 64 {
		t.Fatalf("fingerprint=%q", want)
	}
	clone := base.Clone()
	if got, err := clone.Fingerprint(); err != nil || got != want {
		t.Fatalf("clone fingerprint=%q err=%v", got, err)
	}

	cases := map[string]func(*Request){
		"context_exec_id":  func(v *Request) { v.ContextExecID += "x" },
		"session_id":       func(v *Request) { v.SessionID += "x" },
		"authority_epoch":  func(v *Request) { v.AuthorityEpoch++ },
		"argv":             func(v *Request) { v.Argv[1] = "run" },
		"timeout_ms":       func(v *Request) { v.TimeoutMS++ },
		"max_output_bytes": func(v *Request) { v.MaxOutputBytes++ },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := base.Clone()
			mutate(&changed)
			got, err := changed.Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatalf("%s did not change fingerprint", name)
			}
		})
	}
}
