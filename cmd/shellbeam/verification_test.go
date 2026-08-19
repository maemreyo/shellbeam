package main

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

type fakeDaemonVerification struct {
	calls    int
	inspect  verificationapp.InspectRequest
	preview  verificationapp.PreviewPolicyRequest
	activate verificationapp.ActivateRequest
	set      verificationapp.SetWaiverRequest
	revoke   verificationapp.RevokeWaiverRequest
}

func (f *fakeDaemonVerification) InspectVerification(_ context.Context, r verificationapp.InspectRequest) (verificationapp.Inspection, error) {
	f.inspect = r
	return verificationapp.Inspection{SchemaVersion: 1, Phase: r.Phase, WorkspaceID: r.WorkspaceID, PolicyState: verificationapp.PolicyStateAbsent}, nil
}
func (f *fakeDaemonVerification) PreviewVerificationPolicy(_ context.Context, r verificationapp.PreviewPolicyRequest) (verificationapp.PolicyPreview, error) {
	f.preview = r
	return verificationapp.PolicyPreview{State: verificationapp.PolicyLoadAbsent}, nil
}
func (f *fakeDaemonVerification) ActivateVerificationPolicy(_ context.Context, r verificationapp.ActivateRequest) (verificationcore.ActivationWriteResult, error) {
	f.activate = r
	return verificationcore.ActivationWriteResult{Replayed: true}, nil
}
func (f *fakeDaemonVerification) SetVerificationWaiver(_ context.Context, r verificationapp.SetWaiverRequest) (verificationcore.WaiverWriteResult, error) {
	f.set = r
	return verificationcore.WaiverWriteResult{Active: true}, nil
}
func (f *fakeDaemonVerification) RevokeVerificationWaiver(_ context.Context, r verificationapp.RevokeWaiverRequest) (verificationcore.RevocationWriteResult, error) {
	f.revoke = r
	return verificationcore.RevocationWriteResult{Replayed: true}, nil
}

func TestVerificationCatalogAdvertisesV1AndV2InspectionSupport(t *testing.T) {
	got := daemonCatalog(capability.Limits{})
	if got.Features[capability.FeatureVerificationSemantics] != capability.Available || got.VerificationSemantics == nil {
		t.Fatalf("catalog=%#v", got.VerificationSemantics)
	}
	s := got.VerificationSemantics
	if !reflect.DeepEqual(s.SchemaVersions, []int{1, 2}) || !reflect.DeepEqual(s.PolicySchemaVersions, []int{1}) || s.MaxDomains != 16 || s.MaxRelations != 512 || s.MaxObligations != 256 || s.MaxPolicyGaps != 128 || s.MaxPolicyRules != 128 || s.MaxClassifications != 128 || s.MaxEvidenceRequirementsPerRule != 32 {
		t.Fatalf("support=%#v", s)
	}
}

func TestDaemonActionsVerificationDelegatesWithoutExecution(t *testing.T) {
	fake := &fakeDaemonVerification{}
	actions := &daemonActions{verification: fake}
	ctx := context.Background()
	wid := "ws_01K00000000000000000000000"
	if _, err := actions.InspectVerification(ctx, verificationapp.InspectRequest{WorkspaceID: wid, Phase: verificationcore.PhaseCheckpoint}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.PreviewVerificationPolicy(ctx, verificationapp.PreviewPolicyRequest{WorkspaceID: wid, Profile: "team"}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.ActivateVerificationPolicy(ctx, verificationapp.ActivateRequest{ActivationID: "act_one", WorkspaceID: wid}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.SetVerificationWaiver(ctx, verificationapp.SetWaiverRequest{WaiverID: "wv_one", WorkspaceID: wid}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.RevokeVerificationWaiver(ctx, verificationapp.RevokeWaiverRequest{WaiverID: "wv_one", WorkspaceID: wid}); err != nil {
		t.Fatal(err)
	}
	if fake.inspect.Phase != verificationcore.PhaseCheckpoint || fake.preview.Profile != "team" || fake.activate.ActivationID != "act_one" || fake.set.WaiverID != "wv_one" || fake.revoke.WaiverID != "wv_one" {
		t.Fatalf("fake=%#v", fake)
	}
}

func TestVerificationRuntimeCompositionHasNoProcessExecutorDependency(t *testing.T) {
	typ := reflect.TypeOf(composeVerificationRuntime)
	for i := 0; i < typ.NumIn(); i++ {
		name := typ.In(i).String()
		if strings.Contains(name, "daemon.Service") || strings.Contains(name, "process") || strings.Contains(name, "Executor") || strings.Contains(name, "Runner") {
			t.Fatalf("execution dependency leaked into verification composition: %s", name)
		}
	}
}

type verificationBaseActions struct{}

func (verificationBaseActions) Start(context.Context, daemonapp.StartRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (verificationBaseActions) Poll(context.Context, daemonapp.PollRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (verificationBaseActions) Write(context.Context, daemonapp.WriteRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (verificationBaseActions) Kill(context.Context, daemonapp.KillRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (verificationBaseActions) InspectServer(context.Context) (daemonapp.ServerInfo, error) {
	return daemonapp.ServerInfo{}, nil
}

func TestOrdinarySessionActionsDoNotCallVerification(t *testing.T) {
	verification := &fakeDaemonVerification{}
	actions := daemonActions{Actions: verificationBaseActions{}, verification: verification}
	ctx := context.Background()
	if _, err := actions.Start(ctx, daemonapp.StartRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Poll(ctx, daemonapp.PollRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Write(ctx, daemonapp.WriteRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Kill(ctx, daemonapp.KillRequest{}); err != nil {
		t.Fatal(err)
	}
	if verification.calls != 0 {
		t.Fatalf("ordinary session actions called verification %d times", verification.calls)
	}
}

type verificationEvidenceSourceStub struct{}

func (verificationEvidenceSourceStub) Candidates(context.Context, verificationapp.CandidateQuery) (verificationapp.CandidateResultSet, error) {
	return verificationapp.CandidateResultSet{Coverage: verificationcore.CoverageComplete}, nil
}

type verificationEnvironmentSourceStub struct{}

func (verificationEnvironmentSourceStub) CurrentBinding(context.Context, string) (environmentcore.Binding, bool, error) {
	return environmentcore.Binding{}, false, nil
}

type verificationQuiescenceSourceStub struct{}

func (verificationQuiescenceSourceStub) Observe(context.Context, string, string, string) (verificationcore.QuiescenceObservation, bool, error) {
	return verificationcore.QuiescenceObservation{}, false, nil
}

type verificationCostSourceStub struct{}

func (verificationCostSourceStub) Histories(context.Context, []string) (map[string]verificationapp.CostHistory, error) {
	return map[string]verificationapp.CostHistory{}, nil
}

func TestVerificationRuntimeBindsReadOnlyEvaluationSourcesExactlyOnce(t *testing.T) {
	runtime := &daemonVerificationRuntime{inspection: verificationapp.NewInspectionService(nil, nil, nil, nil, nil, nil, nil, nil)}
	sources := verificationapp.EvaluationSources{
		Evidence: verificationEvidenceSourceStub{}, Environment: verificationEnvironmentSourceStub{},
		Quiescence: verificationQuiescenceSourceStub{}, Costs: verificationCostSourceStub{},
	}
	if err := runtime.BindEvaluationSources(sources); err != nil {
		t.Fatal(err)
	}
	if err := runtime.BindEvaluationSources(sources); err == nil {
		t.Fatal("second evaluation-source bind accepted")
	}
}

func TestVerificationRuntimeBindsProductionReadOnlySourcesWithoutExecutor(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	evidenceRuntime, err := newExecutionEvidenceRuntime(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer evidenceRuntime.shutdown(context.Background())
	telemetryRuntime, err := newExecutionTelemetryRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	defer telemetryRuntime.shutdown(context.Background())
	runtime := &daemonVerificationRuntime{
		inspection:  verificationapp.NewInspectionService(nil, nil, nil, nil, nil, nil, nil, nil),
		environment: verificationEnvironmentSourceStub{},
	}
	if err := runtime.bindRuntimeEvaluationSources(store, evidenceRuntime, telemetryRuntime); err != nil {
		t.Fatal(err)
	}
	if err := runtime.bindRuntimeEvaluationSources(store, evidenceRuntime, telemetryRuntime); err == nil {
		t.Fatal("production evaluation sources rebound")
	}
}
