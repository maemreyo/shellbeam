//go:build darwin

package process

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

const maxDarwinProcessOutputBytes = 256 << 10

func platformProcessSnapshot(ctx context.Context, pid int) (processSnapshot, error) {
	output, truncated, err := runBoundedProcessCommand(ctx, maxDarwinProcessOutputBytes, "ps", "-p", strconv.Itoa(pid), "-o", "uid=,ppid=,state=,lstart=,comm=")
	if err != nil {
		return processSnapshot{}, classifyProcessProbeError(pid, err)
	}
	if truncated {
		return processSnapshot{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": strconv.Itoa(pid), "reason": "process_record_too_large"}, nil)
	}
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) < 9 {
		return processSnapshot{}, failure.New(failure.ProcessNotFound, map[string]string{"pid": strconv.Itoa(pid)}, nil)
	}
	uid, err := strconv.Atoi(fields[0])
	if err != nil {
		return processSnapshot{}, parseProcessFailure(pid, err)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return processSnapshot{}, parseProcessFailure(pid, err)
	}
	start, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(fields[3:8], " "), time.Local)
	if err != nil {
		return processSnapshot{}, parseProcessFailure(pid, err)
	}
	executable := strings.Join(fields[8:], " ")
	if len(executable) > 1024 {
		executable = executable[:1024]
	}
	return processSnapshot{PID: pid, ParentPID: ppid, UID: uid, State: processState(fields[2]), StartTime: start.UTC(), ExecutableIdentity: executable}, nil
}

func platformProcessChildren(ctx context.Context, parents []int) (map[int][]int, bool, error) {
	parentSet := make(map[int]struct{}, len(parents))
	for _, pid := range parents {
		if pid > 0 {
			parentSet[pid] = struct{}{}
		}
	}
	result := make(map[int][]int, len(parentSet))
	if len(parentSet) == 0 {
		return result, false, nil
	}
	output, truncated, err := runBoundedProcessCommand(ctx, maxDarwinProcessOutputBytes, "ps", "-axo", "pid=,ppid=")
	if err != nil {
		return nil, false, failure.New(failure.ProcessObservationIncomplete, map[string]string{"reason": "process_table_unavailable"}, err)
	}
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		if _, ok := parentSet[ppid]; !ok {
			continue
		}
		result[ppid] = append(result[ppid], pid)
		count++
		if count > core.MaxDescendants {
			truncated = true
			break
		}
	}
	for parent := range result {
		sort.Ints(result[parent])
	}
	return result, truncated, nil
}

func processState(value string) core.State {
	if value == "" {
		return core.StateUnknown
	}
	switch value[0] {
	case 'R':
		return core.StateRunning
	case 'S', 'I', 'U':
		return core.StateSleeping
	case 'T':
		return core.StateStopped
	case 'Z':
		return core.StateZombie
	default:
		return core.StateUnknown
	}
}

func parseProcessFailure(pid int, err error) error {
	return failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": strconv.Itoa(pid), "reason": "process_record_invalid"}, err)
}

type boundedProcessBuffer struct {
	limit     int
	data      []byte
	truncated bool
}

func (b *boundedProcessBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		b.data = append(b.data, p[:remaining]...)
	}
	if len(p) > remaining {
		b.truncated = true
	}
	return n, nil
}

func runBoundedProcessCommand(ctx context.Context, limit int, name string, args ...string) ([]byte, bool, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, false, err
	}
	buffer := &boundedProcessBuffer{limit: limit}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	if err := cmd.Run(); err != nil {
		return nil, buffer.truncated, fmt.Errorf("process command failed: %w", err)
	}
	return append([]byte(nil), buffer.data...), buffer.truncated, nil
}
