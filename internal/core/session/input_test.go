package session

import "testing"

func TestInputLedgerRetryAndGap(t *testing.T) {
	l := NewInputLedger(5, false)
	r, err := l.AcceptChars(0, []byte("hé"))
	if err != nil || r.AcceptedBytes != 3 || r.NextOffset != 3 {
		t.Fatalf("%#v %v", r, err)
	}
	r, err = l.AcceptChars(0, []byte("hé"))
	if err != nil || !r.Duplicate {
		t.Fatalf("retry %#v %v", r, err)
	}
	if _, err = l.AcceptChars(1, []byte("x")); err == nil {
		t.Fatal("conflict")
	}
	if _, err = l.AcceptChars(4, []byte("x")); err == nil {
		t.Fatal("gap")
	}
	if _, err = l.AcceptChars(3, []byte("abc")); err == nil {
		t.Fatal("backpressure")
	}
}
func TestInputEOF(t *testing.T) {
	l := NewInputLedger(5, false)
	if _, err := l.AcceptEOF(0); err != nil {
		t.Fatal(err)
	}
	r, err := l.AcceptEOF(0)
	if err != nil || !r.Duplicate {
		t.Fatalf("%#v %v", r, err)
	}
	if _, err = l.AcceptChars(0, []byte("x")); err == nil {
		t.Fatal("input after eof")
	}
	tty := NewInputLedger(5, true)
	if _, err = tty.AcceptEOF(0); err == nil {
		t.Fatal("tty eof")
	}
}
