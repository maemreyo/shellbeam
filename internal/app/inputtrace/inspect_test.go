package inputtrace

import (
	"context"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type inspectRepo struct {
	reservation operation.Reservation
	found       bool
	record      core.Record
	recordFound bool
}

func (r *inspectRepo) FindOperation(context.Context, operation.ID) (operation.Reservation, bool, error) {
	return r.reservation, r.found, nil
}
func (r *inspectRepo) LoadInputTraceByOperation(context.Context, string) (core.Record, bool, error) {
	return r.record, r.recordFound, nil
}

func TestE27InputTraceInspectDistinguishesUnavailablePendingTerminal(t *testing.T) {
	id := "inspect-op"
	untraced := &inspectRepo{reservation: operation.Reservation{OperationID: operation.ID(id)}, found: true}
	got, err := NewInspector(untraced).Inspect(context.Background(), InspectRequest{OperationID: id, MaxResources: 10})
	if err != nil || got.Status != InspectUnavailable {
		t.Fatalf("unavailable=%#v err=%v", got, err)
	}

	binding := serviceTraceBinding()
	pending := &inspectRepo{reservation: operation.Reservation{OperationID: operation.ID(id), Trace: &binding}, found: true}
	got, err = NewInspector(pending).Inspect(context.Background(), InspectRequest{OperationID: id, MaxResources: 10})
	if err != nil || got.Status != InspectPending || got.TraceID != binding.TraceID {
		t.Fatalf("pending=%#v err=%v", got, err)
	}

	record := inspectTraceRecord(id, binding)
	terminal := &inspectRepo{reservation: pending.reservation, found: true, record: record, recordFound: true}
	got, err = NewInspector(terminal).Inspect(context.Background(), InspectRequest{OperationID: id, MaxResources: 1})
	if err != nil || got.Status != InspectTerminal || got.Record == nil || len(got.Record.Resources) != 1 || got.ResourcesAvailable != 2 || got.ResourcesReturned != 1 {
		t.Fatalf("terminal=%#v err=%v", got, err)
	}
	if got.Record.Coverage.FilesystemReads != core.CoveragePartial || !got.Record.MayHaveUnobservedDependencies {
		t.Fatalf("deep coverage/authority missing: %#v", got.Record)
	}
}

func TestE27InputTraceInspectBoundsAndCopiesResources(t *testing.T) {
	binding := serviceTraceBinding()
	record := inspectTraceRecord("inspect-bounds", binding)
	repo := &inspectRepo{reservation: operation.Reservation{OperationID: "inspect-bounds", Trace: &binding}, found: true, record: record, recordFound: true}
	service := NewInspector(repo)
	if _, err := service.Inspect(context.Background(), InspectRequest{OperationID: "inspect-bounds", MaxResources: 0}); err == nil {
		t.Fatal("zero bound accepted")
	}
	got, err := service.Inspect(context.Background(), InspectRequest{OperationID: "inspect-bounds", MaxResources: 1})
	if err != nil {
		t.Fatal(err)
	}
	got.Record.Resources[0].Identity = "mutated"
	if repo.record.Resources[0].Identity == "mutated" {
		t.Fatal("inspect aliased durable record")
	}
}

func inspectTraceRecord(id string, binding core.InstrumentationBinding) core.Record {
	return core.Record{SchemaVersion: core.SchemaVersion, DerivationKey: strings.Repeat("a", 64), TraceID: binding.TraceID, OperationID: id, SessionID: id + "-session", ReceiptDigest: strings.Repeat("b", 64), Mode: binding.Mode, Provider: binding.Provider, Platform: binding.Platform, InstrumentationFingerprint: binding.InstrumentationFingerprint, InstrumentationEffect: binding.InstrumentationEffect, Authority: core.AuthorityAdvisory, ScopeKind: core.ScopeObservedInput, MayHaveUnobservedDependencies: true, Coverage: binding.Coverage, Outcome: core.OutcomePartial, Resources: []core.Resource{{ObservationClass: core.ClassFilesystemReads, PathClass: core.PathRepoRelative, Identity: "dep-a.go"}, {ObservationClass: core.ClassFilesystemReads, PathClass: core.PathRepoRelative, Identity: "dep-b.go"}}, Summary: core.Summary{ResourcesReturned: 2, ResourcesObserved: 2}}
}
