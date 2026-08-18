//go:build linux

package process

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func platformMaxRSSBytes(state *os.ProcessState) (int64, bool) {
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage.Maxrss < 0 || usage.Maxrss > math.MaxInt64/1024 {
		return 0, false
	}
	// Linux reports ru_maxrss in KiB.
	return usage.Maxrss * 1024, true
}

func platformProcessTreeCount(rootPID int) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	parents := make(map[int]int, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		parent, err := linuxProcessParent(string(data))
		if err == nil {
			parents[pid] = parent
		}
	}
	return countProcessTree(rootPID, parents), nil
}

func linuxProcessParent(value string) (int, error) {
	closeIndex := strings.LastIndex(value, ")")
	if closeIndex < 0 || closeIndex+2 >= len(value) {
		return 0, fmt.Errorf("malformed proc stat")
	}
	fields := strings.Fields(value[closeIndex+2:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("short proc stat")
	}
	return strconv.Atoi(fields[1])
}
