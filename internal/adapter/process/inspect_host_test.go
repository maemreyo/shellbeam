package process

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

func TestHostInspectorObservesStableCurrentUserIdentity(t *testing.T) {
	start := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	snapshot := processSnapshot{PID: 42, ParentPID: 1, UID: 501, State: core.StateSleeping, StartTime: start, ExecutableIdentity: "/usr/bin/sleep"}
	calls := 0
	host := &HostInspector{
		signal0:    func(int) error { return nil },
		currentUID: func() int { return 501 },
		snapshot:   func(context.Context, int) (processSnapshot, error) { calls++; return snapshot, nil },
	}
	got, err := host.Observe(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || got.PID != 42 || got.ParentPID != 1 || got.Identity == nil || got.Identity.Value == "" || !got.StartTime.Equal(start) || got.ExecutableIdentity != "/usr/bin/sleep" || got.Relation != core.RelationExternal {
		t.Fatalf("observation=%#v calls=%d", got, calls)
	}
	if got.ArgvView == nil || got.ArgvView.ExecutableIdentity != "/usr/bin/sleep" || got.ArgvView.ArgumentCount != 0 {
		t.Fatalf("argv view=%#v", got.ArgvView)
	}
}

func TestHostInspectorDistinguishesNotFoundAccessDeniedAndIdentityChange(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		host := &HostInspector{signal0: func(int) error { return syscall.ESRCH }}
		_, err := host.Observe(context.Background(), 44)
		if !errors.Is(err, failure.ProcessNotFound) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("signal access denied", func(t *testing.T) {
		host := &HostInspector{signal0: func(int) error { return syscall.EPERM }}
		_, err := host.Observe(context.Background(), 44)
		if !errors.Is(err, failure.ProcessAccessDenied) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("uid access denied", func(t *testing.T) {
		host := &HostInspector{
			signal0: func(int) error { return nil }, currentUID: func() int { return 501 },
			snapshot: func(context.Context, int) (processSnapshot, error) {
				return processSnapshot{PID: 44, UID: 0, StartTime: time.Unix(1, 0)}, nil
			},
		}
		_, err := host.Observe(context.Background(), 44)
		if !errors.Is(err, failure.ProcessAccessDenied) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("identity changed", func(t *testing.T) {
		sequence := []processSnapshot{
			{PID: 44, UID: 501, StartTime: time.Unix(1, 0), State: core.StateRunning},
			{PID: 44, UID: 501, StartTime: time.Unix(2, 0), State: core.StateRunning},
		}
		host := &HostInspector{
			signal0: func(int) error { return nil }, currentUID: func() int { return 501 },
			snapshot: func(context.Context, int) (processSnapshot, error) {
				value := sequence[0]
				sequence = sequence[1:]
				return value, nil
			},
		}
		_, err := host.Observe(context.Background(), 44)
		if !errors.Is(err, failure.ProcessIdentityChanged) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestHostInspectorChildrenDelegatesBoundedPlatformEnumeration(t *testing.T) {
	host := &HostInspector{children: func(_ context.Context, parents []int) (map[int][]int, bool, error) {
		if len(parents) != 2 || parents[0] != 10 || parents[1] != 20 {
			t.Fatalf("parents=%v", parents)
		}
		return map[int][]int{10: {11, 12}, 20: {21}}, true, nil
	}}
	got, truncated, err := host.Children(context.Background(), []int{10, 20})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(got[10]) != 2 || len(got[20]) != 1 {
		t.Fatalf("got=%v truncated=%v", got, truncated)
	}
}

func TestHostInspectorObservesCurrentProcessOnRealHost(t *testing.T) {
	got, err := NewHostInspector().Observe(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != os.Getpid() || got.Identity == nil || got.Identity.Value == "" || got.ExecutableIdentity == "" {
		t.Fatalf("current process=%#v", got)
	}
}
