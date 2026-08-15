//go:build linux || darwin

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

type processSnapshot struct {
	PID                int
	ParentPID          int
	UID                int
	State              core.State
	StartTime          time.Time
	StartToken         string
	ExecutableIdentity string
}

type HostInspector struct {
	signal0    func(int) error
	currentUID func() int
	snapshot   func(context.Context, int) (processSnapshot, error)
	children   func(context.Context, []int) (map[int][]int, bool, error)
}

func NewHostInspector() *HostInspector {
	return &HostInspector{
		signal0:    func(pid int) error { return syscall.Kill(pid, 0) },
		currentUID: os.Getuid,
		snapshot:   platformProcessSnapshot,
		children:   platformProcessChildren,
	}
}

func (h *HostInspector) Observe(ctx context.Context, pid int) (core.ProcessFact, error) {
	if pid <= 0 {
		return core.ProcessFact{}, failure.New(failure.ProcessNotFound, map[string]string{"pid": fmt.Sprint(pid)}, nil)
	}
	signal0 := h.signal0
	if signal0 == nil {
		signal0 = func(pid int) error { return syscall.Kill(pid, 0) }
	}
	if err := signal0(pid); err != nil {
		return core.ProcessFact{}, classifyProcessProbeError(pid, err)
	}
	snapshotFn := h.snapshot
	if snapshotFn == nil {
		snapshotFn = platformProcessSnapshot
	}
	first, err := snapshotFn(ctx, pid)
	if err != nil {
		return core.ProcessFact{}, err
	}
	currentUID := h.currentUID
	if currentUID == nil {
		currentUID = os.Getuid
	}
	if first.UID != currentUID() {
		return core.ProcessFact{}, failure.New(failure.ProcessAccessDenied, map[string]string{"pid": fmt.Sprint(pid)}, nil)
	}
	second, err := snapshotFn(ctx, pid)
	if err != nil {
		if errors.Is(err, failure.ProcessNotFound) {
			return core.ProcessFact{}, failure.New(failure.ProcessIdentityChanged, map[string]string{"pid": fmt.Sprint(pid)}, err)
		}
		return core.ProcessFact{}, err
	}
	if first.PID != second.PID || first.StartToken != second.StartToken || !first.StartTime.Equal(second.StartTime) {
		return core.ProcessFact{}, failure.New(failure.ProcessIdentityChanged, map[string]string{"pid": fmt.Sprint(pid)}, nil)
	}
	identity, err := processIdentity(first)
	if err != nil {
		return core.ProcessFact{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": fmt.Sprint(pid), "reason": "identity_unavailable"}, err)
	}
	fact := core.ProcessFact{
		PID: pid, ParentPID: first.ParentPID, Identity: &identity,
		Relation: core.RelationExternal, State: first.State, StartTime: first.StartTime,
		ExecutableIdentity: first.ExecutableIdentity,
		ArgvView:           &core.ArgvView{ExecutableIdentity: first.ExecutableIdentity, ArgumentCount: 0},
	}
	return fact, nil
}

func (h *HostInspector) Children(ctx context.Context, parents []int) (map[int][]int, bool, error) {
	children := h.children
	if children == nil {
		children = platformProcessChildren
	}
	return children(ctx, append([]int(nil), parents...))
}

func classifyProcessProbeError(pid int, err error) error {
	switch {
	case errors.Is(err, syscall.ESRCH):
		return failure.New(failure.ProcessNotFound, map[string]string{"pid": fmt.Sprint(pid)}, err)
	case errors.Is(err, syscall.EPERM):
		return failure.New(failure.ProcessAccessDenied, map[string]string{"pid": fmt.Sprint(pid)}, err)
	default:
		return failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": fmt.Sprint(pid), "reason": "host_probe_failed"}, err)
	}
}

func processIdentity(snapshot processSnapshot) (core.Identity, error) {
	if !snapshot.StartTime.IsZero() {
		return core.NewIdentity(snapshot.PID, snapshot.StartTime, snapshot.ExecutableIdentity)
	}
	if snapshot.StartToken != "" {
		return core.NewIdentityFromToken(snapshot.PID, snapshot.StartToken, snapshot.ExecutableIdentity)
	}
	return core.Identity{}, fmt.Errorf("process start identity unavailable")
}
