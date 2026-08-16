//go:build linux || darwin

package main

import (
	"fmt"
	"sort"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
)

func TestB1NativePersistentStartLatencyReport(t *testing.T) {
	const samples = 32

	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	daemon := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer daemon.hardKill(t)

	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		operationID := fmt.Sprintf("b1-native-perf-%02d", i)
		startedAt := time.Now()
		started := callB1Native(t, daemon.client, ipcadapter.RequestV2{
			Action:         "start",
			OperationID:    operationID,
			CWD:            "/tmp",
			Command:        "sleep 30",
			Persistent:     true,
			SessionName:    fmt.Sprintf("b1-perf-%02d", i),
			YieldMS:        0,
			MaxOutputBytes: 1024,
		})
		durations = append(durations, time.Since(startedAt))
		if started.Result == nil || started.Result.Operation.SessionID == "" {
			t.Fatalf("sample %d missing persistent session", i)
		}
		sessionID := started.Result.Operation.SessionID
		callB1Native(t, daemon.client, ipcadapter.RequestV2{
			Action:    "kill",
			SessionID: sessionID,
			KillID:    operationID + "-cleanup",
			Signal:    "KILL",
		})
		_ = waitB1NativeTerminal(t, daemon.client, sessionID)
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[percentileIndex(len(durations), 50)]
	p95 := durations[percentileIndex(len(durations), 95)]
	p99 := durations[percentileIndex(len(durations), 99)]
	t.Logf("persistent start native host samples=%d p50=%s p95=%s p99=%s", len(durations), p50, p95, p99)
}

func percentileIndex(n, percentile int) int {
	if n <= 1 {
		return 0
	}
	index := (n*percentile + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > n {
		index = n
	}
	return index - 1
}
