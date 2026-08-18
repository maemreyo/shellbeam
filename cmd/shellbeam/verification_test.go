package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/capability"
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

func TestVerificationCatalogAdvertisesExactV1Support(t *testing.T) {
	got := daemonCatalog(capability.Limits{})
	if got.Features[capability.FeatureVerificationSemantics] != capability.Available || got.VerificationSemantics == nil {
		t.Fatalf("catalog=%#v", got.VerificationSemantics)
	}
	s := got.VerificationSemantics
	if !reflect.DeepEqual(s.SchemaVersions, []int{1}) || !reflect.DeepEqual(s.PolicySchemaVersions, []int{1}) || s.MaxDomains != 16 || s.MaxRelations != 512 || s.MaxObligations != 256 || s.MaxPolicyGaps != 128 || s.MaxPolicyRules != 128 || s.MaxClassifications != 128 || s.MaxEvidenceRequirementsPerRule != 32 {
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
