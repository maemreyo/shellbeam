package inputtrace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type serviceFakePreparer struct {
	calls    int
	prepared Prepared
	err      error
	panic    bool
}

func (f *serviceFakePreparer) Prepare(context.Context, PrepareRequest) (Prepared, error) {
	if f.panic {
		panic("input trace preparer called")
	}
	f.calls++
	return f.prepared, f.err
}

type serviceFakePrepared struct {
	binding core.InstrumentationBinding
	env     []operation.EnvironmentEntry
	aborts  int
}

func (p *serviceFakePrepared) Binding() core.InstrumentationBinding { return p.binding }
func (p *serviceFakePrepared) EnvironmentAdditions() []operation.EnvironmentEntry {
	return append([]operation.EnvironmentEntry(nil), p.env...)
}
func (p *serviceFakePrepared) Abort() error { p.aborts++; return nil }

func TestE27InputTraceServiceOffHasZeroProviderWork(t *testing.T) {
	preparer := &serviceFakePreparer{panic: true}
	got, err := New(preparer).Prepare(context.Background(), PrepareRequest{Mode: core.ModeOff, OperationID: "off", ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", CWD: "/tmp"})
	if err != nil || got.Handle != nil || got.Binding != nil || len(got.EnvironmentAdditions) != 0 {
		t.Fatalf("off preparation=%#v err=%v", got, err)
	}
}

func TestE27InputTraceServiceMissingProviderIsTypedUnavailable(t *testing.T) {
	_, err := New(nil).Prepare(context.Background(), PrepareRequest{Mode: core.ModeBestEffort, OperationID: "missing", ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", CWD: "/tmp"})
	if !errors.Is(err, failure.InputTraceProviderUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestE27InputTraceServiceFreezesValidatedPreparedFacts(t *testing.T) {
	prepared := &serviceFakePrepared{binding: serviceTraceBinding(), env: []operation.EnvironmentEntry{{Name: "SHELLBEAM_TRACE_ID", Value: "trace_01K00000000000000000000000"}}}
	preparer := &serviceFakePreparer{prepared: prepared}
	got, err := New(preparer).Prepare(context.Background(), PrepareRequest{Mode: core.ModeBestEffort, OperationID: "active", ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", CWD: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if preparer.calls != 1 || got.Handle != prepared || got.Binding == nil || got.Binding.TraceID != prepared.binding.TraceID || len(got.EnvironmentAdditions) != 1 {
		t.Fatalf("preparation=%#v calls=%d", got, preparer.calls)
	}
	prepared.binding.InstrumentationFingerprint = strings.Repeat("b", 64)
	prepared.env[0].Value = "changed"
	if got.Binding.InstrumentationFingerprint != strings.Repeat("a", 64) || got.EnvironmentAdditions[0].Value != "trace_01K00000000000000000000000" {
		t.Fatal("service did not freeze provider facts")
	}
}

func TestE27InputTraceServiceRejectsBindingModeMismatch(t *testing.T) {
	prepared := &serviceFakePrepared{binding: serviceTraceBinding()}
	_, err := New(&serviceFakePreparer{prepared: prepared}).Prepare(context.Background(), PrepareRequest{Mode: core.ModeRequired, OperationID: "required", ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", CWD: "/tmp"})
	if !errors.Is(err, failure.InputTraceProviderUnavailable) {
		t.Fatalf("mode mismatch err=%v", err)
	}
	if prepared.aborts != 1 {
		t.Fatalf("invalid prepared handle aborts=%d", prepared.aborts)
	}
}

func serviceTraceBinding() core.InstrumentationBinding {
	return core.InstrumentationBinding{
		SchemaVersion: core.SchemaVersion, TraceID: "trace_01K00000000000000000000000", Mode: core.ModeBestEffort, Status: core.BindingActive,
		Provider: core.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
		InstrumentationFingerprint: strings.Repeat("a", 64), InstrumentationEffect: core.EffectEnvironmentAffecting,
		Coverage: core.CoverageMatrix{FilesystemReads: core.CoveragePartial, FilesystemMetadataQueries: core.CoveragePartial, DirectoryEnumerations: core.CoveragePartial, FilesystemWrites: core.CoveragePartial, ExecutedBinaries: core.CoveragePartial, LoadedLibraries: core.CoveragePartial, EnvironmentNamesObserved: core.CoverageUnsupported, NetworkAttempts: core.CoverageUnsupported, ChildProcesses: core.CoveragePartial},
	}
}
