package verification

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

const maxEvidenceCandidatePages = 4

var evidenceProjectCommandIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type evidenceInspector interface {
	Inspect(context.Context, evidenceapp.InspectRequest) (evidenceapp.InspectResult, error)
}

type structuredEvidenceReader interface {
	FindOperationDerivation(context.Context, string) (structuredcore.Derivation, bool, error)
	GetRecordSummary(context.Context, string) (structuredapp.RecordSummary, bool, error)
	ListRecords(context.Context, string, structuredapp.RecordQuery) ([]structuredcore.Record, error)
}

type EvidenceSource struct {
	inspector    evidenceInspector
	reservations verificationapp.EvidenceReservationReader
}

func NewEvidenceSource(inspector evidenceInspector, reservations verificationapp.EvidenceReservationReader) *EvidenceSource {
	return &EvidenceSource{inspector: inspector, reservations: reservations}
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
		capped, err := s.appendCandidateViews(ctx, &out, result.Records, commandSet, maxRecords)
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

func (s *EvidenceSource) appendCandidateViews(ctx context.Context, out *verificationapp.CandidateResultSet, views []evidenceapp.InspectRecord, commandSet map[string]bool, maxRecords int) (bool, error) {
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
		out.Diagnostics = append(out.Diagnostics, s.bindReservationAuthority(ctx, &candidate)...)
		out.Diagnostics = append(out.Diagnostics, s.bindStructuredDetail(ctx, &candidate)...)
		if err := candidate.Validate(); err != nil {
			return false, fmt.Errorf("invalid reservation-bound candidate %q: %w", candidate.EvidenceID, err)
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

func (s *EvidenceSource) bindReservationAuthority(ctx context.Context, candidate *core.EvidenceCandidate) []string {
	if candidate == nil {
		return nil
	}
	if s == nil || s.reservations == nil {
		return []string{"verification_attempt_authority_unavailable:" + candidate.OperationID}
	}
	id, err := operation.ParseID(candidate.OperationID)
	if err != nil {
		return []string{"verification_attempt_authority_unavailable:" + candidate.OperationID}
	}
	reservation, found, err := s.reservations.FindOperation(ctx, id)
	if err != nil || !found {
		return []string{"verification_attempt_authority_unavailable:" + candidate.OperationID}
	}
	contractDigest, ok, err := reservationContractDigest(reservation)
	if err != nil || !ok || contractDigest != candidate.ContractDigest || !reservationCommandMatchesCandidate(reservation, *candidate) {
		candidate.Attempt = nil
		candidate.SemanticContractDigest = ""
		return []string{"evidence_contract_authority_mismatch:" + candidate.OperationID}
	}
	candidate.SemanticContractDigest = contractDigest
	candidate.Attempt = cloneEvidenceAttempt(reservation.VerificationAttempt)
	return nil
}

func (s *EvidenceSource) bindStructuredDetail(ctx context.Context, candidate *core.EvidenceCandidate) []string {
	if s == nil || candidate == nil || s.reservations == nil {
		return nil
	}
	reader, ok := s.reservations.(structuredEvidenceReader)
	if !ok {
		return nil
	}
	derivation, found, err := reader.FindOperationDerivation(ctx, candidate.OperationID)
	if err != nil || !found || derivation.Lifecycle != structuredcore.LifecycleTerminal {
		return nil
	}
	detail := &core.StructuredEvidenceDetail{DerivationKey: derivation.DerivationKey, Completeness: derivation.Completeness}
	if derivation.SemanticsCoverage != nil {
		coverage := *derivation.SemanticsCoverage
		coverage.MechanicallyObservable = append([]string(nil), derivation.SemanticsCoverage.MechanicallyObservable...)
		coverage.Unavailable = append([]string(nil), derivation.SemanticsCoverage.Unavailable...)
		detail.SemanticsCoverage = &coverage
	}
	if derivation.Completeness == structuredcore.CompletenessCompacted {
		candidate.StructuredDetail = detail
		return nil
	}
	summary, summaryFound, err := reader.GetRecordSummary(ctx, derivation.DerivationKey)
	if err != nil || !summaryFound || summary.Compacted {
		candidate.StructuredDetail = detail
		return nil
	}
	if summary.RecordsTotal > structuredapp.MaxListRecords {
		return []string{"structured_evidence_detail_bounded:" + candidate.OperationID}
	}
	records, err := reader.ListRecords(ctx, derivation.DerivationKey, structuredapp.RecordQuery{Offset: 0, Limit: max(1, summary.RecordsTotal)})
	if err != nil {
		return nil
	}
	set := map[structuredcore.TestStatus]struct{}{}
	for _, record := range records {
		if record.Authority != structuredcore.AuthorityMechanical {
			continue
		}
		if record.TestCase != nil {
			set[record.TestCase.Status] = struct{}{}
		}
		if record.TestSuite != nil {
			set[record.TestSuite.Status] = struct{}{}
		}
	}
	for status := range set {
		detail.MechanicalTestStatuses = append(detail.MechanicalTestStatuses, status)
	}
	sort.Slice(detail.MechanicalTestStatuses, func(i, j int) bool { return detail.MechanicalTestStatuses[i] < detail.MechanicalTestStatuses[j] })
	if detail.Validate() != nil {
		return nil
	}
	candidate.StructuredDetail = detail
	return nil
}

func reservationContractDigest(reservation operation.Reservation) (string, bool, error) {
	if reservation.Evidence != nil {
		digest, err := reservation.Evidence.Digest()
		return digest, err == nil, err
	}
	if reservation.ProjectCommand == nil {
		return "", false, nil
	}
	binding := reservation.ProjectCommand
	if err := binding.Validate(); err != nil {
		return "", false, err
	}
	if binding.SchemaVersion == project.BindingSchemaV1 {
		return "", false, nil
	}
	kind, mapped := evidenceKindFromProjectBinding(binding.Kind)
	if !mapped && len(binding.ExpectedOutputs) == 0 {
		return "", false, nil
	}
	if !mapped {
		kind = evidence.VerificationArtifact
	}
	contract := evidence.Contract{VerificationKind: kind, SourceScope: evidence.SourceScope(binding.SourceScope), ExpectedOutputs: append([]project.Output(nil), binding.ExpectedOutputs...)}
	digest, err := contract.Digest()
	return digest, err == nil, err
}

func reservationCommandMatchesCandidate(reservation operation.Reservation, candidate core.EvidenceCandidate) bool {
	if reservation.ProjectCommand == nil {
		return candidate.ProjectCommandID == "" && candidate.ProjectBindingDigest == ""
	}
	digest, err := reservation.ProjectCommand.Digest()
	if err != nil {
		return false
	}
	return reservation.ProjectCommand.CommandID == candidate.ProjectCommandID && digest == candidate.ProjectBindingDigest && reservation.ProjectCommand.ManifestDigest == candidate.ManifestDigest
}

func evidenceKindFromProjectBinding(kind string) (evidence.VerificationKind, bool) {
	switch kind {
	case "format":
		return evidence.VerificationFormat, true
	case "test":
		return evidence.VerificationTest, true
	case "build":
		return evidence.VerificationBuild, true
	case "generate":
		return evidence.VerificationGenerate, true
	case "release":
		return evidence.VerificationRelease, true
	default:
		return "", false
	}
}

func cloneEvidenceAttempt(value *evidence.VerificationAttemptIntent) *evidence.VerificationAttemptIntent {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
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
