package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const verificationSemanticsPolicyP1 = `schema_version = 1
policy_id = "stage-a-p1"

[[classifications]]
id = "security"
paths = ["internal/auth/**"]
surface_class = "security_sensitive"

[[rules]]
id = "docs-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs_contract"
minimum_affected_authority = "mechanical"

[[rules.evidence]]
id = "docs-format"
provider_class = "static_format_check"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
`

const verificationSemanticsPolicyP2 = `schema_version = 1
policy_id = "stage-a-p2"

[[rules]]
id = "docs-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs_contract"
minimum_affected_authority = "mechanical"

[[rules.evidence]]
id = "docs-format"
provider_class = "static_format_check"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
`

type verificationLifecycleState struct {
	repo         string
	workspace    workspacecore.Workspace
	client       *ipcadapter.Client
	p1Digest     string
	p1Generation string
	p1Activate   ipcadapter.RequestV2
	p1Result     verificationcore.ActivationWriteResult
}

func TestVerificationSemanticsPolicyLifecycle(t *testing.T) {
	repo := initVerificationSemanticsRepo(t)
	stateDir, runtimeDir := a1RuntimeDirs(t)
	state := &verificationLifecycleState{repo: repo, workspace: attachA5AcceptanceWorkspace(t, repo, stateDir)}
	state.client = runA1Daemon(t, stateDir, runtimeDir)
	verificationLifecycleBootstrapP1(t, state)
	verificationLifecycleAssertP1Authority(t, state)
	verificationLifecycleProposeAndActivateP2(t, state)
}

func verificationLifecycleBootstrapP1(t *testing.T, state *verificationLifecycleState) {
	t.Helper()
	t.Run("TestPolicyAbsentDoesNotSelectStarter", func(t *testing.T) {
		inspection := inspectVerificationSemantics(t, state.client, state.workspace, "absent")
		if inspection.PolicyState != verificationapp.PolicyStateAbsent || inspection.EffectivePolicy != nil || inspection.ProposedPolicy != nil || len(inspection.Obligations) != 0 {
			t.Fatalf("absent inspection=%#v", inspection)
		}
		preview := previewVerificationPolicy(t, state.client, state.workspace, "", "absent-preview")
		if preview.State != verificationapp.PolicyLoadAbsent || preview.Proposal != nil {
			t.Fatalf("absent preview=%#v", preview)
		}
		assertVerificationInspectionHasNoCompletionTruth(t, inspection)
	})
	writeVerificationPolicy(t, state.repo, verificationSemanticsPolicyP1)
	p1Preview := previewVerificationPolicy(t, state.client, state.workspace, "", "p1-preview")
	if p1Preview.State != verificationapp.PolicyLoadValid || p1Preview.Proposal == nil {
		t.Fatalf("p1 preview=%#v", p1Preview)
	}
	state.p1Digest = p1Preview.Proposal.Digest
	p1Proposed := inspectVerificationSemantics(t, state.client, state.workspace, "p1-proposed")
	state.p1Generation = p1Proposed.SourceGeneration
	t.Run("TestFirstPolicyRequiresExternalActivationSubsequentCut", func(t *testing.T) {
		if p1Proposed.PolicyState != verificationapp.PolicyStateProposalPending || p1Proposed.EffectivePolicy != nil || p1Proposed.ProposedPolicy == nil || p1Proposed.ProposedPolicy.Digest != state.p1Digest || len(p1Proposed.Obligations) != 0 {
			t.Fatalf("first policy became effective without activation: %#v", p1Proposed)
		}
	})
	state.p1Activate = verificationActivationRequest(state.workspace, "act_stage_a_p1", state.p1Digest, "absent", state.p1Generation, "stage-a")
	t.Run("TestPolicyCannotActivateForItsIntroducingGeneration", func(t *testing.T) {
		response, err := state.client.CallV2(context.Background(), state.p1Activate)
		if err != nil {
			t.Fatal(err)
		}
		if response.OK || response.Error == nil || response.VerificationActivation != nil {
			t.Fatalf("same-generation activation accepted: %#v", response)
		}
	})
	writeVerificationFile(t, state.repo, "activation-cut.txt", "cut-1\n")
	postCut := inspectVerificationSemantics(t, state.client, state.workspace, "p1-post-cut")
	if postCut.SourceGeneration == "" || postCut.SourceGeneration == state.p1Generation {
		t.Fatalf("source generation did not transition: before=%q after=%q", state.p1Generation, postCut.SourceGeneration)
	}
	state.p1Result = activateVerificationPolicy(t, state.client, state.p1Activate, "p1-activate")
	if !state.p1Result.Created || !state.p1Result.Effective || state.p1Result.Replayed || state.p1Result.Record.ProposedPolicyDigest != state.p1Digest {
		t.Fatalf("p1 activation=%#v", state.p1Result)
	}
}

func verificationLifecycleAssertP1Authority(t *testing.T, state *verificationLifecycleState) {
	t.Helper()
	t.Run("TestPolicyActivationIsImmutableAuditableAuthority", func(t *testing.T) {
		replay := activateVerificationPolicy(t, state.client, state.p1Activate, "p1-replay")
		if !replay.Replayed || !replay.Effective || !replay.Record.ActivatedAt.Equal(state.p1Result.Record.ActivatedAt) || replay.Record.IntentFingerprint != state.p1Result.Record.IntentFingerprint {
			t.Fatalf("same-intent replay changed authority record: first=%#v replay=%#v", state.p1Result, replay)
		}
		changed := state.p1Activate
		changed.RequestID, changed.Actor = "p1-conflict", "different-actor"
		response, err := state.client.CallV2(context.Background(), changed)
		if err != nil {
			t.Fatal(err)
		}
		if response.OK || response.Error == nil {
			t.Fatalf("same activation id with different intent accepted: %#v", response)
		}
	})
	writeVerificationFile(t, state.repo, "internal/auth/a.go", "package auth\n\nfunc A() int { return 200 }\n")
	p1Effective := inspectVerificationSemantics(t, state.client, state.workspace, "p1-effective")
	if p1Effective.PolicyState != verificationapp.PolicyStateEffective || p1Effective.EffectivePolicy == nil || p1Effective.EffectivePolicy.Digest != state.p1Digest || !hasVerificationPolicyGap(p1Effective, "security_sensitive", "internal/auth/a.go") {
		t.Fatalf("p1 effective classification/gap=%#v", p1Effective)
	}
}

func verificationLifecycleProposeAndActivateP2(t *testing.T, state *verificationLifecycleState) {
	t.Helper()
	writeVerificationPolicy(t, state.repo, verificationSemanticsPolicyP2)
	p2Preview := previewVerificationPolicy(t, state.client, state.workspace, "", "p2-preview")
	if p2Preview.State != verificationapp.PolicyLoadValid || p2Preview.Proposal == nil || p2Preview.Proposal.Digest == state.p1Digest {
		t.Fatalf("p2 preview=%#v", p2Preview)
	}
	p2Digest := p2Preview.Proposal.Digest
	p2Proposed := inspectVerificationSemantics(t, state.client, state.workspace, "p2-proposed")
	p2Generation := p2Proposed.SourceGeneration
	t.Run("TestProposedPolicyCannotChangeEffectiveClassificationProjection", func(t *testing.T) {
		if p2Proposed.PolicyState != verificationapp.PolicyStateProposalPending || p2Proposed.EffectivePolicy == nil || p2Proposed.EffectivePolicy.Digest != state.p1Digest || p2Proposed.ProposedPolicy == nil || p2Proposed.ProposedPolicy.Digest != p2Digest || !hasVerificationPolicyGap(p2Proposed, "security_sensitive", "internal/auth/a.go") {
			t.Fatalf("proposal contaminated effective p1: %#v", p2Proposed)
		}
	})
	t.Run("TestProposedPolicyCannotSelfGrantActivationOrWaiverAuthority", func(t *testing.T) {
		badActivation := verificationActivationRequest(state.workspace, "act_stage_a_p2_bad", p2Digest, state.p1Digest, p2Generation, "stage-a")
		badActivation.Authority = "repository_policy"
		if _, err := state.client.CallV2(context.Background(), badActivation); err == nil {
			t.Fatal("repository proposal self-granted activation authority")
		}
		badWaiver := verificationWaiverRequest(state.workspace, "wv_stage_a_bad", state.p1Digest, "docs-contract", p2Generation)
		badWaiver.Authority = "repository_policy"
		if _, err := state.client.CallV2(context.Background(), badWaiver); err == nil {
			t.Fatal("repository proposal self-granted waiver authority")
		}
	})
	writeVerificationFile(t, state.repo, "activation-cut-2.txt", "cut-2\n")
	if cut := inspectVerificationSemantics(t, state.client, state.workspace, "p2-post-cut"); cut.SourceGeneration == p2Generation {
		t.Fatalf("p2 activation cut did not transition generation: %q", p2Generation)
	}
	p2Activate := verificationActivationRequest(state.workspace, "act_stage_a_p2", p2Digest, state.p1Digest, p2Generation, "stage-a")
	p2Result := activateVerificationPolicy(t, state.client, p2Activate, "p2-activate")
	if !p2Result.Effective || p2Result.Record.PreviousEffectiveDigest != state.p1Digest {
		t.Fatalf("p2 activation=%#v", p2Result)
	}
	t.Run("TestActivationRetryNeverRollsCurrentIndexBackward", func(t *testing.T) {
		old := activateVerificationPolicy(t, state.client, state.p1Activate, "p1-superseded-retry")
		current := inspectVerificationSemantics(t, state.client, state.workspace, "after-old-retry")
		if !old.Replayed || old.Effective || !old.Record.ActivatedAt.Equal(state.p1Result.Record.ActivatedAt) || current.EffectivePolicy == nil || current.EffectivePolicy.Digest != p2Digest || hasVerificationPolicyGap(current, "security_sensitive", "internal/auth/a.go") {
			t.Fatalf("superseded replay/current old=%#v current=%#v", old, current)
		}
	})
}
func initVerificationSemanticsRepo(t *testing.T) string {
	t.Helper()
	repo := initWorkspaceCLIRepo(t)
	writeVerificationFile(t, repo, "docs/guide.md", "# Guide\n")
	writeVerificationFile(t, repo, "internal/auth/a.go", "package auth\n\nfunc A() int { return 1 }\n")
	writeVerificationFile(t, repo, "password.go", "package root\n")
	runWorkspaceGit(t, repo, "add", "docs/guide.md", "internal/auth/a.go", "password.go")
	runWorkspaceGit(t, repo, "commit", "-m", "seed verification semantics fixtures")
	return repo
}

func writeVerificationPolicy(t *testing.T, repo, contents string) {
	t.Helper()
	writeVerificationFile(t, repo, ".shellbeam/verification-policy.toml", contents)
}

func writeVerificationFile(t *testing.T, repo, rel, contents string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func inspectVerificationSemantics(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, id string) verificationapp.Inspection {
	t.Helper()
	req := ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: id, Action: "inspect.verification", WorkspaceID: string(workspace.ID)}
	req.Phase = verificationcore.PhaseCheckpoint
	response, err := client.CallV2(context.Background(), req)
	if err != nil || !response.OK || response.Verification == nil {
		t.Fatalf("inspect.verification %s response=%#v err=%v", id, response, err)
	}
	return *response.Verification
}

func previewVerificationPolicy(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, profile, id string) verificationapp.PolicyPreview {
	t.Helper()
	req := ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: id, Action: "verification.policy.preview", WorkspaceID: string(workspace.ID)}
	req.Profile = profile
	response, err := client.CallV2(context.Background(), req)
	if err != nil || !response.OK || response.VerificationPolicyPreview == nil {
		t.Fatalf("verification.policy.preview %s response=%#v err=%v", id, response, err)
	}
	return *response.VerificationPolicyPreview
}

func verificationActivationRequest(workspace workspacecore.Workspace, activationID, proposed, previous, generation, actor string) ipcadapter.RequestV2 {
	req := ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: activationID, Action: "verification.policy.activate", WorkspaceID: string(workspace.ID)}
	req.ActivationID = activationID
	req.ProposedPolicyDigest = proposed
	req.ExpectedPreviousPolicyDigest = previous
	req.ProposalGeneration = generation
	req.Authority = verificationapp.AuthorityExplicitCaller
	req.Actor = actor
	return req
}

func activateVerificationPolicy(t *testing.T, client *ipcadapter.Client, req ipcadapter.RequestV2, id string) verificationcore.ActivationWriteResult {
	t.Helper()
	req.RequestID = id
	response, err := client.CallV2(context.Background(), req)
	if err != nil || !response.OK || response.VerificationActivation == nil {
		t.Fatalf("verification.policy.activate %s response=%#v err=%v", id, response, err)
	}
	return *response.VerificationActivation
}

func verificationWaiverRequest(workspace workspacecore.Workspace, waiverID, digest, ruleID, generation string) ipcadapter.RequestV2 {
	req := ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: waiverID, Action: "verification.waiver.set", WorkspaceID: string(workspace.ID)}
	req.WaiverID = waiverID
	req.PolicyDigest = digest
	req.RuleID = ruleID
	req.Phase = verificationcore.PhaseCheckpoint
	req.Generation = generation
	req.Authority = verificationapp.AuthorityExplicitCaller
	req.Actor = "stage-a"
	req.Reason = "stage-a acceptance"
	return req
}

func hasVerificationPolicyGap(inspection verificationapp.Inspection, class, path string) bool {
	for _, gap := range inspection.PolicyGaps {
		if gap.DeclaredClass == class && gap.SurfaceRef == path {
			return true
		}
	}
	return false
}

func assertVerificationInspectionHasNoCompletionTruth(t *testing.T, inspection verificationapp.Inspection) {
	t.Helper()
	data, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"task_complete", "work_complete", "safe_to_finish", "gate_status"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("verification inspection leaked completion truth %q: %s", forbidden, data)
		}
	}
}

const verificationSemanticsMixedPolicy = `schema_version = 1
policy_id = "stage-a-mixed"

[[classifications]]
id = "security"
paths = ["internal/auth/**"]
surface_class = "security_sensitive"

[[rules]]
id = "docs-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs_contract"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "docs-format"
provider_class = "static_format_check"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"

[[rules]]
id = "go-contract"
phases = ["checkpoint"]
match_paths = ["a/**", "b/**", "c/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "go_behavior"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "go-focused"
provider_class = "focused_behavior_test"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
`

func TestVerificationSemanticsAffectedObligations(t *testing.T) {
	repo := initVerificationSemanticsGraphRepo(t, verificationSemanticsMixedPolicy)
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, repo, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)
	digest := activateCommittedVerificationPolicy(t, client, workspace, repo, "mixed")
	verificationAffectedSelectionScenarios(t, client, workspace, repo, digest)
	verificationWaiverAndPartialScenarios(t, client, workspace, repo, digest)
}

func verificationAffectedSelectionScenarios(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, repo, digest string) {
	t.Helper()
	t.Run("docs-only selects only docs rule", func(t *testing.T) {
		settleVerificationWorkspace(t, client, workspace, repo, "docs-clean")
		appendVerificationFile(t, repo, "docs/guide.md", "docs change\n")
		inspection := inspectVerificationSemantics(t, client, workspace, "docs-only")
		docs, goRule := requireVerificationObligation(t, inspection, "docs-contract"), requireVerificationObligation(t, inspection, "go-contract")
		if docs.Disposition != verificationcore.DispositionRequiredNow || goRule.Disposition != verificationcore.DispositionNotTriggered || docs.PolicyDigest != digest || goRule.PolicyDigest != digest {
			t.Fatalf("docs=%#v go=%#v", docs, goRule)
		}
	})
	t.Run("shared Go package change projects importer relations with independent authority and coverage", func(t *testing.T) {
		settleVerificationWorkspace(t, client, workspace, repo, "go-clean")
		appendVerificationFile(t, repo, "a/a.go", "\n// changed\n")
		inspection := inspectVerificationSemantics(t, client, workspace, "go-shared")
		goRule, domain := requireVerificationObligation(t, inspection, "go-contract"), requireAffectedDomain(t, inspection, verificationcore.DomainGoImportGraph)
		if goRule.Disposition != verificationcore.DispositionRequiredNow || domain.DerivationAuthority != verificationcore.AuthorityMechanical || domain.Coverage != verificationcore.CoverageComplete {
			t.Fatalf("go obligation=%#v domain=%#v", goRule, domain)
		}
		if inspection.Affected.RelationCount < 3 || inspection.Affected.ByAuthority[verificationcore.AuthorityMechanical] != inspection.Affected.RelationCount || inspection.Affected.ByCoverage[verificationcore.CoverageComplete] != inspection.Affected.RelationCount {
			t.Fatalf("affected summary=%#v", inspection.Affected)
		}
	})
	t.Run("declared security-sensitive path without rule emits advisory policy gap", func(t *testing.T) {
		settleVerificationWorkspace(t, client, workspace, repo, "security-clean")
		appendVerificationFile(t, repo, "internal/auth/a.go", "\n// auth changed\n")
		inspection := inspectVerificationSemantics(t, client, workspace, "security-gap")
		if !hasVerificationPolicyGap(inspection, "security_sensitive", "internal/auth/a.go") {
			t.Fatalf("security gap missing: %#v", inspection.PolicyGaps)
		}
	})
	t.Run("password.go outside declared classification emits no security gap", func(t *testing.T) {
		settleVerificationWorkspace(t, client, workspace, repo, "password-clean")
		appendVerificationFile(t, repo, "password.go", "\n// password changed\n")
		inspection := inspectVerificationSemantics(t, client, workspace, "password-outside")
		if hasVerificationPolicyGap(inspection, "security_sensitive", "password.go") || len(inspection.PolicyGaps) != 0 {
			t.Fatalf("outside path invented security gap: %#v", inspection.PolicyGaps)
		}
	})
}

func verificationWaiverAndPartialScenarios(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, repo, digest string) {
	t.Helper()
	t.Run("valid waiver changes disposition but never evidence truth", func(t *testing.T) {
		settleVerificationWorkspace(t, client, workspace, repo, "waiver-clean")
		appendVerificationFile(t, repo, "docs/guide.md", "waiver change\n")
		before := inspectVerificationSemantics(t, client, workspace, "waiver-before")
		docs := requireVerificationObligation(t, before, "docs-contract")
		if docs.Disposition != verificationcore.DispositionRequiredNow || docs.EvidenceStatus != verificationcore.EvidenceNotEvaluated {
			t.Fatalf("before waiver=%#v", docs)
		}
		req := verificationWaiverRequest(workspace, "wv_stage_a_docs", digest, "docs-contract", before.SourceGeneration)
		response, err := client.CallV2(context.Background(), req)
		if err != nil || !response.OK || response.VerificationWaiver == nil || !response.VerificationWaiver.Active {
			t.Fatalf("waiver response=%#v err=%v", response, err)
		}
		docs = requireVerificationObligation(t, inspectVerificationSemantics(t, client, workspace, "waiver-after"), "docs-contract")
		if docs.Disposition != verificationcore.DispositionWaived || docs.WaiverID != "wv_stage_a_docs" || docs.EvidenceStatus != verificationcore.EvidenceNotEvaluated {
			t.Fatalf("waiver rewrote evidence truth: %#v", docs)
		}
	})
	t.Run("partial relation provider cannot narrow mandatory obligation", func(t *testing.T) {
		settleVerificationWorkspace(t, client, workspace, repo, "partial-clean")
		writeVerificationFile(t, repo, "nested/go.mod", "module example.com/nested\n")
		appendVerificationFile(t, repo, "docs/guide.md", "partial change\n")
		inspection := inspectVerificationSemantics(t, client, workspace, "partial-go")
		domain, goRule := requireAffectedDomain(t, inspection, verificationcore.DomainGoImportGraph), requireVerificationObligation(t, inspection, "go-contract")
		if domain.Coverage == verificationcore.CoverageComplete || goRule.Disposition != verificationcore.DispositionRequiredNow {
			t.Fatalf("partial domain=%#v narrowed rule=%#v", domain, goRule)
		}
	})
}
func initVerificationSemanticsGraphRepo(t *testing.T, policy string) string {
	t.Helper()
	repo := initVerificationSemanticsRepo(t)
	writeVerificationFile(t, repo, "go.mod", "module example.com/repo\n\ngo 1.24\n")
	writeVerificationFile(t, repo, "a/a.go", "package a\n")
	writeVerificationFile(t, repo, "b/b.go", "package b\nimport _ \"example.com/repo/a\"\n")
	writeVerificationFile(t, repo, "c/c.go", "package c\nimport _ \"example.com/repo/b\"\n")
	writeVerificationPolicy(t, repo, policy)
	runWorkspaceGit(t, repo, "add", "go.mod", "a/a.go", "b/b.go", "c/c.go", ".shellbeam/verification-policy.toml")
	runWorkspaceGit(t, repo, "commit", "-m", "seed mixed verification policy")
	return repo
}

func activateCommittedVerificationPolicy(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, repo, label string) string {
	t.Helper()
	preview := previewVerificationPolicy(t, client, workspace, "", label+"-preview")
	if preview.State != verificationapp.PolicyLoadValid || preview.Proposal == nil {
		t.Fatalf("committed policy preview=%#v", preview)
	}
	proposal := inspectVerificationSemantics(t, client, workspace, label+"-proposal")
	if proposal.SourceGeneration == "" {
		t.Fatalf("proposal generation unavailable: %#v", proposal)
	}
	writeVerificationFile(t, repo, "activation-"+label+".cut", "activate\n")
	cut := inspectVerificationSemantics(t, client, workspace, label+"-cut")
	if cut.SourceGeneration == proposal.SourceGeneration {
		t.Fatalf("activation cut failed to change generation %q", proposal.SourceGeneration)
	}
	req := verificationActivationRequest(workspace, "act_"+label, preview.Proposal.Digest, "absent", proposal.SourceGeneration, "stage-a")
	result := activateVerificationPolicy(t, client, req, label+"-activate")
	if !result.Effective {
		t.Fatalf("activation=%#v", result)
	}
	return preview.Proposal.Digest
}

func settleVerificationWorkspace(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace, repo, label string) {
	t.Helper()
	runWorkspaceGit(t, repo, "reset", "--hard", "HEAD")
	runWorkspaceGit(t, repo, "clean", "-fd")
	_ = inspectVerificationSemantics(t, client, workspace, label+"-resolved")
	clean := inspectVerificationSemantics(t, client, workspace, label+"-clean")
	if clean.Affected.RelationCount != 0 {
		t.Fatalf("workspace did not settle clean: %#v", clean.Affected)
	}
}

func appendVerificationFile(t *testing.T, repo, rel, contents string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(rel))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(contents); err != nil {
		t.Fatal(err)
	}
}

func requireVerificationObligation(t *testing.T, inspection verificationapp.Inspection, ruleID string) verificationcore.VerificationObligation {
	t.Helper()
	for _, obligation := range inspection.Obligations {
		if obligation.SourceRuleID == ruleID {
			return obligation
		}
	}
	t.Fatalf("obligation %q missing: %#v", ruleID, inspection.Obligations)
	return verificationcore.VerificationObligation{}
}

func requireAffectedDomain(t *testing.T, inspection verificationapp.Inspection, kind verificationcore.AffectedDomainKind) verificationcore.AffectedDomain {
	t.Helper()
	for _, domain := range inspection.Affected.Domains {
		if domain.Kind == kind {
			return domain
		}
	}
	t.Fatalf("affected domain %q missing: %#v", kind, inspection.Affected.Domains)
	return verificationcore.AffectedDomain{}
}

func TestVerificationSemanticsTypedStarterAndNoSpawn(t *testing.T) {
	repo := initVerificationSemanticsRepo(t)
	sentinel := filepath.Join(repo, "VERIFICATION_MUST_NOT_EXECUTE")
	writeVerificationProjectManifest(t, repo, sentinel, false, 2)
	runWorkspaceGit(t, repo, "add", ".shellbeam/project.toml")
	runWorkspaceGit(t, repo, "commit", "-m", "seed typed project manifest")

	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, repo, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)

	starter := previewVerificationPolicy(t, client, workspace, "team", "starter-team")
	if starter.State != verificationapp.PolicyLoadValid || starter.Proposal == nil || starter.Proposal.Origin != verificationcore.ProposalStarterProfile || starter.Proposal.ProfileOrigin != "shellbeam/team@v1" || starter.RenderedTOML == "" {
		t.Fatalf("starter preview=%#v", starter)
	}
	if len(starter.Proposal.Content.Rules) != 2 || !containsVerificationAdvisory(starter.Advisories, "typed_binding_shell_unsupported:shell") {
		t.Fatalf("starter rules=%#v advisories=%v", starter.Proposal.Content.Rules, starter.Advisories)
	}
	assertVerificationSentinelAbsent(t, sentinel)

	writeVerificationPolicy(t, repo, starter.RenderedTOML)
	repositoryPreview := previewVerificationPolicy(t, client, workspace, "", "starter-roundtrip")
	if repositoryPreview.State != verificationapp.PolicyLoadValid || repositoryPreview.Proposal == nil || repositoryPreview.Proposal.Origin != verificationcore.ProposalRepositoryAuthored || repositoryPreview.Proposal.ProfileOrigin != starter.Proposal.ProfileOrigin || repositoryPreview.Proposal.Digest != starter.Proposal.Digest {
		t.Fatalf("starter roundtrip preview=%#v starter=%#v", repositoryPreview, starter)
	}
	proposal := inspectVerificationSemantics(t, client, workspace, "starter-proposal")
	writeVerificationFile(t, repo, "starter-activation.cut", "activate starter\n")
	cut := inspectVerificationSemantics(t, client, workspace, "starter-cut")
	if cut.SourceGeneration == proposal.SourceGeneration {
		t.Fatalf("starter activation cut did not transition generation: %q", proposal.SourceGeneration)
	}
	activate := verificationActivationRequest(workspace, "act_starter", starter.Proposal.Digest, "absent", proposal.SourceGeneration, "stage-a")
	activation := activateVerificationPolicy(t, client, activate, "starter-activate")
	if !activation.Effective {
		t.Fatalf("starter activation=%#v", activation)
	}
	assertVerificationSentinelAbsent(t, sentinel)

	t.Run("manifest-v2 fixed zero-param argv backs activated requirement", func(t *testing.T) {
		inspection := inspectVerificationSemantics(t, client, workspace, "starter-effective")
		checkpoint := requireVerificationObligation(t, inspection, "starter_checkpoint_fixed")
		if checkpoint.Disposition != verificationcore.DispositionRequiredNow || checkpoint.EvidenceStatus != verificationcore.EvidenceNotEvaluated || len(checkpoint.EvidenceRequirements) != 1 || checkpoint.EvidenceRequirements[0].ExpectedProjectBindingDigest == "" {
			t.Fatalf("fixed binding obligation=%#v", checkpoint)
		}
		assertVerificationSentinelAbsent(t, sentinel)
	})

	t.Run("TestVerificationInspectionNeverSpawnsProviders", func(t *testing.T) {
		_ = inspectVerificationSemantics(t, client, workspace, "no-spawn-inspect")
		_ = previewVerificationPolicy(t, client, workspace, "team", "no-spawn-preview")
		assertVerificationSentinelAbsent(t, sentinel)
	})

	t.Run("TestPinnedPolicyUnaffectedByStarterTemplateChange", func(t *testing.T) {
		writeVerificationProjectManifest(t, repo, sentinel, true, 2)
		changedStarter := previewVerificationPolicy(t, client, workspace, "team", "starter-changed-input")
		if changedStarter.Proposal == nil || changedStarter.Proposal.Digest == starter.Proposal.Digest {
			t.Fatalf("changed starter did not produce distinct proposal: old=%s new=%#v", starter.Proposal.Digest, changedStarter.Proposal)
		}
		inspection := inspectVerificationSemantics(t, client, workspace, "pinned-after-starter-change")
		if inspection.EffectivePolicy == nil || inspection.EffectivePolicy.Digest != starter.Proposal.Digest {
			t.Fatalf("starter input change mutated pinned effective policy: %#v", inspection.EffectivePolicy)
		}
		assertVerificationSentinelAbsent(t, sentinel)
	})

	t.Run("manifest-v1 and shell-form stay advisory-only for starter binding", func(t *testing.T) {
		writeVerificationProjectManifest(t, repo, sentinel, false, 1)
		v1 := previewVerificationPolicy(t, client, workspace, "team", "starter-v1")
		if v1.Proposal == nil || len(v1.Proposal.Content.Rules) != 0 || !containsVerificationAdvisoryPrefix(v1.Advisories, "typed_binding_requires_manifest_v2:") {
			t.Fatalf("manifest v1 typed binding was not rejected: proposal=%#v advisories=%v", v1.Proposal, v1.Advisories)
		}
		assertVerificationSentinelAbsent(t, sentinel)
	})
}

func writeVerificationProjectManifest(t *testing.T, repo, sentinel string, includeExtra bool, schemaVersion int) {
	t.Helper()
	coding := `["fixed", "shell"]`
	extra := ""
	if includeExtra {
		coding = `["fixed", "shell", "extra"]`
		extra = fmt.Sprintf(`
[commands.extra]
argv = ["/usr/bin/touch", %q]
cwd = "."
kind = "test"
source_scope = "full"
`, sentinel+"-EXTRA")
	}
	manifest := fmt.Sprintf(`schema_version = %d

[commands.fixed]
argv = ["/usr/bin/touch", %q]
cwd = "."
kind = "test"
source_scope = "full"

[commands.shell]
shell = %q
cwd = "."
kind = "test"
source_scope = "full"
%s
[verification.profiles.coding]
steps = %s

[verification.profiles.checkpoint]
steps = ["fixed"]

[verification.profiles.release]
steps = ["fixed"]
`, schemaVersion, sentinel, "touch "+sentinel+"-SHELL", extra, coding)
	writeVerificationFile(t, repo, ".shellbeam/project.toml", manifest)
}

func assertVerificationSentinelAbsent(t *testing.T, path string) {
	t.Helper()
	for _, candidate := range []string{path, path + "-SHELL", path + "-EXTRA"} {
		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			t.Fatalf("verification executed project command; sentinel=%s err=%v", candidate, err)
		}
	}
}

func containsVerificationAdvisory(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsVerificationAdvisoryPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func TestVerificationSemanticsPracticalDocsOnlyMeasurement(t *testing.T) {
	repo := initVerificationSemanticsGraphRepo(t, verificationSemanticsMixedPolicy)
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, repo, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)
	_ = activateCommittedVerificationPolicy(t, client, workspace, repo, "practical")
	settleVerificationWorkspace(t, client, workspace, repo, "practical-clean")

	for i := 1; i <= 4; i++ {
		writeVerificationFile(t, repo, fmt.Sprintf("docs/superpowers/specs/changed-%d.md", i), fmt.Sprintf("# Changed %d\n", i))
	}
	started := time.Now()
	inspection := inspectVerificationSemantics(t, client, workspace, "practical-docs-only")
	elapsed := time.Since(started)
	data, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	docs := requireVerificationObligation(t, inspection, "docs-contract")
	goRule := requireVerificationObligation(t, inspection, "go-contract")
	if docs.Disposition != verificationcore.DispositionRequiredNow || len(docs.EvidenceRequirements) != 1 || docs.EvidenceRequirements[0].Requirement.ProviderClass != verificationcore.ProviderStaticFormatCheck {
		t.Fatalf("docs practical obligation=%#v", docs)
	}
	if goRule.Disposition != verificationcore.DispositionNotTriggered {
		t.Fatalf("broad Go obligation triggered for docs-only fixture: %#v", goRule)
	}
	t.Logf("PRACTICAL scenario=docs_only_four_markdown_specs required_rule=%s provider=%s go_rule=%s relation_count=%d mechanical_relations=%d complete_relations=%d model_visible_bytes=%d tool_call_count=1 inspection_wall_ms=%.3f",
		docs.SourceRuleID, docs.EvidenceRequirements[0].Requirement.ProviderClass, goRule.Disposition,
		inspection.Affected.RelationCount, inspection.Affected.ByAuthority[verificationcore.AuthorityMechanical], inspection.Affected.ByCoverage[verificationcore.CoverageComplete], len(data), float64(elapsed.Microseconds())/1000,
	)
}
