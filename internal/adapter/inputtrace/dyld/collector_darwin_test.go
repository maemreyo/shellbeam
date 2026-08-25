//go:build darwin

package dyld

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

func TestE27CollectorActivationHandshakeDistinguishesInactiveFromEmptyTrace(t *testing.T) {
	newTrace := func(id string) *collector {
		root := filepath.Join(e27PrivateState(t), id)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		c, err := newCollector(root, e27TestSocketRoot(t), "trace_01K00000000000000000000000", defaultCollectorLimits())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	inactive := newTrace("inactive")
	if got := inactive.finalize(); got.GapReason != "instrumentation_inactive" || got.RawEventCount != 0 {
		t.Fatalf("inactive snapshot=%#v", got)
	}
	active := newTrace("active")
	active.ingest(encodeEvent(eventInstrumentationActive, 10, ""))
	if got := active.finalize(); got.GapReason != "" || got.RawEventCount != 0 || len(got.Resources) != 0 {
		t.Fatalf("active empty snapshot=%#v", got)
	}
}

func TestE27CollectorFinalizeDrainsAcceptedSocketDatagrams(t *testing.T) {
	root := filepath.Join(e27PrivateState(t), "trace")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	limits := collectorLimits{maxEvents: 1024, maxUnique: 8, maxPrivateBytes: 1 << 20, maxDuration: time.Minute}
	collector, err := newCollector(root, e27TestSocketRoot(t), "trace_01K00000000000000000000000", limits)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: collector.socketPath, Net: "unixgram"})
	if err != nil {
		collector.abort()
		t.Fatal(err)
	}
	const accepted = 32
	raw := encodeEvent(eventExecutedBinary, 10, "/usr/bin/true")
	for i := 0; i < accepted; i++ {
		if _, err := sender.Write(raw); err != nil {
			_ = sender.Close()
			collector.abort()
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := sender.Close(); err != nil {
		collector.abort()
		t.Fatal(err)
	}
	snapshot := collector.finalize()
	if snapshot.RawEventCount != accepted {
		t.Fatalf("finalize dropped accepted datagrams: got=%d want=%d", snapshot.RawEventCount, accepted)
	}
}

func TestE27CollectorProtocolPrivacyAndBudgets(t *testing.T) {
	root := filepath.Join(e27PrivateState(t), "trace")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	limits := collectorLimits{maxEvents: 2, maxUnique: 2, maxPrivateBytes: 1 << 20, maxDuration: time.Minute}
	collector, err := newCollector(root, e27TestSocketRoot(t), "trace_01K00000000000000000000000", limits)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(collector.rawPath); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("raw info=%#v err=%v", info, err)
	}
	if info, err := os.Lstat(collector.socketPath); err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("socket info=%#v err=%v", info, err)
	}
	collector.ingest(encodeEvent(eventFilesystemRead, 10, "/repo/a.go"))
	collector.ingest([]byte{0, 1, 2})
	collector.ingest(encodeEvent(eventMetadataQuery, 10, "/repo/b.go"))
	collector.ingest(encodeEvent(eventDirectoryEnumeration, 10, "/repo/c"))
	snapshot := collector.finalize()
	if snapshot.RawEventCount != 2 || !snapshot.Truncated || collector.malformed != 1 {
		t.Fatalf("snapshot=%#v malformed=%d", snapshot, collector.malformed)
	}
	if len(snapshot.Resources) != 2 {
		t.Fatalf("resources=%#v", snapshot.Resources)
	}
	before := collector.rawEvents
	collector.ingest(encodeEvent(eventFilesystemWrite, 10, "/repo/after"))
	if collector.rawEvents != before {
		t.Fatal("send/ingest after finalize resurrected collector")
	}
	if _, err := os.Lstat(collector.socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket survived finalize: %v", err)
	}
}

func TestE27CollectorMarksDurationAndPrivateByteBudgetTruncated(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits collectorLimits
		age    time.Duration
		path   string
	}{
		{"duration", collectorLimits{maxEvents: 10, maxUnique: 10, maxPrivateBytes: 1 << 20, maxDuration: time.Nanosecond}, time.Second, "/repo/a"},
		{"bytes", collectorLimits{maxEvents: 10, maxUnique: 10, maxPrivateBytes: 16, maxDuration: time.Minute}, 0, "/repo/a-very-long-path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(e27PrivateState(t), "trace")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			collector, err := newCollector(root, e27TestSocketRoot(t), "trace_01K00000000000000000000000", tc.limits)
			if err != nil {
				t.Fatal(err)
			}
			collector.startedAt = collector.startedAt.Add(-tc.age)
			collector.ingest(encodeEvent(eventFilesystemRead, 10, tc.path))
			snapshot := collector.finalize()
			if !snapshot.Truncated {
				t.Fatalf("snapshot=%#v", snapshot)
			}
		})
	}
}

func TestE27CollectorRejectsOversizedAndUnknownEvents(t *testing.T) {
	root := filepath.Join(e27PrivateState(t), "trace")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	collector, err := newCollector(root, e27TestSocketRoot(t), "trace_01K00000000000000000000000", defaultCollectorLimits())
	if err != nil {
		t.Fatal(err)
	}
	collector.ingest(encodeEvent(eventClass(255), 1, "/x"))
	collector.ingest(encodeEvent(eventFilesystemRead, 1, "/"+string(make([]byte, trace.MaxRawEventBytes+1))))
	snapshot := collector.finalize()
	if snapshot.RawEventCount != 0 || collector.malformed < 1 {
		t.Fatalf("snapshot=%#v malformed=%d", snapshot, collector.malformed)
	}
}

func e27TestSocketRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "sb-e27-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}
