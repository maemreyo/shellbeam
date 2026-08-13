package config

import "testing"

func TestDefaultsValidate(t *testing.T) {
	c := Defaults()
	if c.MaxConcurrentSessions != 4 || c.MaxSessionOutputBytes != 268435456 || c.ControlReserveSessionBytes != 1048576 {
		t.Fatalf("defaults=%#v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.MaxConcurrentSessions = 0
	if err := c.Validate(); err == nil {
		t.Fatal("zero capacity accepted")
	}
}

func TestResolvePaths(t *testing.T) {
	p, err := ResolvePaths("linux", 42, "/home/u", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if p.RuntimeDir != "/tmp/shellbeam-42" {
		t.Fatalf("runtime=%s", p.RuntimeDir)
	}
	if _, err := ResolvePaths("windows", 1, "/x", nil); err == nil {
		t.Fatal("windows accepted")
	}
}
