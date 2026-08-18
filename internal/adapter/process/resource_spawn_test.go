//go:build linux || darwin

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type task5Sink struct{}

func (task5Sink) Append(context.Context, []byte) error { return nil }
func (task5Sink) CaptureFailed(error)                  {}

type task5FailingResourceProvider struct {
	calls int
	err   error
}

func (p *task5FailingResourceProvider) prepareExecution(operation.ResourceLimits) (resourceExecutionDomain, error) {
	p.calls++
	return nil, p.err
}
func (*task5FailingResourceProvider) support() capability.ResourceEnforcementSupport {
	return capability.ResourceEnforcementSupport{}
}

var _ app.OutputSink = task5Sink{}

func TestResourceOwnerPreparationFailurePreventsChildSpawn(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "should-not-exist")
	provider := &task5FailingResourceProvider{err: errors.New("prepare failed")}
	owner := Owner{resources: provider}
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	h, spawn, err := owner.Start(context.Background(), operation.ExecutionSpec{
		Shell: "/bin/sh", Command: "touch " + marker, CWD: root, ResourceLimits: limits,
	}, task5Sink{})
	if err == nil {
		if h != nil {
			_ = h.Close()
		}
		t.Fatal("resource preparation failure still spawned a child")
	}
	if provider.calls != 1 {
		t.Fatalf("provider prepare calls=%d want=1", provider.calls)
	}
	if spawn.Attempted || spawn.Succeeded {
		t.Fatalf("provider preflight was reported as child spawn: %#v", spawn)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("child side effect occurred despite prepare failure: %v", statErr)
	}
}

func TestResourceOwnerNoLimitsHasZeroProviderWork(t *testing.T) {
	root := t.TempDir()
	provider := &task5FailingResourceProvider{err: errors.New("must not be called")}
	owner := Owner{resources: provider}
	h, spawn, err := owner.Start(context.Background(), operation.ExecutionSpec{
		Shell: "/bin/sh", Command: "true", CWD: root,
	}, task5Sink{})
	if err != nil || !spawn.Succeeded || h == nil {
		t.Fatalf("ordinary start changed: spawn=%#v err=%v", spawn, err)
	}
	_ = h.Wait(context.Background())
	_ = h.Close()
	if provider.calls != 0 {
		t.Fatalf("ordinary start touched resource provider %d times", provider.calls)
	}
}

func TestFrozenOwnerRejectsResourceLimitsBeforeSpawn(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "frozen-should-not-exist")
	limits := &operation.ResourceLimits{Processes: 2}
	h, spawn, err := (FrozenOwner{}).Start(context.Background(), operation.ExecutionSpec{
		Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh",
		Command: "touch " + marker, CWD: root, ResourceLimits: limits,
	}, task5Sink{})
	if err == nil {
		if h != nil {
			_ = h.Close()
		}
		t.Fatal("frozen owner silently ignored resource limits")
	}
	if spawn.Attempted || spawn.Succeeded {
		t.Fatalf("frozen resource refusal reported child spawn: %#v", spawn)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("frozen child side effect occurred: %v", statErr)
	}
}

type task5ResourceProvider struct {
	calls  int
	domain resourceExecutionDomain
	err    error
}

func (p *task5ResourceProvider) prepareExecution(operation.ResourceLimits) (resourceExecutionDomain, error) {
	p.calls++
	return p.domain, p.err
}
func (*task5ResourceProvider) support() capability.ResourceEnforcementSupport {
	return capability.ResourceEnforcementSupport{}
}

type task5Binding struct{ closed int }

func (b *task5Binding) Close() error { b.closed++; return nil }

type task5Domain struct {
	bindCalls       int
	monitorCalls    int
	finishCalls     int
	abortCalls      int
	binding         task5Binding
	cmd             *exec.Cmd
	sawSetpgid      bool
	sawPTYLifecycle bool
	breach          operation.ResourceLimitKind
}

func (d *task5Domain) bind(cmd *exec.Cmd) (resourceSpawnBinding, error) {
	d.bindCalls++
	d.cmd = cmd
	if cmd.SysProcAttr != nil {
		d.sawSetpgid = cmd.SysProcAttr.Setpgid
	}
	cmd.Env = append(cmd.Environ(), "TASK5_RESOURCE_BOUND=1")
	return &d.binding, nil
}
func (d *task5Domain) startMonitoring() {
	d.monitorCalls++
	if d.cmd != nil && d.cmd.SysProcAttr != nil {
		d.sawPTYLifecycle = d.cmd.SysProcAttr.Setsid && d.cmd.SysProcAttr.Setctty
	}
}
func (d *task5Domain) finish() (operation.ResourceLimitKind, error) {
	d.finishCalls++
	return d.breach, nil
}
func (d *task5Domain) abort() error { d.abortCalls++; return nil }

func TestResourceOwnerSuccessfulLimitedSpawnBindsBeforeStartAndFreezesBeforeWait(t *testing.T) {
	domain := &task5Domain{breach: operation.ResourceLimitMemory}
	provider := &task5ResourceProvider{domain: domain}
	owner := Owner{resources: provider}
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	h, spawn, err := owner.Start(context.Background(), operation.ExecutionSpec{
		Shell: "/bin/sh", Command: `[ "$TASK5_RESOURCE_BOUND" = 1 ]`, CWD: t.TempDir(), ResourceLimits: limits,
	}, task5Sink{})
	if err != nil || !spawn.Succeeded || h == nil {
		t.Fatalf("limited start spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit=%#v", exit)
	}
	if provider.calls != 1 || domain.bindCalls != 1 || domain.monitorCalls != 1 || domain.finishCalls != 1 || domain.abortCalls != 0 {
		t.Fatalf("lifecycle provider=%d bind=%d monitor=%d finish=%d abort=%d", provider.calls, domain.bindCalls, domain.monitorCalls, domain.finishCalls, domain.abortCalls)
	}
	if domain.binding.closed != 1 {
		t.Fatalf("transient binding closed=%d want=1", domain.binding.closed)
	}
	if !domain.sawSetpgid {
		t.Fatal("resource bind did not preserve non-TTY Setpgid placement")
	}
	status, ok := h.(interface {
		ResourceLimitBreach() operation.ResourceLimitKind
	})
	if !ok || status.ResourceLimitBreach() != operation.ResourceLimitMemory {
		t.Fatalf("resource breach status=%T %#v", h, status)
	}
	_ = h.Close()
}

func TestResourceOwnerFailedOSSpawnClosesBindingAndAbortsDomain(t *testing.T) {
	domain := &task5Domain{}
	provider := &task5ResourceProvider{domain: domain}
	owner := Owner{resources: provider}
	limits := &operation.ResourceLimits{Processes: 2}
	h, spawn, err := owner.Start(context.Background(), operation.ExecutionSpec{
		Shell: "/definitely/not/a/shell", Command: "true", CWD: t.TempDir(), ResourceLimits: limits,
	}, task5Sink{})
	if err == nil || h != nil || spawn.Succeeded {
		t.Fatalf("missing executable start h=%T spawn=%#v err=%v", h, spawn, err)
	}
	if domain.bindCalls != 1 || domain.binding.closed != 1 || domain.abortCalls != 1 || domain.monitorCalls != 0 || domain.finishCalls != 0 {
		t.Fatalf("failed-spawn lifecycle bind=%d close=%d abort=%d monitor=%d finish=%d", domain.bindCalls, domain.binding.closed, domain.abortCalls, domain.monitorCalls, domain.finishCalls)
	}
}

func TestResourceOwnerPTYKeepsSessionAndControllingTTYAttributes(t *testing.T) {
	domain := &task5Domain{}
	provider := &task5ResourceProvider{domain: domain}
	owner := Owner{resources: provider}
	limits := &operation.ResourceLimits{Processes: 2}
	h, spawn, err := owner.Start(context.Background(), operation.ExecutionSpec{
		Shell: "/bin/sh", Command: "true", CWD: t.TempDir(), TTY: true, ResourceLimits: limits,
	}, task5Sink{})
	if err != nil || !spawn.Succeeded || h == nil {
		t.Fatalf("PTY limited start spawn=%#v err=%v", spawn, err)
	}
	_ = h.Wait(context.Background())
	if !domain.sawPTYLifecycle {
		t.Fatalf("PTY attrs after start=%#v", domain.cmd.SysProcAttr)
	}
	if domain.bindCalls != 1 || domain.binding.closed != 1 || domain.finishCalls != 1 {
		t.Fatalf("PTY lifecycle bind=%d close=%d finish=%d", domain.bindCalls, domain.binding.closed, domain.finishCalls)
	}
	_ = h.Close()
}

type task5CleanupFailDomain struct {
	task5Domain
	finishErr error
}

func (d *task5CleanupFailDomain) finish() (operation.ResourceLimitKind, error) {
	d.finishCalls++
	return d.breach, d.finishErr
}

func TestResourceOwnerFreezesCleanupIncompleteSeparatelyFromLiteralExit(t *testing.T) {
	domain := &task5CleanupFailDomain{finishErr: resourceProviderFailure("cleanup_remove_failed")}
	provider := &task5ResourceProvider{domain: domain}
	owner := Owner{resources: provider}
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	h, spawn, err := owner.Start(context.Background(), operation.ExecutionSpec{
		Shell: "/bin/sh", Command: "true", CWD: t.TempDir(), ResourceLimits: limits,
	}, task5Sink{})
	if err != nil || !spawn.Succeeded || h == nil {
		t.Fatalf("start spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if exit.Code == nil || *exit.Code != 0 || exit.Signal != "" {
		t.Fatalf("cleanup defect rewrote exit evidence: %#v", exit)
	}
	status, ok := h.(interface{ ResourceCleanupIncomplete() string })
	if !ok || status.ResourceCleanupIncomplete() != "cleanup_remove_failed" {
		t.Fatalf("cleanup status=%T %#v", h, status)
	}
	breach, ok := h.(interface {
		ResourceLimitBreach() operation.ResourceLimitKind
	})
	if !ok || breach.ResourceLimitBreach() != "" {
		t.Fatalf("cleanup defect invented breach: %T %#v", h, breach)
	}
	_ = h.Close()
}
