package session

import "testing"

func TestKillLedger(t *testing.T) {
	l := NewKillLedger()
	r, send, err := l.Admit("k1", "TERM", false)
	if err != nil || !send {
		t.Fatalf("%#v %v", r, err)
	}
	r.Succeeded = true
	l.Record(r)
	got, send, err := l.Admit("k1", "TERM", false)
	if err != nil || send || !got.Succeeded {
		t.Fatalf("%#v %v", got, err)
	}
	if _, _, err = l.Admit("k1", "KILL", false); err == nil {
		t.Fatal("conflict")
	}
}
