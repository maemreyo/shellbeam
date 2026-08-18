package verification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	app "github.com/maemreyo/shellbeam/internal/app/verification"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

const MaxPolicyBytes = 64 << 10
const (
	CodePolicyAbsent      = "verification_policy_absent"
	CodePolicyInvalid     = "verification_policy_invalid"
	CodePolicyUnsupported = "verification_policy_unsupported"
	CodePolicyPathEscape  = "verification_policy_path_escape"
	CodePolicyTooLarge    = "verification_policy_too_large"
)

type PolicyLoadState = app.PolicyLoadState
type PolicyLoadResult = app.PolicyLoadResult

const (
	PolicyLoadAbsent      = app.PolicyLoadAbsent
	PolicyLoadValid       = app.PolicyLoadValid
	PolicyLoadInvalid     = app.PolicyLoadInvalid
	PolicyLoadUnsupported = app.PolicyLoadUnsupported
)

type PolicyLoader struct{}

func NewPolicyLoader() *PolicyLoader { return &PolicyLoader{} }

type rawPolicy struct {
	SchemaVersion   *int                `toml:"schema_version"`
	PolicyID        string              `toml:"policy_id"`
	ProfileOrigin   string              `toml:"profile_origin"`
	Classifications []rawClassification `toml:"classifications"`
	Rules           []rawRule           `toml:"rules"`
}
type rawClassification struct {
	ID           string   `toml:"id"`
	Paths        []string `toml:"paths"`
	SurfaceClass string   `toml:"surface_class"`
}
type rawRule struct {
	ID                       string        `toml:"id"`
	Phases                   []string      `toml:"phases"`
	MatchClasses             []string      `toml:"match_classes"`
	MatchPaths               []string      `toml:"match_paths"`
	Ownership                string        `toml:"ownership"`
	RiskClass                string        `toml:"risk_class"`
	Required                 bool          `toml:"required"`
	SufficiencyBasis         string        `toml:"sufficiency_basis"`
	MinimumAffectedAuthority string        `toml:"minimum_affected_authority"`
	Evidence                 []rawEvidence `toml:"evidence"`
}
type rawEvidence struct {
	ID                string            `toml:"id"`
	ProviderClass     string            `toml:"provider_class"`
	ProjectCommandID  string            `toml:"project_command_id"`
	Params            map[string]string `toml:"params"`
	MinimumAuthority  string            `toml:"minimum_authority"`
	RequireCurrent    bool              `toml:"require_current"`
	Environment       string            `toml:"environment"`
	Stability         string            `toml:"stability"`
	Flake             *rawFlake         `toml:"flake"`
	RequireQuiescence bool              `toml:"require_quiescence"`
	Execution         rawExecution      `toml:"execution"`
}
type rawFlake struct {
	Runs        int `toml:"runs"`
	MinPasses   int `toml:"min_passes"`
	MaxFailures int `toml:"max_failures"`
}
type rawExecution struct {
	ParallelSafe           *bool    `toml:"parallel_safe"`
	SharedResources        []string `toml:"shared_resources"`
	ExclusiveResourceClass string   `toml:"exclusive_resource_class"`
	ExpectedWorkloadClass  string   `toml:"expected_workload_class"`
}

func (l *PolicyLoader) Load(ctx context.Context, w workspace.Workspace) app.PolicyLoadResult {
	if err := ctx.Err(); err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyInvalid}
	}
	root := filepath.Clean(w.Root)
	if !filepath.IsAbs(root) {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyPathEscape}
	}
	filePath := filepath.Join(root, ".shellbeam", "verification-policy.toml")
	info, err := os.Lstat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return app.PolicyLoadResult{State: app.PolicyLoadAbsent, Code: CodePolicyAbsent}
	}
	if err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyInvalid}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, e := filepath.EvalSymlinks(filePath)
		if e != nil || !withinRoot(root, target) {
			return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyPathEscape}
		}
		filePath = target
		info, err = os.Stat(filePath)
		if err != nil {
			return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyInvalid}
		}
	}
	if !info.Mode().IsRegular() {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyInvalid}
	}
	if info.Size() > MaxPolicyBytes {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyTooLarge}
	}
	f, err := os.Open(filePath)
	if err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyInvalid}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxPolicyBytes+1))
	if err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, Code: CodePolicyInvalid}
	}
	digest := rawDigest(data)
	if len(data) > MaxPolicyBytes {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, RawDigest: digest, Code: CodePolicyTooLarge}
	}
	if !utf8.Valid(data) {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, RawDigest: digest, Code: CodePolicyInvalid}
	}
	var header struct {
		SchemaVersion *int `toml:"schema_version"`
	}
	if err := toml.Unmarshal(data, &header); err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, RawDigest: digest, Code: CodePolicyInvalid}
	}
	if header.SchemaVersion == nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, RawDigest: digest, Code: CodePolicyInvalid}
	}
	if *header.SchemaVersion != 1 {
		return app.PolicyLoadResult{State: app.PolicyLoadUnsupported, RawDigest: digest, Code: CodePolicyUnsupported}
	}
	var raw rawPolicy
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, RawDigest: digest, Code: CodePolicyInvalid}
	}
	content, err := convertRawPolicy(raw)
	if err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, RawDigest: digest, Code: CodePolicyInvalid}
	}
	policyDigest, err := core.PolicyDigest(content)
	if err != nil {
		return app.PolicyLoadResult{State: app.PolicyLoadInvalid, RawDigest: digest, Code: CodePolicyInvalid}
	}
	proposal := core.PolicyProposal{RepositoryID: string(w.RepositoryID), Digest: policyDigest, Origin: core.ProposalRepositoryAuthored, ProfileOrigin: raw.ProfileOrigin, Content: content}
	return app.PolicyLoadResult{State: app.PolicyLoadValid, Proposal: &proposal, RawDigest: digest}
}

func rawDigest(data []byte) string {
	s := sha256.Sum256(data)
	return "sha256_" + hex.EncodeToString(s[:])
}
func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

var paramID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func validateGlob(v string) bool {
	if v == "" || len(v) > 1024 || !utf8.ValidString(v) || path.IsAbs(v) || strings.Contains(v, "\\") {
		return false
	}
	for _, p := range strings.Split(v, "/") {
		if p == ".." || p == "." || p == "" {
			return false
		}
	}
	_, err := path.Match(v, "probe/path.go")
	return err == nil
}
func convertRawPolicy(r rawPolicy) (core.PolicyContent, error) {
	out := core.PolicyContent{SchemaVersion: *r.SchemaVersion, PolicyID: r.PolicyID}
	for _, c := range r.Classifications {
		for _, p := range c.Paths {
			if !validateGlob(p) {
				return out, errors.New("invalid glob")
			}
		}
		out.Classifiers = append(out.Classifiers, core.Classification{ID: c.ID, Paths: append([]string(nil), c.Paths...), SurfaceClass: c.SurfaceClass})
	}
	for _, rr := range r.Rules {
		for _, p := range rr.MatchPaths {
			if !validateGlob(p) {
				return out, errors.New("invalid match glob")
			}
		}
		rule := core.Rule{ID: rr.ID, MatchClasses: append([]string(nil), rr.MatchClasses...), MatchPaths: append([]string(nil), rr.MatchPaths...), Ownership: core.OwnershipClass(rr.Ownership), RiskClass: core.RiskClass(rr.RiskClass), Required: rr.Required, SufficiencyBasis: rr.SufficiencyBasis, MinimumAffectedAuthority: core.DerivationAuthority(rr.MinimumAffectedAuthority)}
		for _, p := range rr.Phases {
			rule.Phases = append(rule.Phases, core.Phase(p))
		}
		for _, re := range rr.Evidence {
			if len(re.Params) > 32 {
				return out, errors.New("params limit")
			}
			for k, v := range re.Params {
				if !paramID.MatchString(k) || v == "" || len(v) > 1024 || !utf8.ValidString(v) {
					return out, errors.New("invalid param")
				}
			}
			ev := core.EvidenceRequirement{ID: re.ID, ProviderClass: core.ProviderClass(re.ProviderClass), ProjectCommandID: re.ProjectCommandID, Params: re.Params, MinimumAuthority: core.DerivationAuthority(re.MinimumAuthority), RequireCurrent: re.RequireCurrent, Environment: core.EnvironmentRequirement(re.Environment), Stability: core.StabilityRequirement(re.Stability), RequireQuiescence: re.RequireQuiescence, Execution: core.ProviderExecutionSemantics{ParallelSafe: re.Execution.ParallelSafe, SharedResources: re.Execution.SharedResources, ExclusiveResourceClass: re.Execution.ExclusiveResourceClass, ExpectedWorkloadClass: re.Execution.ExpectedWorkloadClass}}
			if re.Flake != nil {
				ev.Flake = &core.FlakeProtocol{Runs: re.Flake.Runs, MinPasses: re.Flake.MinPasses, MaxFailures: re.Flake.MaxFailures}
			}
			rule.Evidence = append(rule.Evidence, ev)
		}
		out.Rules = append(out.Rules, rule)
	}
	if err := out.Validate(); err != nil {
		return out, err
	}
	return out, nil
}
