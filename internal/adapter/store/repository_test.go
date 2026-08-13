package store

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"path/filepath"
	"sync"
	"testing"
)

func TestReserveOperationIsIdempotent(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "op1", SessionID: "s1", Fingerprint: "abc", Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "d1"}
	var wg sync.WaitGroup
	created := 0
	var mu sync.Mutex
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fresh, result := r.ReserveOperation(context.Background(), res)
			if result.Err != nil {
				t.Error(result.Err)
				return
			}
			if fresh {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("created=%d", created)
	}
	conflict := res
	conflict.Fingerprint = "other"
	if _, _, got := r.ReserveOperation(context.Background(), conflict); got.Err == nil {
		t.Fatal("expected conflict")
	}
}

func TestOutputCursorAndLimit(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 1, MaxSessionOutput: 5, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	res := operation.Reservation{SchemaVersion: 1, OperationID: "op", SessionID: "s", Fingerprint: "x", Command: "x", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "d"}
	if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := r.AppendOutput(context.Background(), "s", []byte("hello")); got.Err != nil {
		t.Fatal(got.Err)
	}
	b, next, err := r.ReadOutput(context.Background(), "s", 1, 2)
	if err != nil || string(b) != "el" || next != 3 {
		t.Fatalf("%q %d %v", b, next, err)
	}
	if _, got := r.AppendOutput(context.Background(), "s", []byte("!")); got.Err == nil {
		t.Fatal("limit not enforced")
	}
}
