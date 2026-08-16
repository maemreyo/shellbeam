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

func TestSpoolCanonicalAcknowledgementIsMonotonicDurableAndBounded(t *testing.T) {
	layout := testSupervisorLayout(t, "persistent-session-spool-ack", "generation-spool-ack")
	spool, err := OpenSpool(layout, 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spool.AppendRange([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if got := spool.Acknowledged(); got != 0 {
		t.Fatalf("initial ack=%d", got)
	}
	if err := spool.Acknowledge(4); err != nil || spool.Acknowledged() != 4 {
		t.Fatalf("ack=%d err=%v", spool.Acknowledged(), err)
	}
	if err := spool.Acknowledge(4); err != nil {
		t.Fatalf("duplicate ack err=%v", err)
	}
	for _, bad := range []int64{3, 7} {
		if err := spool.Acknowledge(bad); err == nil {
			t.Fatalf("invalid ack %d accepted", bad)
		}
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSpool(layout, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Acknowledged() != 4 {
		t.Fatalf("reopened ack=%d", reopened.Acknowledged())
	}
	data, extent, err := reopened.ReadRange(reopened.Acknowledged(), 16)
	if err != nil || string(data) != "ef" || extent != 6 {
		t.Fatalf("unacked=%q extent=%d err=%v", data, extent, err)
	}
}
