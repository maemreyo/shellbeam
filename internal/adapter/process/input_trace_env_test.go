//go:build linux || darwin

package process

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestE27InputTraceOrdinaryCommandsLeaveExecEnvironmentNil(t *testing.T) {
	for _, spec := range []operation.ExecutionSpec{
		{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Command: "true", CWD: t.TempDir()},
		{Mode: operation.ExecutionModeArgv, Argv: []string{"/bin/echo", "ok"}, CWD: t.TempDir()},
	} {
		cmd, _, err := commandFor(spec)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyEnvironmentAdditions(cmd, spec.EnvironmentAdditions); err != nil {
			t.Fatal(err)
		}
		if cmd.Env != nil {
			t.Fatalf("ordinary command forced environment copy: %#v", cmd.Env)
		}
	}
}

func TestE27InputTraceInjectedEnvironmentReplacesTraceKeysAndPreservesInheritedValues(t *testing.T) {
	t.Setenv("SHELLBEAM_E27_INHERITED", "kept")
	t.Setenv("SHELLBEAM_TRACE_PROTOCOL", "stale")
	t.Setenv("SHELLBEAM_E27_ENV_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	traceEnv := e27TraceEnvironment()
	spec := operation.ExecutionSpec{
		Mode: operation.ExecutionModeArgv,
		Argv: []string{exe, "-test.run=TestE27InputTraceEnvironmentHelperProcess"},
		CWD:  t.TempDir(), EnvironmentAdditions: traceEnv,
	}
	cmd, _, err := commandFor(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyEnvironmentAdditions(cmd, spec.EnvironmentAdditions); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"SHELLBEAM_E27_INHERITED":  "kept",
		"SHELLBEAM_TRACE_ID":       "trace_01K00000000000000000000000",
		"SHELLBEAM_TRACE_PROTOCOL": "1",
		"SHELLBEAM_TRACE_SOCKET":   "/tmp/e27.sock",
		"DYLD_INSERT_LIBRARIES":    traceEnv[0].Value,
	} {
		if got := commandEnvironmentValue(cmd.Env, key); got != want {
			t.Fatalf("cmd.Env[%s]=%q want=%q", key, got, want)
		}
	}

	sink := &memorySink{}
	h, spawn, err := (Owner{}).Start(context.Background(), spec, sink)
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if !exit.Reaped || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit=%#v", exit)
	}
	sink.mu.Lock()
	got := string(sink.b)
	sink.mu.Unlock()
	marker := "E27_ENV=kept|trace_01K00000000000000000000000|1|/tmp/e27.sock|" + traceEnv[0].Value
	if !strings.Contains(got, marker) {
		t.Fatalf("environment output=%q missing %q", got, marker)
	}
}

func TestE27InputTraceEnvironmentHelperProcess(t *testing.T) {
	if os.Getenv("SHELLBEAM_E27_ENV_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString("E27_ENV=" + strings.Join([]string{
		os.Getenv("SHELLBEAM_E27_INHERITED"),
		os.Getenv("SHELLBEAM_TRACE_ID"),
		os.Getenv("SHELLBEAM_TRACE_PROTOCOL"),
		os.Getenv("SHELLBEAM_TRACE_SOCKET"),
		os.Getenv("DYLD_INSERT_LIBRARIES"),
	}, "|"))
}

func commandEnvironmentValue(environment []string, key string) string {
	prefix := key + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func TestE27InputTraceEnvironmentRejectsDuplicateOrUnsupportedKeysWithoutLeakingValues(t *testing.T) {
	secret := "TOP-SECRET-TRACE-VALUE"
	cases := []struct {
		name string
		env  []operation.EnvironmentEntry
	}{
		{"duplicate", []operation.EnvironmentEntry{{Key: "SHELLBEAM_TRACE_ID", Value: secret}, {Key: "SHELLBEAM_TRACE_ID", Value: "other"}}},
		{"unsupported", []operation.EnvironmentEntry{{Key: "HOME", Value: secret}}},
		{"nul", []operation.EnvironmentEntry{{Key: "SHELLBEAM_TRACE_ID", Value: secret + "\x00"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &memorySink{}
			_, spawn, err := (Owner{}).Start(context.Background(), operation.ExecutionSpec{Shell: "/bin/sh", Command: "true", CWD: t.TempDir(), EnvironmentAdditions: tc.env}, sink)
			if err == nil || spawn.Succeeded {
				t.Fatalf("spawn=%#v err=%v", spawn, err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "/tmp/e27") {
				t.Fatalf("trace value leaked in error: %v", err)
			}
		})
	}
}

func TestE27InputTraceTTYRejectsInstrumentationEnvironmentBeforePTYSpawn(t *testing.T) {
	sink := &memorySink{}
	_, spawn, err := (Owner{}).Start(context.Background(), operation.ExecutionSpec{Shell: "/bin/sh", Command: "printf should-not-run", CWD: t.TempDir(), TTY: true, EnvironmentAdditions: e27TraceEnvironment()}, sink)
	if err == nil || spawn.Succeeded || !strings.Contains(err.Error(), "trace_environment_unsupported") {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.b) != 0 {
		t.Fatalf("TTY child ran unexpectedly: %q", sink.b)
	}
}

func TestE27InputTraceMergeIsDeterministic(t *testing.T) {
	cmdA, _, err := commandFor(operation.ExecutionSpec{Shell: "/bin/sh", Command: "true", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	cmdB, _, err := commandFor(operation.ExecutionSpec{Shell: "/bin/sh", Command: "true", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	env := e27TraceEnvironment()
	if err := applyEnvironmentAdditions(cmdA, env); err != nil {
		t.Fatal(err)
	}
	if err := applyEnvironmentAdditions(cmdB, []operation.EnvironmentEntry{env[3], env[1], env[0], env[2]}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cmdA.Env, "\x00") != strings.Join(cmdB.Env, "\x00") {
		t.Fatal("trace environment merge depends on input order")
	}
}

func e27TraceEnvironment() []operation.EnvironmentEntry {
	dylib := "/tmp/e27.dylib"
	if runtime.GOOS == "darwin" {
		dylib = "/usr/lib/libSystem.B.dylib"
	}
	return []operation.EnvironmentEntry{
		{Key: "DYLD_INSERT_LIBRARIES", Value: dylib},
		{Key: "SHELLBEAM_TRACE_SOCKET", Value: "/tmp/e27.sock"},
		{Key: "SHELLBEAM_TRACE_PROTOCOL", Value: "1"},
		{Key: "SHELLBEAM_TRACE_ID", Value: "trace_01K00000000000000000000000"},
	}
}
