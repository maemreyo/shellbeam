//go:build linux

package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

func platformProcessSnapshot(ctx context.Context, pid int) (processSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return processSnapshot{}, err
	}
	base := filepath.Join("/proc", strconv.Itoa(pid))
	statData, err := os.ReadFile(filepath.Join(base, "stat"))
	if os.IsNotExist(err) {
		return processSnapshot{}, failure.New(failure.ProcessNotFound, map[string]string{"pid": strconv.Itoa(pid)}, err)
	}
	if os.IsPermission(err) {
		return processSnapshot{}, failure.New(failure.ProcessAccessDenied, map[string]string{"pid": strconv.Itoa(pid)}, err)
	}
	if err != nil {
		return processSnapshot{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": strconv.Itoa(pid), "reason": "proc_stat_unavailable"}, err)
	}
	state, ppid, startToken, err := parseLinuxStat(string(statData))
	if err != nil {
		return processSnapshot{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": strconv.Itoa(pid), "reason": "proc_stat_invalid"}, err)
	}
	statusData, err := os.ReadFile(filepath.Join(base, "status"))
	if os.IsPermission(err) {
		return processSnapshot{}, failure.New(failure.ProcessAccessDenied, map[string]string{"pid": strconv.Itoa(pid)}, err)
	}
	if err != nil {
		return processSnapshot{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": strconv.Itoa(pid), "reason": "proc_status_unavailable"}, err)
	}
	uid, err := parseLinuxUID(string(statusData))
	if err != nil {
		return processSnapshot{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": strconv.Itoa(pid), "reason": "proc_status_invalid"}, err)
	}
	executable, _ := os.Readlink(filepath.Join(base, "exe"))
	if len(executable) > 1024 {
		executable = executable[:1024]
	}
	return processSnapshot{PID: pid, ParentPID: ppid, UID: uid, State: state, StartToken: "proc-start-ticks:" + startToken, ExecutableIdentity: executable}, nil
}

func platformProcessChildren(ctx context.Context, parents []int) (map[int][]int, bool, error) {
	parentSet := make(map[int]struct{}, len(parents))
	for _, pid := range parents {
		if pid > 0 {
			parentSet[pid] = struct{}{}
		}
	}
	result := make(map[int][]int, len(parentSet))
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false, failure.New(failure.ProcessObservationIncomplete, map[string]string{"reason": "proc_enumeration_unavailable"}, err)
	}
	count := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, true, nil
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		_, ppid, _, err := parseLinuxStat(string(data))
		if err != nil {
			continue
		}
		if _, ok := parentSet[ppid]; !ok {
			continue
		}
		result[ppid] = append(result[ppid], pid)
		count++
		if count > core.MaxDescendants {
			return result, true, nil
		}
	}
	for parent := range result {
		sort.Ints(result[parent])
	}
	return result, false, nil
}

func parseLinuxStat(value string) (core.State, int, string, error) {
	closeIndex := strings.LastIndex(value, ")")
	if closeIndex < 0 || closeIndex+2 >= len(value) {
		return core.StateUnknown, 0, "", fmt.Errorf("malformed proc stat")
	}
	fields := strings.Fields(value[closeIndex+2:])
	if len(fields) <= 19 {
		return core.StateUnknown, 0, "", fmt.Errorf("short proc stat")
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return core.StateUnknown, 0, "", err
	}
	return processState(fields[0]), ppid, fields[19], nil
}

func parseLinuxUID(value string) (int, error) {
	for _, line := range strings.Split(value, "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		return strconv.Atoi(fields[1])
	}
	return 0, fmt.Errorf("uid missing")
}

func processState(value string) core.State {
	if value == "" {
		return core.StateUnknown
	}
	switch value[0] {
	case 'R':
		return core.StateRunning
	case 'S', 'D', 'I':
		return core.StateSleeping
	case 'T', 't':
		return core.StateStopped
	case 'Z':
		return core.StateZombie
	default:
		return core.StateUnknown
	}
}
