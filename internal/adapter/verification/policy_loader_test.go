package verification

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func testWorkspace(root string) workspace.Workspace {
	return workspace.Workspace{RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"), Root: root}
}
func writePolicy(t *testing.T, root, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".shellbeam"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".shellbeam", "verification-policy.toml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

const validPolicyTOML = `schema_version = 1
policy_id = "team-policy"
profile_origin = "shellbeam/team@v1"

[[classifications]]
id = "documentation"
paths = ["docs/**"]
surface_class = "documentation"

[[rules]]
id = "docs-contract"
phases = ["inner_loop", "checkpoint"]
match_classes = ["documentation"]
ownership = "application_owned"
required = true
sufficiency_basis = "repository_markdown_contract"
minimum_affected_authority = "mechanical"

[[rules.evidence]]
id = "docs-markdown"
provider_class = "project_command"
project_command_id = "docs_contract"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
execution = { parallel_safe = true, expected_workload_class = "light" }
`

func TestPolicyLoaderAbsentValidAndStrictUnknownField(t *testing.T) {
	l := NewPolicyLoader()
	root := t.TempDir()
	if got := l.Load(context.Background(), testWorkspace(root)); got.State != PolicyLoadAbsent {
		t.Fatalf("absent=%#v", got)
	}
	writePolicy(t, root, validPolicyTOML)
	got := l.Load(context.Background(), testWorkspace(root))
	if got.State != PolicyLoadValid || got.Proposal == nil || got.Proposal.Origin != core.ProposalRepositoryAuthored || got.Proposal.ProfileOrigin != "shellbeam/team@v1" || got.Proposal.Digest == "" {
		t.Fatalf("valid=%#v", got)
	}
	writePolicy(t, root, validPolicyTOML+"\nunknown_field = true\n")
	if got := l.Load(context.Background(), testWorkspace(root)); got.State != PolicyLoadInvalid {
		t.Fatalf("unknown=%#v", got)
	}
}

func TestPolicyLoaderRejectsEscapingSymlinkAndOversize(t *testing.T) {
	l := NewPolicyLoader()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(outside, []byte(validPolicyTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".shellbeam"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".shellbeam", "verification-policy.toml")); err != nil {
		t.Fatal(err)
	}
	if got := l.Load(context.Background(), testWorkspace(root)); got.State != PolicyLoadInvalid || got.Code != CodePolicyPathEscape {
		t.Fatalf("symlink=%#v", got)
	}
	root = t.TempDir()
	writePolicy(t, root, strings.Repeat("x", MaxPolicyBytes+1))
	if got := l.Load(context.Background(), testWorkspace(root)); got.State != PolicyLoadInvalid || got.Code != CodePolicyTooLarge {
		t.Fatalf("large=%#v", got)
	}
}

func TestPolicyLoaderUnsupportedAndSemanticValidation(t *testing.T) {
	cases := map[string]struct {
		text  string
		state PolicyLoadState
	}{
		"unsupported":   {strings.Replace(validPolicyTOML, "schema_version = 1", "schema_version = 2", 1), PolicyLoadUnsupported},
		"missing basis": {strings.Replace(validPolicyTOML, "sufficiency_basis = \"repository_markdown_contract\"\n", "", 1), PolicyLoadInvalid},
		"parent glob":   {strings.Replace(validPolicyTOML, "docs/**", "../docs/**", 1), PolicyLoadInvalid},
		"absolute glob": {strings.Replace(validPolicyTOML, "docs/**", "/docs/**", 1), PolicyLoadInvalid},
		"bad param key": {validPolicyTOML + "\n[rules.evidence.params]\n\"BAD KEY\" = \"x\"\n", PolicyLoadInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writePolicy(t, root, tc.text)
			got := NewPolicyLoader().Load(context.Background(), testWorkspace(root))
			if got.State != tc.state {
				t.Fatalf("got=%#v", got)
			}
		})
	}
}

func TestPolicyLoaderRejectsDuplicateIDsAndFlakeMismatch(t *testing.T) {
	dup := validPolicyTOML + `\n[[rules]]\nid = "docs-contract"\nphases=["checkpoint"]\nownership="application_owned"\nrequired=true\nsufficiency_basis="dup"\nminimum_affected_authority="mechanical"\n[[rules.evidence]]\nid="x"\nprovider_class="project_command"\nproject_command_id="docs_contract"\nminimum_authority="mechanical"\nrequire_current=true\nenvironment="none"\nstability="no_contradiction"\n`
	root := t.TempDir()
	writePolicy(t, root, dup)
	if got := NewPolicyLoader().Load(context.Background(), testWorkspace(root)); got.State != PolicyLoadInvalid {
		t.Fatalf("dup=%#v", got)
	}
	badFlake := strings.Replace(validPolicyTOML, "stability = \"no_contradiction\"", "stability = \"flake_protocol\"", 1)
	root = t.TempDir()
	writePolicy(t, root, badFlake)
	if got := NewPolicyLoader().Load(context.Background(), testWorkspace(root)); got.State != PolicyLoadInvalid {
		t.Fatalf("flake=%#v", got)
	}
}
