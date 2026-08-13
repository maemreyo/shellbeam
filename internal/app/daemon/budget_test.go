package daemon

import "testing"

func TestBudgetAdmission(t *testing.T) {
	b := NewBudget(1, 20, 5)
	if err := b.AcquireStart(); err != nil {
		t.Fatal(err)
	}
	if err := b.AcquireStart(); err == nil {
		t.Fatal("capacity")
	}
	if err := b.AcquireOutput(16); err == nil {
		t.Fatal("reserve")
	}
	if err := b.AcquireOutput(15); err != nil {
		t.Fatal(err)
	}
}
