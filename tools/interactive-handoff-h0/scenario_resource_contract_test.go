package main

import (
	"context"
	"testing"
	"time"
)

func TestCountDescendantsFromPSIncludesNestedChildren(t *testing.T) {
	got, err := countDescendantsFromPS("10 1\n11 10\n12 10\n13 11\n20 1\n", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("descendants=%d want 3", got)
	}
}

func TestSelfFDCountAvailable(t *testing.T) {
	got, err := selfFDCount()
	if err != nil {
		t.Fatal(err)
	}
	if got <= 0 {
		t.Fatalf("self fd count=%d", got)
	}
}

func TestInspectP13LiveShapeCountsTmuxObjectsAndServerChildren(t *testing.T) {
	tmuxPath := requireH0Tmux(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root, err := newProbeFixtureRoot()
	if err != nil {
		t.Fatal(err)
	}
	f, err := newNativeFixtureWithCommand(ctx, tmuxPath, root, "stty -echo; exec cat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.close(context.Background())
	human, control, err := attachP13Clients(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	defer human.close()
	defer control.close()

	shape, err := inspectP13LiveShape(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if shape.Sessions != 1 || shape.Panes != 1 || shape.Clients != 2 {
		t.Fatalf("tmux shape=%#v", shape)
	}
	if shape.ServerDescendants < 1 || shape.RootEntries < 1 {
		t.Fatalf("process/root shape=%#v", shape)
	}
}
