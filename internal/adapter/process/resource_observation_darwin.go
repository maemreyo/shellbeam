//go:build darwin

package process

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func platformMaxRSSBytes(state *os.ProcessState) (int64, bool) {
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage.Maxrss < 0 {
		return 0, false
	}
	// Darwin reports ru_maxrss in bytes.
	return usage.Maxrss, true
}

func platformProcessTreeCount(rootPID int) (int, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return 0, err
	}
	parents := make(map[int]int, len(processes))
	for i := range processes {
		parents[int(processes[i].Proc.P_pid)] = int(processes[i].Eproc.Ppid)
	}
	return countProcessTree(rootPID, parents), nil
}
