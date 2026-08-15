package evidence

import (
	"context"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

type InspectStatus string

const (
	InspectAvailable      InspectStatus = "available"
	InspectNeverRun       InspectStatus = "never_run"
	InspectUnavailable    InspectStatus = "unavailable"
	defaultInspectRecords               = 20
)

type InspectRequest struct {
	Filter       InspectFilter `json:"filter"`
	MaxRecords   int           `json:"max_records,omitempty"`
	Continuation string        `json:"continuation,omitempty"`
}

type InspectRecord struct {
	Record           core.Record               `json:"record"`
	Validity         core.Validity             `json:"validity"`
	CurrentSource    core.CurrentSource        `json:"current_source"`
	LastRevalidation *core.ValidityObservation `json:"last_revalidation,omitempty"`
}

type InspectResult struct {
	SchemaVersion   int             `json:"schema_version"`
	Status          InspectStatus   `json:"status"`
	Records         []InspectRecord `json:"records,omitempty"`
	Continuation    string          `json:"continuation,omitempty"`
	IndexGeneration uint64          `json:"index_generation,omitempty"`
}

type Inspector struct {
	repository InspectionRepository
	current    CurrentStateProvider
	observer   ArtifactObserver
	codec      *CursorCodec
	now        func() time.Time
}

func NewInspector(repository InspectionRepository, current CurrentStateProvider, observer ArtifactObserver, codec *CursorCodec) *Inspector {
	return &Inspector{repository: repository, current: current, observer: observer, codec: codec, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Inspector) Inspect(ctx context.Context, request InspectRequest) (InspectResult, error) {
	maxRecords, err := normalizeInspectRequest(request)
	if err != nil {
		return InspectResult{}, err
	}
	if s == nil || s.repository == nil || s.codec == nil {
		return InspectResult{SchemaVersion: 1, Status: InspectUnavailable}, nil
	}
	if request.Filter.EvidenceID != "" || request.Filter.OperationID != "" {
		if request.Continuation != "" {
			return InspectResult{}, failure.New(failure.EvidenceCursorInvalid, map[string]string{"reason": "direct_selector"}, nil)
		}
		return s.inspectDirect(ctx, request.Filter)
	}
	return s.inspectIndex(ctx, request, maxRecords)
}

func NormalizeInspectRequestForTransport(request InspectRequest) (int, error) {
	return normalizeInspectRequest(request)
}

func normalizeInspectRequest(request InspectRequest) (int, error) {
	if err := request.Filter.Validate(); err != nil {
		return 0, failure.New(failure.InvalidInput, map[string]string{"field": "filter"}, err)
	}
	maxRecords := request.MaxRecords
	if maxRecords == 0 {
		maxRecords = defaultInspectRecords
	}
	if maxRecords < 1 || maxRecords > core.MaxInspectRecords {
		return 0, failure.New(failure.InvalidInput, map[string]string{"field": "max_records"}, nil)
	}
	direct := request.Filter.EvidenceID != "" || request.Filter.OperationID != ""
	if request.Filter.RevalidateArtifacts && !direct && maxRecords > core.MaxRevalidateRecords {
		return 0, failure.New(failure.InvalidInput, map[string]string{"field": "max_records", "reason": "artifact_revalidation_limit"}, nil)
	}
	return maxRecords, nil
}

func (s *Inspector) inspectDirect(ctx context.Context, filter InspectFilter) (InspectResult, error) {
	var record core.Record
	var found bool
	var err error
	status := InspectUnavailable
	if filter.EvidenceID != "" {
		record, found, err = s.repository.FindEvidenceByID(ctx, filter.EvidenceID)
	} else {
		id, parseErr := operation.ParseID(filter.OperationID)
		if parseErr != nil {
			return InspectResult{}, parseErr
		}
		record, found, err = s.repository.FindEvidenceByOperation(ctx, id)
		if !found {
			status = InspectNeverRun
		}
	}
	if err != nil {
		return InspectResult{}, err
	}
	if !found {
		return InspectResult{SchemaVersion: 1, Status: status}, nil
	}
	if !matchesFilter(record, filter) {
		return InspectResult{SchemaVersion: 1, Status: InspectAvailable}, nil
	}
	view, err := s.inspectRecord(ctx, record, filter.RevalidateArtifacts, map[string]CurrentState{})
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{SchemaVersion: 1, Status: InspectAvailable, Records: []InspectRecord{view}}, nil
}

func (s *Inspector) inspectIndex(ctx context.Context, request InspectRequest, maxRecords int) (InspectResult, error) {
	high, err := s.repository.ObservationHighWatermark(ctx)
	if err != nil {
		return InspectResult{}, err
	}
	cut := high
	var after observation.ChangeSeq
	if request.Continuation != "" {
		state, decodeErr := s.codec.Decode(request.Continuation, request.Filter)
		if decodeErr != nil {
			return InspectResult{}, decodeErr
		}
		cut, after = observation.ChangeSeq(state.IndexGeneration), observation.ChangeSeq(state.AfterSequence)
		if cut > high {
			return InspectResult{}, failure.New(failure.EvidenceCursorExpired, map[string]string{"reason": "index_generation"}, nil)
		}
	}
	result := InspectResult{SchemaVersion: 1, Status: InspectAvailable, IndexGeneration: uint64(cut)}
	if after >= cut {
		return result, nil
	}
	batch, err := s.repository.ListEvidenceIndexObligations(ctx, after, cut, core.MaxInspectScanRecords)
	if err != nil {
		return InspectResult{}, err
	}
	if len(batch) == 0 && after < cut {
		return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "evidence_index_gap"}, nil)
	}
	cache := map[string]CurrentState{}
	progressed := after
	for _, obligation := range batch {
		if obligation.ChangeSeq <= progressed || obligation.ChangeSeq > cut {
			return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "evidence_index_order"}, nil)
		}
		progressed = obligation.ChangeSeq
		if obligation.State != observation.ObligationCommitted || obligation.Kind != observation.EventEvidenceRecorded {
			continue
		}
		id, ok := evidenceIDFromSubject(obligation.SubjectRef)
		if !ok {
			continue
		}
		record, found, loadErr := s.repository.FindEvidenceByID(ctx, id)
		if loadErr != nil {
			return InspectResult{}, loadErr
		}
		if !found {
			return InspectResult{}, failure.New(failure.EventContinuityUnavailable, map[string]string{"reason": "evidence_index_subject"}, nil)
		}
		if !matchesFilter(record, request.Filter) {
			continue
		}
		view, viewErr := s.inspectRecord(ctx, record, request.Filter.RevalidateArtifacts, cache)
		if viewErr != nil {
			return InspectResult{}, viewErr
		}
		result.Records = append(result.Records, view)
		if len(result.Records) >= maxRecords {
			break
		}
	}
	if progressed < cut {
		token, encodeErr := s.codec.Encode(request.Filter, CursorState{IndexGeneration: uint64(cut), AfterSequence: uint64(progressed)})
		if encodeErr != nil {
			return InspectResult{}, encodeErr
		}
		result.Continuation = token
	}
	return result, nil
}

func (s *Inspector) inspectRecord(ctx context.Context, record core.Record, revalidate bool, cache map[string]CurrentState) (InspectRecord, error) {
	current := CurrentState{Source: core.CurrentSource{Quality: core.SourceQualityUnknown}}
	if record.WorkspaceID != "" && s.current != nil {
		if cached, ok := cache[record.WorkspaceID]; ok {
			current = cached
		} else {
			current = s.current.ObserveCurrent(ctx, record)
			cache[record.WorkspaceID] = current
		}
	}
	validity := core.DeriveSourceValidity(record.Source, current.Source)
	if len(record.Artifacts) == 0 {
		validity.ArtifactMatch = core.ArtifactMatchNotRequired
	} else {
		validity.ArtifactMatch = core.ArtifactMatchUnknown
	}
	validity.PolicyMatch = core.PolicyMatchUnknown
	view := InspectRecord{Record: record, Validity: validity, CurrentSource: current.Source}
	if last, found, err := s.repository.LoadEvidenceValidity(ctx, record.EvidenceID); err != nil {
		return InspectRecord{}, err
	} else if found {
		copy := last
		view.LastRevalidation = &copy
	}
	if !revalidate {
		return view, nil
	}
	artifacts := s.revalidateArtifacts(ctx, record, current)
	validity.ArtifactMatch = deriveArtifactMatch(record.Artifacts, artifacts)
	observed := core.ValidityObservation{SchemaVersion: core.ValiditySchemaVersion, EvidenceID: record.EvidenceID, Validity: validity, CurrentSource: current.Source, Artifacts: artifacts, ObservedAt: s.now()}
	if _, err := s.repository.PutEvidenceValidity(ctx, observed); err != nil {
		return InspectRecord{}, err
	}
	view.Validity = validity
	view.LastRevalidation = &observed
	return view, nil
}

func (s *Inspector) revalidateArtifacts(ctx context.Context, record core.Record, current CurrentState) []core.ArtifactObservation {
	if len(record.Artifacts) == 0 {
		return nil
	}
	outputs := outputsFromArtifacts(record.Artifacts)
	if current.WorkspaceRoot == "" || s.observer == nil {
		return unavailableArtifacts(outputs, s.now())
	}
	observed, err := s.observer.Observe(ctx, current.WorkspaceRoot, outputs)
	if err != nil {
		return unavailableArtifacts(outputs, s.now())
	}
	return observed
}

func outputsFromArtifacts(values []core.ArtifactObservation) []project.Output {
	out := make([]project.Output, 0, len(values))
	for _, v := range values {
		out = append(out, project.Output{Path: v.Path, Kind: v.DeclaredKind, Digest: v.DigestMode, Required: v.Required})
	}
	return out
}

func deriveArtifactMatch(before, current []core.ArtifactObservation) core.ArtifactMatch {
	if len(before) == 0 {
		return core.ArtifactMatchNotRequired
	}
	if len(before) != len(current) {
		return core.ArtifactMatchUnknown
	}
	unknown := false
	changed := false
	for i, now := range current {
		old := before[i]
		if old.Path != now.Path || old.DeclaredKind != now.DeclaredKind || old.Required != now.Required {
			return core.ArtifactMatchUnknown
		}
		if now.Required && now.Status == core.ArtifactMissing {
			return core.ArtifactMatchMissing
		}
		if now.Status == core.ArtifactUnavailable || old.Status == core.ArtifactUnavailable {
			unknown = true
			continue
		}
		if old.Status != now.Status || old.Exists != now.Exists || old.ObservedKind != now.ObservedKind {
			changed = true
			continue
		}
		if now.DigestMode != "" && now.DigestMode != "none" && old.Digest != now.Digest {
			changed = true
		}
		if now.DeclaredKind == "symlink" && old.LinkText != now.LinkText {
			changed = true
		}
	}
	if changed {
		return core.ArtifactMatchChanged
	}
	if unknown {
		return core.ArtifactMatchUnknown
	}
	return core.ArtifactMatchCurrent
}

func matchesFilter(record core.Record, filter InspectFilter) bool {
	if filter.EvidenceID != "" && record.EvidenceID != filter.EvidenceID {
		return false
	}
	if filter.OperationID != "" && record.OperationID != filter.OperationID {
		return false
	}
	if filter.WorkspaceID != "" && record.WorkspaceID != filter.WorkspaceID {
		return false
	}
	if filter.ProjectCommandID != "" && record.Command.ProjectCommandID != filter.ProjectCommandID {
		return false
	}
	if filter.ActivityID != "" && record.ActivityID != filter.ActivityID {
		return false
	}
	if filter.VerificationKind != "" && record.VerificationKind != filter.VerificationKind {
		return false
	}
	if filter.Result != "" && record.Result != filter.Result {
		return false
	}
	return true
}
func evidenceIDFromSubject(subject string) (string, bool) {
	const prefix = "evidence:"
	if !strings.HasPrefix(subject, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(subject, prefix)
	if strings.Contains(id, ":") || len(id) != 67 || !strings.HasPrefix(id, "ev_") {
		return "", false
	}
	return id, true
}
