package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type p13LiveShape struct {
	Sessions          int
	Panes             int
	Clients           int
	ServerDescendants int
	RootEntries       int
}

func inspectP13LiveShape(ctx context.Context, f *nativeFixture) (p13LiveShape, error) {
	if f == nil {
		return p13LiveShape{}, errors.New("nil native fixture")
	}
	sessionsOut, err := f.tmux(ctx, "list-sessions", "-F", "#{session_id}")
	if err != nil {
		return p13LiveShape{}, err
	}
	panes, err := f.paneIDs(ctx, f.Session)
	if err != nil {
		return p13LiveShape{}, err
	}
	clients, err := f.clients(ctx)
	if err != nil {
		return p13LiveShape{}, err
	}
	server, err := f.serverIdentity(ctx)
	if err != nil {
		return p13LiveShape{}, err
	}
	descendants, err := processDescendantCount(ctx, server.PID)
	if err != nil {
		return p13LiveShape{}, err
	}
	rootEntries, err := countTreeEntries(f.Root)
	if err != nil {
		return p13LiveShape{}, err
	}
	return p13LiveShape{
		Sessions:          countNonEmptyLines(string(sessionsOut)),
		Panes:             len(panes),
		Clients:           len(clients),
		ServerDescendants: descendants,
		RootEntries:       rootEntries,
	}, nil
}

func processDescendantCount(ctx context.Context, rootPID int) (int, error) {
	if rootPID <= 0 {
		return 0, errors.New("root PID must be positive")
	}
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return 0, fmt.Errorf("ps process tree: %w", err)
	}
	return countDescendantsFromPS(string(out), rootPID)
}

func countDescendantsFromPS(text string, rootPID int) (int, error) {
	children := make(map[int][]int)
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return 0, fmt.Errorf("invalid ps row %q", line)
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			return 0, fmt.Errorf("invalid ps pid %q", fields[0])
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid < 0 {
			return 0, fmt.Errorf("invalid ps ppid %q", fields[1])
		}
		children[ppid] = append(children[ppid], pid)
	}
	seen := map[int]bool{rootPID: true}
	stack := append([]int(nil), children[rootPID]...)
	count := 0
	for len(stack) != 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		count++
		stack = append(stack, children[pid]...)
	}
	return count, nil
}

func countTreeEntries(root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			count++
		}
		return nil
	})
	return count, err
}

func countNonEmptyLines(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

type selfResourceSnapshot struct {
	FDs        int
	Goroutines int
}

func validateP13LiveShape(shape p13LiveShape) error {
	if shape.Sessions != 1 || shape.Panes != 1 || shape.Clients != 2 {
		return fmt.Errorf("unexpected live tmux shape: %#v", shape)
	}
	if shape.ServerDescendants < 1 {
		return fmt.Errorf("private server has no observed workload descendant: %#v", shape)
	}
	if shape.RootEntries < 1 {
		return fmt.Errorf("private fixture root has no live entries: %#v", shape)
	}
	return nil
}

func sampleSelfResources() (selfResourceSnapshot, error) {
	fds, err := selfFDCount()
	if err != nil {
		return selfResourceSnapshot{}, err
	}
	return selfResourceSnapshot{FDs: fds, Goroutines: runtime.NumGoroutine()}, nil
}

func waitSelfResourceConvergence(ctx context.Context, baseline selfResourceSnapshot, timeout time.Duration) (selfResourceSnapshot, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var last selfResourceSnapshot
	for {
		current, err := sampleSelfResources()
		if err != nil {
			return last, err
		}
		last = current
		if current.FDs <= baseline.FDs && current.Goroutines <= baseline.Goroutines {
			return current, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-deadline.C:
			return last, fmt.Errorf("self resources did not converge: baseline=%#v current=%#v", baseline, last)
		case <-ticker.C:
		}
	}
}

func maxSelfResourceSnapshot(a, b selfResourceSnapshot) selfResourceSnapshot {
	if b.FDs > a.FDs {
		a.FDs = b.FDs
	}
	if b.Goroutines > a.Goroutines {
		a.Goroutines = b.Goroutines
	}
	return a
}

func maxP13LiveShape(a, b p13LiveShape) p13LiveShape {
	if b.Sessions > a.Sessions {
		a.Sessions = b.Sessions
	}
	if b.Panes > a.Panes {
		a.Panes = b.Panes
	}
	if b.Clients > a.Clients {
		a.Clients = b.Clients
	}
	if b.ServerDescendants > a.ServerDescendants {
		a.ServerDescendants = b.ServerDescendants
	}
	if b.RootEntries > a.RootEntries {
		a.RootEntries = b.RootEntries
	}
	return a
}
