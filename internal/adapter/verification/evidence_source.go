package verification

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

const maxEvidenceCandidatePages = 4

var evidenceProjectCommandIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type evidenceInspector interface {
	Inspect(context.Context, evidenceapp.InspectRequest) (evidenceapp.InspectResult, error)
}

type EvidenceSource struct{ inspector evidenceInspector }

func NewEvidenceSource(inspector evidenceInspector) *EvidenceSource {
	return &EvidenceSource{inspector: inspector}
}

func (s *EvidenceSource) Candidates(ctx context.Context, query verificationapp.CandidateQuery) (verificationapp.CandidateResultSet, error) {
	maxRecords, commandSet, err := normalizeCandidateQuery(query)
	if err != nil {
		return verificationapp.CandidateResultSet{}, err
	}
	if s == nil || s.inspector == nil {
		return verificationapp.CandidateResultSet{Coverage: core.CoverageUnknown, Diagnostics: []string{"evidence_inspector_unavailable"}}, nil
	}
	out := verificationapp.CandidateResultSet{Coverage: core.CoverageComplete}
	continuation := ""
	for page := 0; page < maxEvidenceCandidatePages; page++ {
		result, err := s.inspector.Inspect(ctx, evidenceapp.InspectRequest{Filter: evidenceapp.InspectFilter{WorkspaceID: query.WorkspaceID, ActivityID: query.ActivityID}, MaxRecords: evidence.MaxInspectRecords, Continuation: continuation})
		if err != nil {
			return verificationapp.CandidateResultSet{}, err
		}
		if result.Status == evidenceapp.InspectUnavailable {
			return verificationapp.CandidateResultSet{Coverage: core.CoverageUnknown, Diagnostics: []string{"evidence_inspector_unavailable"}}, nil
		}
		capped, err := appendCandidateViews(&out, result.Records, commandSet, maxRecords)
		if err != nil {
			return verificationapp.CandidateResultSet{}, err
		}
		if capped {
			out.Coverage = core.CoverageBounded
			out.Diagnostics = append(out.Diagnostics, "evidence_history_candidate_limit")
			return finalizeCandidateResult(out), nil
		}
		if result.Continuation == "" {
			return finalizeCandidateResult(out), nil
		}
		continuation = result.Continuation
	}
	out.Coverage = core.CoverageBounded
	out.Diagnostics = append(out.Diagnostics, "evidence_history_page_limit")
	return finalizeCandidateResult(out), nil
}

func normalizeCandidateQuery(query verificationapp.CandidateQuery) (int, map[string]bool, error) {
	if query.WorkspaceID == "" {
		return 0, nil, fmt.Errorf("candidate query requires workspace id")
	}
	maxRecords := query.MaxRecords
	if maxRecords == 0 {
		maxRecords = evidence.MaxInspectRecords
	}
	if maxRecords < 1 || maxRecords > evidence.MaxInspectRecords {
		return 0, nil, fmt.Errorf("candidate max records out of range")
	}
	commands := make(map[string]bool, len(query.ProjectCommandIDs))
	for _, id := range query.ProjectCommandIDs {
		if !evidenceProjectCommandIDPattern.MatchString(id) {
			return 0, nil, fmt.Errorf("invalid candidate project command id %q", id)
		}
		commands[id] = true
	}
	return maxRecords, commands, nil
}

func appendCandidateViews(out *verificationapp.CandidateResultSet, views []evidenceapp.InspectRecord, commandSet map[string]bool, maxRecords int) (bool, error) {
	for _, view := range views {
		if len(commandSet) != 0 && !commandSet[view.Record.Command.ProjectCommandID] {
			continue
		}
		if len(out.Candidates) >= maxRecords {
			return true, nil
		}
		candidate, err := mapEvidenceCandidate(view)
		if err != nil {
			return false, err
		}
		out.Candidates = append(out.Candidates, candidate)
	}
	return false, nil
}

func mapEvidenceCandidate(view evidenceapp.InspectRecord) (core.EvidenceCandidate, error) {
	r := view.Record
	if err := r.Validate(); err != nil {
		return core.EvidenceCandidate{}, fmt.Errorf("invalid evidence record %q: %w", r.EvidenceID, err)
	}
	candidate := core.EvidenceCandidate{
		EvidenceID: r.EvidenceID, VerificationKind: r.VerificationKind, ProjectCommandID: r.Command.ProjectCommandID,
		OperationID: r.OperationID, SessionID: r.SessionID, ActivityID: r.ActivityID, WorkspaceID: r.WorkspaceID,
		SourceGeneration: r.Source.PostGeneration, SourceContentDigest: r.Source.SourceContentDigest,
		ProjectBindingDigest: r.Command.ProjectBindingDigest, ManifestDigest: r.Command.ManifestDigest,
		ContractDigest: r.ContractDigest, Freshness: mapCandidateFreshness(view.Validity.Freshness), Result: mapCandidateResult(r.Result),
		SemanticContractDigest: r.ContractDigest, CompletedAt: r.CompletedAt,
	}
	if candidate.SourceGeneration == "" {
		candidate.SourceGeneration = r.Source.PreGeneration
	}
	if r.EnvironmentBinding != nil {
		candidate.EnvironmentFingerprint = r.EnvironmentBinding.EnvironmentFingerprint
		candidate.EnvironmentFingerprintVersion = r.EnvironmentBinding.EnvironmentFingerprintVersion
		candidate.ToolchainFingerprint = r.EnvironmentBinding.ToolchainFingerprint
		candidate.ToolchainFingerprintVersion = r.EnvironmentBinding.ToolchainFingerprintVersion
	}
	if frozenTypedCommandAuthority(r.Command) {
		candidate.ProviderClass = core.ProviderProjectCommand
		candidate.ProviderClassKnown = true
		candidate.Authority = core.AuthorityMechanical
		candidate.AuthorityKnown = true
	}
	if err := candidate.Validate(); err != nil {
		return core.EvidenceCandidate{}, fmt.Errorf("invalid evidence candidate %q: %w", r.EvidenceID, err)
	}
	return candidate, nil
}

func frozenTypedCommandAuthority(command evidence.CommandAuthority) bool {
	return evidenceProjectCommandIDPattern.MatchString(command.ProjectCommandID) && validHexDigest(command.ProjectBindingDigest)
}

func validHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func mapCandidateFreshness(value evidence.Freshness) core.CandidateFreshness {
	switch value {
	case evidence.FreshnessCurrent:
		return core.CandidateCurrent
	case evidence.FreshnessStale:
		return core.CandidateStale
	default:
		return core.CandidateUnknown
	}
}

func mapCandidateResult(value evidence.Result) core.CandidateResult {
	switch value {
	case evidence.ResultPass:
		return core.CandidatePass
	case evidence.ResultFail:
		return core.CandidateFail
	case evidence.ResultIncomplete:
		return core.CandidateIncomplete
	default:
		return core.CandidateAmbiguous
	}
}

func finalizeCandidateResult(result verificationapp.CandidateResultSet) verificationapp.CandidateResultSet {
	sort.Slice(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].CompletedAt.Equal(result.Candidates[j].CompletedAt) {
			return result.Candidates[i].EvidenceID < result.Candidates[j].EvidenceID
		}
		return result.Candidates[i].CompletedAt.Before(result.Candidates[j].CompletedAt)
	})
	sort.Strings(result.Diagnostics)
	return result
}
