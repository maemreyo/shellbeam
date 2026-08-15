//go:build linux || darwin

package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	processapp "github.com/maemreyo/shellbeam/internal/app/process"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
)

type failingA25PortRunner struct{ calls int }

func (r *failingA25PortRunner) Run(context.Context, []string, int) ([]byte, error) {
	r.calls++
	return nil, errors.New("forced lsof failure token=private")
}

func TestA25RealHostProcessInspectionKeepsPortsOptInAndFailureIsolated(t *testing.T) {
	runner := &failingA25PortRunner{}
	svc := processapp.NewService(processadapter.NewHostInspector(), nil, processapp.Options{Ports: processadapter.NewPortInspector(runner)})
	target := processcore.Target{Kind: processcore.TargetPID, PID: os.Getpid()}
	base, err := svc.Inspect(context.Background(), processapp.Request{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 || base.Root == nil || base.Root.PID != os.Getpid() || len(base.Ports) != 0 {
		t.Fatalf("base=%#v runner_calls=%d", base, runner.calls)
	}
	withPorts, err := svc.Inspect(context.Background(), processapp.Request{Target: target, IncludePorts: true})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || withPorts.Root == nil || withPorts.Root.PID != os.Getpid() || withPorts.Quality != processcore.QualityPartial || len(withPorts.Ports) != 0 || !hasA25Diagnostic(withPorts.DiagnosticCodes, processcore.DiagnosticPortUnavailable) {
		t.Fatalf("withPorts=%#v runner_calls=%d", withPorts, runner.calls)
	}
}

func hasA25Diagnostic(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
