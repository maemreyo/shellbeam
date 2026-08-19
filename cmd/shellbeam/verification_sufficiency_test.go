package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	operationcore "github.com/maemreyo/shellbeam/internal/core/operation"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const verificationSufficiencyPolicy = `schema_version = 1
policy_id = "p1-sufficiency"

[[rules]]
id = "docs-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs_contract"
minimum_affected_authority = "mechanical"

[[rules.evidence]]
id = "docs-verify"
provider_class = "project_command"
project_command_id = "verify_docs"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
execution = { parallel_safe = true, shared_resources = ["verification-cache"], expected_workload_class = "light" }
`

const verificationSufficiencyManifest = `schema_version = 2

[commands.verify_docs]
argv = ["/bin/sh", "-c", "true"]
cwd = "."
kind = "test"
source_scope = "full"
`

type verificationSufficiencyFixture struct {
	repo      string
	workspace workspacecore.Workspace
	client    *ipcadapter.Client
	stateDir  string
}

func newVerificationSufficiencyFixtureWith(t *testing.T, policy, manifest string) verificationSufficiencyFixture {
	t.Helper()
	repo := initWorkspaceCLIRepo(t)
	writeVerificationFile(t, repo, "docs/guide.md", "# Guide\n")
	writeVerificationFile(t, repo, ".shellbeam/project.toml", manifest)
	writeVerificationPolicy(t, repo, policy)
	runWorkspaceGit(t, repo, "add", "docs/guide.md", ".shellbeam/project.toml", ".shellbeam/verification-policy.toml")
	runWorkspaceGit(t, repo, "commit", "-m", "seed verification sufficiency fixture")
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, repo, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)
	_ = activateCommittedVerificationPolicy(t, client, workspace, repo, "sufficiency")
	settleVerificationWorkspace(t, client, workspace, repo, "sufficiency")
	return verificationSufficiencyFixture{repo: repo, workspace: workspace, client: client, stateDir: stateDir}
}

func newVerificationSufficiencyFixture(t *testing.T) verificationSufficiencyFixture {
	t.Helper()
	return newVerificationSufficiencyFixtureWith(t, verificationSufficiencyPolicy, verificationSufficiencyManifest)
}

func (f verificationSufficiencyFixture) runTypedEvidenceRequest(t *testing.T, operationID string, attempt *coreevidence.VerificationAttemptIntent) evidenceapp.InspectRecord {
	t.Helper()
	_ = callA1Terminal(t, f.client, ipcadapter.RequestV2{
		Action:              "start",
		OperationID:         operationID,
		WorkspaceID:         string(f.workspace.ID),
		ProjectCommandID:    "verify_docs",
		VerificationAttempt: attempt,
	})
	return waitEvidenceIPC(t, f.client, operationID)
}

func (f verificationSufficiencyFixture) runTypedEvidence(t *testing.T, operationID string) evidenceapp.InspectRecord {
	t.Helper()
	view := f.runTypedEvidenceRequest(t, operationID, nil)
	if view.Record.Result != coreevidence.ResultPass {
		t.Fatalf("typed PASS fixture result=%#v", view.Record)
	}
	return view
}

func newVerificationExternalOutcomeFixture(t *testing.T) (verificationSufficiencyFixture, string) {
	t.Helper()
	control := filepath.Join(t.TempDir(), "force-failure")
	script := fmt.Sprintf("test ! -f %q", control)
	manifest := fmt.Sprintf(`schema_version = 2

[commands.verify_docs]
argv = ["/bin/sh", "-c", %q]
cwd = "."
kind = "test"
source_scope = "full"
`, script)
	return newVerificationSufficiencyFixtureWith(t, verificationSufficiencyPolicy, manifest), control
}

func prepareVerificationDocsCut(t *testing.T, f verificationSufficiencyFixture, label string) verificationapp.Inspection {
	t.Helper()
	appendVerificationFile(t, f.repo, "docs/guide.md", "\n"+label+"\n")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, label+"-prepared")
	view := requireVerificationView(t, inspection, "docs-contract")
	if view.EvidenceStatus != verificationcore.EvidenceNotEvaluated && view.EvidenceStatus != verificationcore.EvidenceFailed && view.EvidenceStatus != verificationcore.EvidenceInconsistent && view.EvidenceStatus != verificationcore.EvidenceUnknown {
		t.Fatalf("unexpected prepared evidence state: %#v", view)
	}
	return inspection
}

func requireVerificationView(t *testing.T, inspection verificationapp.Inspection, ruleID string) verificationapp.ObligationView {
	t.Helper()
	for _, view := range inspection.ObligationViews {
		if view.SourceRuleID == ruleID {
			return view
		}
	}
	t.Fatalf("obligation view %q missing: %#v", ruleID, inspection.ObligationViews)
	return verificationapp.ObligationView{}
}

func TestVerificationSufficiencyCurrentTypedPassClearsGate(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\ncurrent change\n")
	before := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-before-pass")
	beforeView := requireVerificationView(t, before, "docs-contract")
	if beforeView.EvidenceStatus != verificationcore.EvidenceNotEvaluated {
		t.Fatalf("before evidence status=%s view=%#v", beforeView.EvidenceStatus, beforeView)
	}

	evidence := f.runTypedEvidence(t, "p1-sufficiency-pass")
	after := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-after-pass")
	view := requireVerificationView(t, after, "docs-contract")
	if view.EvidenceStatus != verificationcore.EvidenceSatisfied || after.Gate.Status != verificationcore.GateClear {
		t.Fatalf("current exact typed PASS did not satisfy gate: evidence=%#v gate=%#v view=%#v obligations=%#v", evidence.Record, after.Gate, view, after.Obligations)
	}
	if len(view.EvidenceRefs) != 1 || len(view.RequirementResults) != 1 || view.RequirementResults[0].Status != verificationcore.EvidenceSatisfied {
		t.Fatalf("current exact typed PASS projection=%#v", view)
	}
}

func TestVerificationSufficiencySourceChangeDoesNotReusePriorPass(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nfirst current change\n")
	evidence := f.runTypedEvidence(t, "p1-sufficiency-before-source-change")
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nsecond source change\n")

	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-after-source-change")
	view := requireVerificationView(t, inspection, "docs-contract")
	if view.EvidenceStatus == verificationcore.EvidenceSatisfied || inspection.Gate.Status == verificationcore.GateClear {
		t.Fatalf("prior generation PASS cleared current gate: evidence=%#v gate=%#v view=%#v", evidence.Record, inspection.Gate, view)
	}
	if len(view.RequirementResults) != 1 || (view.RequirementResults[0].ReasonCode != "evidence_freshness_unknown" && view.RequirementResults[0].ReasonCode != "evidence_stale") {
		t.Fatalf("source change did not surface literal freshness reason: %#v", view)
	}
	if len(view.EvidenceRefs) != 1 || view.EvidenceRefs[0] != evidence.Record.EvidenceID {
		t.Fatalf("historical evidence ref lost after source change: %#v", view)
	}
}

func TestVerificationSufficiencyProjectBindingChangeRejectsPriorPass(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nbinding baseline\n")
	evidence := f.runTypedEvidence(t, "p1-sufficiency-old-binding")
	writeVerificationFile(t, f.repo, ".shellbeam/project.toml", verificationSufficiencyManifest+"\n# binding revision\n")

	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-binding-changed")
	view := requireVerificationView(t, inspection, "docs-contract")
	if view.EvidenceStatus != verificationcore.EvidenceInsufficient || inspection.Gate.Status != verificationcore.GateBlocked {
		t.Fatalf("old project binding was accepted: evidence=%#v gate=%#v view=%#v", evidence.Record, inspection.Gate, view)
	}
	if len(view.RequirementResults) != 1 || view.RequirementResults[0].ReasonCode != "project_binding_mismatch" {
		t.Fatalf("binding mismatch reason=%#v", view.RequirementResults)
	}
}

func TestVerificationSufficiencyCurrentTypedFailureBlocksGate(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	failManifest := strings.Replace(verificationSufficiencyManifest, `argv = ["/bin/sh", "-c", "true"]`, `argv = ["/bin/sh", "-c", "exit 7"]`, 1)
	writeVerificationFile(t, f.repo, ".shellbeam/project.toml", failManifest)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nfailing current change\n")
	prepared := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-fail-prepared")
	if requireVerificationView(t, prepared, "docs-contract").EvidenceStatus != verificationcore.EvidenceNotEvaluated {
		t.Fatalf("failure fixture was not prepared on current cut: %#v", prepared)
	}
	terminal := callA1Terminal(t, f.client, ipcadapter.RequestV2{Action: "start", OperationID: "p1-sufficiency-fail", WorkspaceID: string(f.workspace.ID), ProjectCommandID: "verify_docs"})
	if terminal.Child == nil || terminal.Child.ExitCode == nil || *terminal.Child.ExitCode == 0 {
		t.Fatalf("failure fixture unexpectedly succeeded: %#v", terminal)
	}
	evidence := waitEvidenceIPC(t, f.client, "p1-sufficiency-fail")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-after-fail")
	view := requireVerificationView(t, inspection, "docs-contract")
	if view.EvidenceStatus != verificationcore.EvidenceFailed || inspection.Gate.Status != verificationcore.GateBlocked {
		t.Fatalf("current literal failure did not block gate: evidence_pre=%s evidence_post=%s inspection_gen=%s evidence=%#v gate=%#v view=%#v", evidence.Record.Source.PreGeneration, evidence.Record.Source.PostGeneration, inspection.SourceGeneration, evidence.Record, inspection.Gate, view)
	}
	if len(view.RequirementResults) != 1 || view.RequirementResults[0].ReasonCode != "evidence_failed" {
		t.Fatalf("failure reason=%#v", view.RequirementResults)
	}
}

func TestProviderExecutionSemanticsNeverChoosesUniversalConcurrencyRealDaemon(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nexecution semantics\n")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-execution-semantics")
	obligation := requireVerificationObligation(t, inspection, "docs-contract")
	if len(obligation.EvidenceRequirements) != 1 {
		t.Fatalf("evidence requirements=%#v", obligation.EvidenceRequirements)
	}
	execution := obligation.EvidenceRequirements[0].Requirement.Execution
	if execution.ParallelSafe == nil || !*execution.ParallelSafe || len(execution.SharedResources) != 1 || execution.SharedResources[0] != "verification-cache" || execution.ExpectedWorkloadClass != "light" || execution.ExclusiveResourceClass != "" {
		t.Fatalf("execution semantics did not round-trip: %#v", execution)
	}
	if len(inspection.CostSummary) != 1 || inspection.CostSummary[0].Execution.ParallelSafe == nil || !*inspection.CostSummary[0].Execution.ParallelSafe {
		t.Fatalf("cost projection lost execution semantics: %#v", inspection.CostSummary)
	}
	encoded := mustJSON(t, inspection)
	for _, forbidden := range []string{"worker_count", "max_workers", "admission_decision", "selected_provider"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("verification invented scheduler decision %q: %s", forbidden, encoded)
		}
	}
}

func TestVerificationSurfaceForbidsCompletionTruthFieldsRealDaemon(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\ntruth boundary\n")
	prepared := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-truth-prepared")
	if requireVerificationView(t, prepared, "docs-contract").EvidenceStatus != verificationcore.EvidenceNotEvaluated {
		t.Fatalf("truth-boundary fixture was not prepared on current cut: %#v", prepared)
	}
	evidence := f.runTypedEvidence(t, "p1-sufficiency-truth-boundary")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "sufficiency-truth-boundary")
	if inspection.Gate.Status != verificationcore.GateClear {
		t.Fatalf("fixture gate not clear: evidence_pre=%s evidence_post=%s inspection_gen=%s gate=%#v", evidence.Record.Source.PreGeneration, evidence.Record.Source.PostGeneration, inspection.SourceGeneration, inspection.Gate)
	}
	assertVerificationInspectionHasNoCompletionTruth(t, inspection)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestVerificationSufficiencyCompatibleFailThenPassIsInconsistent(t *testing.T) {
	f, control := newVerificationExternalOutcomeFixture(t)
	if err := os.WriteFile(control, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared := prepareVerificationDocsCut(t, f, "same-cohort")
	fail := f.runTypedEvidenceRequest(t, "p1-sufficiency-cohort-fail", nil)
	if fail.Record.Result != coreevidence.ResultFail {
		t.Fatalf("fail fixture=%#v", fail.Record)
	}
	if err := os.Remove(control); err != nil {
		t.Fatal(err)
	}
	pass := f.runTypedEvidence(t, "p1-sufficiency-cohort-pass")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "same-cohort-fold")
	view := requireVerificationView(t, inspection, "docs-contract")
	if fail.Record.Source.PreGeneration != prepared.SourceGeneration || pass.Record.Source.PreGeneration != prepared.SourceGeneration {
		t.Fatalf("runs escaped prepared cohort: prepared=%s fail=%s pass=%s", prepared.SourceGeneration, fail.Record.Source.PreGeneration, pass.Record.Source.PreGeneration)
	}
	if view.EvidenceStatus != verificationcore.EvidenceInconsistent || inspection.Gate.Status != verificationcore.GateBlocked {
		t.Fatalf("compatible FAIL->PASS did not remain inconsistent: gate=%#v view=%#v", inspection.Gate, view)
	}
	if len(view.EvidenceRefs) != 2 {
		t.Fatalf("contradictory refs not retained: %#v", view)
	}
}

func TestSourceMutationSeparatesEvidenceCohortsWithoutRewritingHistory(t *testing.T) {
	f, control := newVerificationExternalOutcomeFixture(t)
	if err := os.WriteFile(control, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstCut := prepareVerificationDocsCut(t, f, "generation-one")
	fail := f.runTypedEvidenceRequest(t, "p1-sufficiency-g1-fail", nil)
	if fail.Record.Result != coreevidence.ResultFail {
		t.Fatalf("G1 fail=%#v", fail.Record)
	}
	if err := os.Remove(control); err != nil {
		t.Fatal(err)
	}
	writeVerificationFile(t, f.repo, "docs/generation-two.md", "# Generation two\n")
	secondCut := inspectVerificationSemantics(t, f.client, f.workspace, "generation-two-prepared")
	if requireVerificationView(t, secondCut, "docs-contract").Disposition != verificationcore.DispositionRequiredNow {
		t.Fatalf("second generation did not keep docs obligation required: %#v", secondCut)
	}
	if secondCut.SourceGeneration == firstCut.SourceGeneration {
		t.Fatalf("source mutation did not change generation: %s", firstCut.SourceGeneration)
	}
	pass := f.runTypedEvidence(t, "p1-sufficiency-g2-pass")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "generation-two-fold")
	view := requireVerificationView(t, inspection, "docs-contract")
	if fail.Record.Source.PreGeneration == pass.Record.Source.PreGeneration || pass.Record.Source.PreGeneration != secondCut.SourceGeneration {
		t.Fatalf("cohorts not separated: fail=%s pass=%s current=%s", fail.Record.Source.PreGeneration, pass.Record.Source.PreGeneration, secondCut.SourceGeneration)
	}
	if view.EvidenceStatus != verificationcore.EvidenceSatisfied || inspection.Gate.Status != verificationcore.GateClear {
		t.Fatalf("old incompatible FAIL blocked current PASS: gate=%#v view=%#v", inspection.Gate, view)
	}
	if len(view.EvidenceRefs) != 2 {
		t.Fatalf("history refs were rewritten/lost: %#v", view)
	}
}

func TestRerunIntentFrozenBeforeExecutionAndDoesNotEraseContradiction(t *testing.T) {
	f, control := newVerificationExternalOutcomeFixture(t)
	if err := os.WriteFile(control, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = prepareVerificationDocsCut(t, f, "diagnostic-rerun")
	root := f.runTypedEvidenceRequest(t, "p1-sufficiency-diagnostic-root", nil)
	if root.Record.Result != coreevidence.ResultFail {
		t.Fatalf("diagnostic root=%#v", root.Record)
	}
	if err := os.Remove(control); err != nil {
		t.Fatal(err)
	}
	attempt := &coreevidence.VerificationAttemptIntent{RerunOfEvidenceID: root.Record.EvidenceID, RerunReason: coreevidence.RerunDiagnoseFlake}
	rerun := f.runTypedEvidenceRequest(t, "p1-sufficiency-diagnostic-rerun", attempt)
	if rerun.Record.Result != coreevidence.ResultPass {
		t.Fatalf("diagnostic rerun=%#v", rerun.Record)
	}
	store := openA1Store(t, f.stateDir)
	opID, err := operationcore.ParseID("p1-sufficiency-diagnostic-rerun")
	if err != nil {
		t.Fatal(err)
	}
	reservation, found, err := store.FindOperation(t.Context(), opID)
	if err != nil || !found || reservation.VerificationAttempt == nil {
		t.Fatalf("frozen rerun intent missing: found=%v err=%v reservation=%#v", found, err, reservation)
	}
	if *reservation.VerificationAttempt != *attempt {
		t.Fatalf("frozen rerun intent changed: got=%#v want=%#v", reservation.VerificationAttempt, attempt)
	}
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "diagnostic-rerun-fold")
	view := requireVerificationView(t, inspection, "docs-contract")
	if view.EvidenceStatus != verificationcore.EvidenceInconsistent || inspection.Gate.Status != verificationcore.GateBlocked {
		t.Fatalf("diagnose_flake erased contradiction: gate=%#v view=%#v", inspection.Gate, view)
	}
}

const verificationSufficiencyManifestWithNative = `schema_version = 2

[commands.verify_docs]
argv = ["/bin/sh", "-c", "true"]
cwd = "."
kind = "test"
source_scope = "full"

[commands.native_linux]
argv = ["/bin/sh", "-c", "true"]
cwd = "."
kind = "test"
source_scope = "full"
`

const verificationWaivedNativePolicy = `schema_version = 1
policy_id = "p1-waived-native"

[[rules]]
id = "docs-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs_contract"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "docs-verify"
provider_class = "project_command"
project_command_id = "verify_docs"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"

[[rules]]
id = "native-linux"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "integration_owned"
required = true
sufficiency_basis = "native_linux_contract"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "native-linux-verify"
provider_class = "native_platform_verification"
project_command_id = "native_linux"
minimum_authority = "mechanical"
require_current = true
environment = "same_current_toolchain"
stability = "no_contradiction"
`

const verificationSecurityGapPolicy = `schema_version = 1
policy_id = "p1-security-gap"

[[classifications]]
id = "security"
paths = ["internal/auth/**"]
surface_class = "security_sensitive"

[[rules]]
id = "docs-only"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs_only"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "docs-verify"
provider_class = "project_command"
project_command_id = "verify_docs"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
`

const verificationScalePolicy = `schema_version = 1
policy_id = "p1-scale"

[[rules]]
id = "load-target"
phases = ["checkpoint"]
match_paths = ["perf/**"]
ownership = "integration_owned"
risk_class = "scale_driven"
required = true
sufficiency_basis = "declared_performance_target"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "load-measurement"
provider_class = "resource_measurement"
minimum_authority = "mechanical"
require_current = true
environment = "same_current"
stability = "no_contradiction"
`

func TestVerificationSufficiencyWaiverPreservesUnavailableNativeEvidence(t *testing.T) {
	f := newVerificationSufficiencyFixtureWith(t, verificationWaivedNativePolicy, verificationSufficiencyManifestWithNative)
	// Policy activation bound native_linux successfully. Removing it afterward makes the current requirement mechanically unavailable without rewriting policy authority.
	writeVerificationFile(t, f.repo, ".shellbeam/project.toml", verificationSufficiencyManifest)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nwaived native\n")
	prepared := inspectVerificationSemantics(t, f.client, f.workspace, "waived-native-prepared")
	nativeBefore := requireVerificationView(t, prepared, "native-linux")
	if nativeBefore.EvidenceStatus != verificationcore.EvidenceUnavailable {
		t.Fatalf("missing native binding did not remain unavailable: %#v", nativeBefore)
	}
	if prepared.EffectivePolicy == nil {
		t.Fatalf("effective policy missing: %#v", prepared)
	}
	waiver := verificationWaiverRequest(f.workspace, "wv_native_linux", prepared.EffectivePolicy.Digest, "native-linux", prepared.SourceGeneration)
	response, err := f.client.CallV2(t.Context(), waiver)
	if err != nil || !response.OK || response.VerificationWaiver == nil || !response.VerificationWaiver.Active {
		t.Fatalf("native waiver response=%#v err=%v", response, err)
	}
	f.runTypedEvidence(t, "p1-sufficiency-waived-docs-pass")
	after := inspectVerificationSemantics(t, f.client, f.workspace, "waived-native-after")
	docs := requireVerificationView(t, after, "docs-contract")
	native := requireVerificationView(t, after, "native-linux")
	rawNative := requireVerificationObligation(t, after, "native-linux")
	if docs.EvidenceStatus != verificationcore.EvidenceSatisfied || rawNative.Disposition != verificationcore.DispositionWaived || native.WaiverID != "wv_native_linux" || native.EvidenceStatus != verificationcore.EvidenceUnavailable {
		t.Fatalf("waiver rewrote evidence truth: docs=%#v native=%#v raw=%#v", docs, native, rawNative)
	}
	if after.Gate.Status != verificationcore.GateClear || after.Gate.Breakdown.EvidenceSatisfied != 1 || after.Gate.Breakdown.Waived != 1 {
		t.Fatalf("waived native requirement did not fold separately: %#v", after.Gate)
	}
}

func TestVerificationSufficiencyPartialAffectedSurfaceKeepsMandatoryObligation(t *testing.T) {
	repo := initVerificationSemanticsGraphRepo(t, verificationSemanticsMixedPolicy)
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, repo, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)
	_ = activateCommittedVerificationPolicy(t, client, workspace, repo, "task8-partial")
	settleVerificationWorkspace(t, client, workspace, repo, "task8-partial")
	writeVerificationFile(t, repo, "nested/go.mod", "module example.com/nested\n")
	appendVerificationFile(t, repo, "docs/guide.md", "\npartial surface\n")
	inspection := inspectVerificationSemantics(t, client, workspace, "task8-partial-inspect")
	domain := requireAffectedDomain(t, inspection, verificationcore.DomainGoImportGraph)
	goRule := requireVerificationObligation(t, inspection, "go-contract")
	if domain.Coverage == verificationcore.CoverageComplete || goRule.Disposition != verificationcore.DispositionRequiredNow {
		t.Fatalf("partial surface narrowed mandatory rule: domain=%#v rule=%#v", domain, goRule)
	}
}

func TestVerificationSufficiencyPolicyGapIsAdvisoryNotGate(t *testing.T) {
	f := newVerificationSufficiencyFixtureWith(t, verificationSecurityGapPolicy, verificationSufficiencyManifest)
	writeVerificationFile(t, f.repo, "internal/auth/new.go", "package auth\n")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "task8-security-gap")
	if !hasVerificationPolicyGap(inspection, "security_sensitive", "internal/auth/new.go") {
		t.Fatalf("security policy gap missing: %#v", inspection.PolicyGaps)
	}
	docs := requireVerificationObligation(t, inspection, "docs-only")
	if docs.Disposition != verificationcore.DispositionNotTriggered || inspection.Gate.Status != verificationcore.GateClear {
		t.Fatalf("advisory gap became gate: docs=%#v gate=%#v", docs, inspection.Gate)
	}
}

func TestVerificationSufficiencyNoPerformanceTargetLeavesLoadNotTriggered(t *testing.T) {
	f := newVerificationSufficiencyFixtureWith(t, verificationScalePolicy, verificationSufficiencyManifest)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nno performance target\n")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "task8-no-performance-target")
	load := requireVerificationObligation(t, inspection, "load-target")
	if load.Disposition != verificationcore.DispositionNotTriggered || load.EvidenceStatus != verificationcore.EvidenceNotEvaluated || len(load.EvidenceRefs) != 0 {
		t.Fatalf("load verification triggered without matching target: %#v", load)
	}
	if inspection.Gate.Status != verificationcore.GateClear {
		t.Fatalf("not-triggered load rule blocked gate: %#v", inspection.Gate)
	}
}

const verificationQuiescencePolicy = `schema_version = 1
policy_id = "p1-quiescence"

[[rules]]
id = "docs-quiescence"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs_process_quiescence"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "docs-verify"
provider_class = "project_command"
project_command_id = "verify_docs"
minimum_authority = "mechanical"
require_current = true
require_quiescence = true
environment = "none"
stability = "no_contradiction"
`

func TestVerificationSufficiencyRealDaemonNeverInventsLifecycleCompletion(t *testing.T) {
	f := newVerificationSufficiencyFixtureWith(t, verificationQuiescencePolicy, verificationSufficiencyManifest)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nquiescence required\n")
	prepared := inspectVerificationSemantics(t, f.client, f.workspace, "quiescence-prepared")
	if requireVerificationView(t, prepared, "docs-quiescence").EvidenceStatus != verificationcore.EvidenceNotEvaluated {
		t.Fatalf("quiescence fixture not prepared: %#v", prepared)
	}
	f.runTypedEvidence(t, "p1-sufficiency-quiescence-pass")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "quiescence-after-pass")
	view := requireVerificationView(t, inspection, "docs-quiescence")
	if view.EvidenceStatus != verificationcore.EvidenceUnknown || inspection.Gate.Status != verificationcore.GateIndeterminate {
		t.Fatalf("daemon without qualified lifecycle provider invented completion: gate=%#v view=%#v", inspection.Gate, view)
	}
	if len(view.RequirementResults) != 1 || view.RequirementResults[0].ReasonCode != "quiescence_unknown" {
		t.Fatalf("missing lifecycle proof reason=%#v", view.RequirementResults)
	}
}

const verificationRawFocusedPolicy = `schema_version = 1
policy_id = "p1-raw-focused"

[[rules]]
id = "focused-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "focused_behavior"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "focused-test"
provider_class = "focused_behavior_test"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
`

const verificationHistoricalPolicy = `schema_version = 1
policy_id = "p1-historical"

[[rules]]
id = "historical-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "historical_bound_evidence"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "historical-verify"
provider_class = "project_command"
project_command_id = "verify_docs"
minimum_authority = "mechanical"
require_current = false
environment = "none"
stability = "no_contradiction"
`

func costIsUnavailable(cost verificationcore.VerificationCost) bool {
	for _, metric := range []verificationcore.CostMetric{cost.WallMS, cost.OutputBytes, cost.CPUUserMS, cost.CPUSystemMS, cost.MaxRSSBytes, cost.ProcessPeak, cost.ProviderCost, cost.ModelCost} {
		if metric.Quality != verificationcore.CostQualityUnavailable || metric.Latest != nil || metric.P50 != nil || metric.P95 != nil || metric.Samples != 0 {
			return false
		}
	}
	return true
}

func TestVerificationSufficiencyNoTelemetryKeepsCostUnavailable(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nno telemetry\n")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "task8-no-telemetry")
	view := requireVerificationView(t, inspection, "docs-contract")
	if len(inspection.CostSummary) != 1 || !costIsUnavailable(inspection.CostSummary[0].Cost) {
		t.Fatalf("missing telemetry became zero/observed cost: %#v", inspection.CostSummary)
	}
	if view.EvidenceStatus == verificationcore.EvidenceSatisfied || inspection.Gate.Status == verificationcore.GateClear {
		t.Fatalf("cost projection changed unsatisfied gate: gate=%#v view=%#v", inspection.Gate, view)
	}
}

func TestVerificationSufficiencyCostFactsNeverOverrideFailure(t *testing.T) {
	f := newVerificationSufficiencyFixture(t)
	failManifest := strings.Replace(verificationSufficiencyManifest, `argv = ["/bin/sh", "-c", "true"]`, `argv = ["/bin/sh", "-c", "exit 9"]`, 1)
	writeVerificationFile(t, f.repo, ".shellbeam/project.toml", failManifest)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\ntelemetry failure\n")
	_ = inspectVerificationSemantics(t, f.client, f.workspace, "task8-cost-fail-prepared")
	failed := f.runTypedEvidenceRequest(t, "p1-sufficiency-cost-fail", nil)
	if failed.Record.Result != coreevidence.ResultFail {
		t.Fatalf("cost failure fixture=%#v", failed.Record)
	}
	store := openA1Store(t, f.stateDir)
	_ = waitDaemonTelemetry(t, store, "p1-sufficiency-cost-fail")
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "task8-cost-fail-fold")
	view := requireVerificationView(t, inspection, "docs-contract")
	if view.EvidenceStatus != verificationcore.EvidenceFailed || inspection.Gate.Status != verificationcore.GateBlocked {
		t.Fatalf("telemetry changed failure sufficiency: gate=%#v view=%#v", inspection.Gate, view)
	}
	if len(inspection.CostSummary) != 1 || costIsUnavailable(inspection.CostSummary[0].Cost) {
		t.Fatalf("telemetry facts were not projected for failed bound provider: %#v", inspection.CostSummary)
	}
	if strings.Contains(mustJSON(t, inspection), "selected_provider") {
		t.Fatalf("cost projection selected an alternative provider: %s", mustJSON(t, inspection))
	}
}

func TestVerificationSufficiencyRawTestEvidenceDoesNotElevateProviderClass(t *testing.T) {
	f := newVerificationSufficiencyFixtureWith(t, verificationRawFocusedPolicy, verificationSufficiencyManifest)
	appendVerificationFile(t, f.repo, "docs/guide.md", "\nraw test evidence\n")
	_ = inspectVerificationSemantics(t, f.client, f.workspace, "task8-raw-prepared")
	terminal := callA1Terminal(t, f.client, rawEvidenceStart("p1-sufficiency-raw-test", f.workspace, "true", coreevidence.VerificationTest))
	assertA1ChildSuccess(t, terminal)
	raw := waitEvidenceIPC(t, f.client, "p1-sufficiency-raw-test")
	if raw.Record.VerificationKind != coreevidence.VerificationTest || raw.Record.Command.ProjectCommandID != "" {
		t.Fatalf("raw evidence authority changed: %#v", raw.Record)
	}
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "task8-raw-fold")
	view := requireVerificationView(t, inspection, "focused-contract")
	if view.EvidenceStatus != verificationcore.EvidenceInsufficient || inspection.Gate.Status != verificationcore.GateBlocked || len(view.RequirementResults) != 1 || view.RequirementResults[0].ReasonCode != "provider_semantics_mismatch" {
		t.Fatalf("raw test implicitly elevated to focused provider: gate=%#v view=%#v", inspection.Gate, view)
	}
}

func TestVerificationSufficiencyHistoricalEvidenceBindsCurrentRequirementWithoutMutation(t *testing.T) {
	repo := initWorkspaceCLIRepo(t)
	writeVerificationFile(t, repo, "docs/guide.md", "# Guide\n")
	writeVerificationFile(t, repo, ".shellbeam/project.toml", verificationSufficiencyManifest)
	writeVerificationPolicy(t, repo, verificationHistoricalPolicy)
	runWorkspaceGit(t, repo, "add", "docs/guide.md", ".shellbeam/project.toml", ".shellbeam/verification-policy.toml")
	runWorkspaceGit(t, repo, "commit", "-m", "seed historical evidence fixture")
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, repo, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)

	preview := previewVerificationPolicy(t, client, workspace, "", "historical-preview")
	if preview.State != verificationapp.PolicyLoadValid || preview.Proposal == nil {
		t.Fatalf("historical preview=%#v", preview)
	}
	appendVerificationFile(t, repo, "docs/guide.md", "\npre-activation evidence\n")
	proposalCut := inspectVerificationSemantics(t, client, workspace, "historical-proposal-cut")
	if proposalCut.PolicyState != verificationapp.PolicyStateProposalPending || proposalCut.SourceGeneration == "" {
		t.Fatalf("historical proposal cut=%#v", proposalCut)
	}
	terminal := callA1Terminal(t, client, ipcadapter.RequestV2{Action: "start", OperationID: "p1-historical-pass", WorkspaceID: string(workspace.ID), ProjectCommandID: "verify_docs"})
	assertA1ChildSuccess(t, terminal)
	before := waitEvidenceIPC(t, client, "p1-historical-pass")
	beforeJSON := mustJSON(t, before.Record)
	for _, forbidden := range []string{"policy_digest", "rule_id", "obligation_id"} {
		if strings.Contains(beforeJSON, forbidden) {
			t.Fatalf("immutable pre-P1 evidence contains fake %s: %s", forbidden, beforeJSON)
		}
	}
	writeVerificationFile(t, repo, "activation-historical.cut", "activate\n")
	cut := inspectVerificationSemantics(t, client, workspace, "historical-activation-cut")
	if cut.SourceGeneration == proposalCut.SourceGeneration {
		t.Fatalf("historical activation cut did not advance generation")
	}
	activate := verificationActivationRequest(workspace, "act_historical", preview.Proposal.Digest, "absent", proposalCut.SourceGeneration, "task8")
	result := activateVerificationPolicy(t, client, activate, "historical-activate")
	if !result.Effective {
		t.Fatalf("historical activation=%#v", result)
	}
	after := inspectVerificationSemantics(t, client, workspace, "historical-fold")
	view := requireVerificationView(t, after, "historical-contract")
	if view.EvidenceStatus != verificationcore.EvidenceSatisfied || after.Gate.Status != verificationcore.GateClear || len(view.RequirementResults) != 1 || view.RequirementResults[0].EvaluationID == "" || len(view.EvidenceRefs) != 1 || view.EvidenceRefs[0] != before.Record.EvidenceID {
		t.Fatalf("historical evidence did not bind derived evaluation: gate=%#v view=%#v", after.Gate, view)
	}
	afterEvidence := inspectEvidenceIPC(t, client, "p1-historical-pass")
	if len(afterEvidence.Records) != 1 || mustJSON(t, afterEvidence.Records[0].Record) != beforeJSON {
		t.Fatalf("policy activation mutated historical evidence: before=%s after=%#v", beforeJSON, afterEvidence)
	}
}

const verificationFlakePolicy = `schema_version = 1
policy_id = "p1-flake"

[[rules]]
id = "docs-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "qualified_flake_protocol"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "docs-verify"
provider_class = "project_command"
project_command_id = "verify_docs"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "flake_protocol"
[rules.evidence.flake]
runs = 2
min_passes = 2
max_failures = 0
`

func TestVerificationSufficiencyApprovedFlakeQualificationCanResolveExactProtocol(t *testing.T) {
	control := filepath.Join(t.TempDir(), "force-flake-failure")
	script := fmt.Sprintf("test ! -f %q", control)
	manifest := fmt.Sprintf(`schema_version = 2

[commands.verify_docs]
argv = ["/bin/sh", "-c", %q]
cwd = "."
kind = "test"
source_scope = "full"
`, script)
	f := newVerificationSufficiencyFixtureWith(t, verificationFlakePolicy, manifest)
	if err := os.WriteFile(control, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared := prepareVerificationDocsCut(t, f, "flake-protocol")
	root := f.runTypedEvidenceRequest(t, "p1-flake-root", nil)
	if root.Record.Result != coreevidence.ResultFail || root.Record.Source.PreGeneration != prepared.SourceGeneration {
		t.Fatalf("flake root escaped cohort: prepared=%s record=%#v", prepared.SourceGeneration, root.Record)
	}
	if err := os.Remove(control); err != nil {
		t.Fatal(err)
	}
	attempt := func() *coreevidence.VerificationAttemptIntent {
		return &coreevidence.VerificationAttemptIntent{RerunOfEvidenceID: root.Record.EvidenceID, RerunReason: coreevidence.RerunFlakeQualification}
	}
	q1 := f.runTypedEvidenceRequest(t, "p1-flake-qualified-1", attempt())
	q2 := f.runTypedEvidenceRequest(t, "p1-flake-qualified-2", attempt())
	if q1.Record.Result != coreevidence.ResultPass || q2.Record.Result != coreevidence.ResultPass || q1.Record.Source.PreGeneration != prepared.SourceGeneration || q2.Record.Source.PreGeneration != prepared.SourceGeneration {
		t.Fatalf("qualified reruns escaped exact cohort: q1=%#v q2=%#v", q1.Record, q2.Record)
	}
	inspection := inspectVerificationSemantics(t, f.client, f.workspace, "flake-qualified-fold")
	view := requireVerificationView(t, inspection, "docs-contract")
	if view.EvidenceStatus != verificationcore.EvidenceSatisfied || inspection.Gate.Status != verificationcore.GateClear {
		t.Fatalf("approved exact flake protocol did not resolve cohort: gate=%#v view=%#v", inspection.Gate, view)
	}
	if len(view.EvidenceRefs) != 3 {
		t.Fatalf("flake resolution dropped contradictory history: %#v", view)
	}
}
