package supervisor

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSpoolAppendsExactOffsetsReopensAndEnforcesLimit(t *testing.T) {
	layout := testSupervisorLayout(t, "persistent-session-spool", "generation-spool")
	spool, err := OpenSpool(layout, 6)
	if err != nil {
		t.Fatal(err)
	}
	first, err := spool.AppendRange([]byte("abc"))
	if err != nil || first.Start != 0 || first.End != 3 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := spool.AppendRange([]byte("def"))
	if err != nil || second.Start != 3 || second.End != 6 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	if _, err := spool.AppendRange([]byte("g")); err == nil {
		t.Fatal("spool limit exceeded without error")
	}
	got, extent, err := spool.ReadRange(0, 6)
	if err != nil || string(got) != "abcdef" || extent != 6 {
		t.Fatalf("got=%q extent=%d err=%v", got, extent, err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSpool(layout, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, extent, err = reopened.ReadRange(3, 3)
	if err != nil || string(got) != "def" || extent != 6 {
		t.Fatalf("reopened got=%q extent=%d err=%v", got, extent, err)
	}
}

func TestSpoolImplementsBoundedOutputSink(t *testing.T) {
	layout := testSupervisorLayout(t, "persistent-session-sink", "generation-sink")
	spool, err := OpenSpool(layout, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if err := spool.Append(context.Background(), []byte("abcd")); err != nil {
		t.Fatal(err)
	}
	if err := spool.Append(context.Background(), []byte("e")); err == nil {
		t.Fatal("sink silently truncated output")
	}
}

func testSupervisorLayout(t *testing.T, sessionID, generation string) Layout {
	t.Helper()
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(filepath.Join(t.TempDir(), "runtime"), sessionID, generation, capability)
	if err != nil {
		t.Fatal(err)
	}
	return layout
}
