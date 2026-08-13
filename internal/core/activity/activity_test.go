package activity

import (
	"strings"
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestActivityIDIsCallerGeneratedBoundedAndPathSafe(t *testing.T) {
	for _, valid := range []string{"ZMR-111-validator", "task_2", "release:v1.2"} {
		if _, err := ParseID(valid); err != nil {
			t.Fatalf("ParseID(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "..", "a/b", "a\b", "bad\x00id", strings.Repeat("x", 129)} {
		if _, err := ParseID(invalid); err == nil {
			t.Fatalf("ParseID(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestActivityReusesWorkspacesAndCompactsOperationHistory(t *testing.T) {
	now := time.Now().UTC()
	record := New(ID("ZMR-111-validator"), now)
	ws1 := workspace.WorkspaceID("ws_01K00000000000000000000000")
	ws2 := workspace.WorkspaceID("ws_01K00000000000000000000001")
	for i, ws := range []workspace.WorkspaceID{ws1, ws1, ws2, ws1} {
		record.ObserveOperation(OperationRef{OperationID: string(rune(97 + i)), SessionID: string(rune(107 + i)), WorkspaceID: ws, ObservedAt: now.Add(time.Duration(i) * time.Second)}, 2)
	}
	if len(record.WorkspaceIDs) != 2 {
		t.Fatalf("workspaces=%v", record.WorkspaceIDs)
	}
	if len(record.Operations) != 2 || record.CompactedOperations != 2 {
		t.Fatalf("operations=%#v compacted=%d", record.Operations, record.CompactedOperations)
	}
	if record.Operations[0].OperationID != "c" || record.Operations[1].OperationID != "d" {
		t.Fatalf("operations=%#v", record.Operations)
	}
	if err := record.Validate(2); err != nil {
		t.Fatal(err)
	}
}

func TestActivityValidateRejectsUnboundedOperationHistory(t *testing.T) {
	now := time.Now().UTC()
	record := New(ID("bounded-history"), now)
	for i := 0; i < MaxOperationHistory+1; i++ {
		record.Operations = append(record.Operations, OperationRef{OperationID: "op", SessionID: "session", ObservedAt: now})
	}
	if err := record.Validate(0); err == nil {
		t.Fatal("unbounded operation history accepted")
	}
}
