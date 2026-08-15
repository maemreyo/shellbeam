package process

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

type fakeResolver struct {
	values map[string]core.SessionResolution
}

func (f fakeResolver) ResolveProcessSession(_ context.Context, id string) (core.SessionResolution, error) {
	return f.values[id], nil
}

type fakeHost struct {
	facts             map[int]core.ProcessFact
	children          map[int][]int
	errors            map[int]error
	observeCalls      []int
	childrenCalls     int
	childrenTruncated bool
}

func (f *fakeHost) Observe(_ context.Context, pid int) (core.ProcessFact, error) {
	f.observeCalls = append(f.observeCalls, pid)
	if err := f.errors[pid]; err != nil {
		return core.ProcessFact{}, err
	}
	fact, ok := f.facts[pid]
	if !ok {
		return core.ProcessFact{}, failure.New(failure.ProcessNotFound, map[string]string{"pid": fmt.Sprint(pid)}, nil)
	}
	return fact, nil
}
func (f *fakeHost) Children(_ context.Context, parents []int) (map[int][]int, bool, error) {
	f.childrenCalls++
	out := make(map[int][]int, len(parents))
	for _, parent := range parents {
		out[parent] = append([]int(nil), f.children[parent]...)
	}
	return out, f.childrenTruncated, nil
}

func testFact(t *testing.T, pid, ppid int, executable string) core.ProcessFact {
	t.Helper()
	start := time.Date(2026, 8, 15, 10, 0, pid%60, 0, time.UTC)
	identity, err := core.NewIdentity(pid, start, executable)
	if err != nil {
		t.Fatal(err)
	}
	return core.ProcessFact{PID: pid, ParentPID: ppid, Identity: &identity, Relation: core.RelationExternal, State: core.StateSleeping, StartTime: start, ExecutableIdentity: executable, ArgvView: &core.ArgvView{ExecutableIdentity: executable}}
}

func TestInspectSessionUsesCurrentResolverAndParentBeforeChildTraversal(t *testing.T) {
	host := &fakeHost{facts: map[int]core.ProcessFact{}, children: map[int][]int{10: {12, 11}, 11: {13}}}
	for pid, ppid := range map[int]int{10: 1, 11: 10, 12: 10, 13: 11} {
		host.facts[pid] = testFact(t, pid, ppid, fmt.Sprintf("/bin/p%d", pid))
	}
	svc := NewService(host, fakeResolver{values: map[string]core.SessionResolution{"s1": {SessionID: "s1", Known: true, Current: true, PID: 10, State: "running"}}}, Options{Now: func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }})
	got, err := svc.Inspect(context.Background(), Request{Target: core.Target{Kind: core.TargetSession, SessionID: "s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Root == nil || got.Root.PID != 10 || got.Root.Relation != core.RelationShellBeamRoot || got.Quality != core.QualityComplete || got.Truncated {
		t.Fatalf("root=%#v quality=%q truncated=%v", got.Root, got.Quality, got.Truncated)
	}
	want := []int{11, 12, 13}
	if len(got.Descendants) != len(want) {
		t.Fatalf("descendants=%#v", got.Descendants)
	}
	for i, pid := range want {
		if got.Descendants[i].PID != pid || got.Descendants[i].Relation != core.RelationShellBeamDescendant {
			t.Fatalf("descendant[%d]=%#v", i, got.Descendants[i])
		}
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("invalid observation: %v", err)
	}
}

func TestInspectKnownNonCurrentSessionNeverProbesPersistedPID(t *testing.T) {
	host := &fakeHost{facts: map[int]core.ProcessFact{}, children: map[int][]int{}}
	svc := NewService(host, fakeResolver{values: map[string]core.SessionResolution{"old": {SessionID: "old", Known: true, Current: false, PID: 0, State: "completed"}}}, Options{})
	got, err := svc.Inspect(context.Background(), Request{Target: core.Target{Kind: core.TargetSession, SessionID: "old"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Quality != core.QualityUnavailable || got.Root != nil || len(host.observeCalls) != 0 || len(got.DiagnosticCodes) == 0 {
		t.Fatalf("observation=%#v calls=%v", got, host.observeCalls)
	}
}

func TestInspectUnknownSessionIsNotArbitraryProcessNotFound(t *testing.T) {
	svc := NewService(&fakeHost{}, fakeResolver{values: map[string]core.SessionResolution{}}, Options{})
	_, err := svc.Inspect(context.Background(), Request{Target: core.Target{Kind: core.TargetSession, SessionID: "missing"}})
	if err == nil || errors.Is(err, failure.ProcessNotFound) {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestInspectExplicitPIDPreservesAccessDeniedAndExternalRelation(t *testing.T) {
	t.Run("access denied", func(t *testing.T) {
		host := &fakeHost{errors: map[int]error{99: failure.New(failure.ProcessAccessDenied, map[string]string{"pid": "99"}, nil)}}
		_, err := NewService(host, nil, Options{}).Inspect(context.Background(), Request{Target: core.Target{Kind: core.TargetPID, PID: 99}})
		if !errors.Is(err, failure.ProcessAccessDenied) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("external", func(t *testing.T) {
		host := &fakeHost{facts: map[int]core.ProcessFact{99: testFact(t, 99, 1, "/bin/external")}, children: map[int][]int{}}
		got, err := NewService(host, nil, Options{}).Inspect(context.Background(), Request{Target: core.Target{Kind: core.TargetPID, PID: 99}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Root == nil || got.Root.Relation != core.RelationExternal {
			t.Fatalf("root=%#v", got.Root)
		}
	})
}

func TestInspectEnforcesDescendantAndByteBounds(t *testing.T) {
	t.Run("descendant bound", func(t *testing.T) {
		host := &fakeHost{facts: map[int]core.ProcessFact{1: testFact(t, 1, 0, "/bin/root")}, children: map[int][]int{}, childrenTruncated: true}
		for pid := 2; pid <= core.MaxDescendants+2; pid++ {
			host.facts[pid] = testFact(t, pid, 1, "/bin/child")
			host.children[1] = append(host.children[1], pid)
		}
		got, err := NewService(host, nil, Options{}).Inspect(context.Background(), Request{Target: core.Target{Kind: core.TargetPID, PID: 1}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Descendants) != core.MaxDescendants || !got.Truncated || got.Quality != core.QualityPartial {
			t.Fatalf("count=%d truncated=%v quality=%q", len(got.Descendants), got.Truncated, got.Quality)
		}
	})
	t.Run("byte bound", func(t *testing.T) {
		large := "/" + strings.Repeat("x", 1000)
		host := &fakeHost{facts: map[int]core.ProcessFact{1: testFact(t, 1, 0, large)}, children: map[int][]int{}}
		for pid := 2; pid < 100; pid++ {
			host.facts[pid] = testFact(t, pid, 1, large)
			host.children[1] = append(host.children[1], pid)
		}
		got, err := NewService(host, nil, Options{}).Inspect(context.Background(), Request{Target: core.Target{Kind: core.TargetPID, PID: 1}})
		if err != nil {
			t.Fatal(err)
		}
		if !got.Truncated || len(got.Descendants) >= 99 {
			t.Fatalf("count=%d truncated=%v", len(got.Descendants), got.Truncated)
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("bounded result invalid: %v", err)
		}
	})
}
