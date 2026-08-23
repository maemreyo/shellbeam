//go:build darwin || linux

package delegatedtmux

import "testing"

func TestSignalNameIsClosed(t *testing.T) {
	for _, name := range []string{"INT", "TERM", "KILL"} {
		if _, err := signalValue(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"", "HUP", "USR1", "SIGINT"} {
		if _, err := signalValue(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}
