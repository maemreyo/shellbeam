package operation

import "testing"

func FuzzFingerprintDeterministic(f *testing.F) {
	f.Add("true", "/tmp", false, int64(0))
	f.Fuzz(func(t *testing.T, cmd, cwd string, tty bool, timeout int64) {
		i := Intent{Command: cmd, CWD: cwd, TTY: tty, TimeoutMS: timeout}
		a, ea := i.Fingerprint()
		b, eb := i.Fingerprint()
		if (ea == nil) != (eb == nil) || a != b {
			t.Fatalf("non-deterministic %q %q %v %v", a, b, ea, eb)
		}
	})
}
