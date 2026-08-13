package process

import "testing"

func TestResolveShell(t *testing.T) {
	got, err := ResolveShell("/bin/sh", "")
	if err != nil || got != "/bin/sh" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ResolveShell("", "relative"); err == nil {
		t.Fatal("relative accepted")
	}
}
