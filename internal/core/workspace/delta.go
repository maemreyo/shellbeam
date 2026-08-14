package workspace

import (
	"fmt"
	"path"
	"strings"
	"time"
)

const DeltaSampleSchemaVersion = 1

const (
	DefaultDeltaMaxPaths       = 256
	MaxDeltaPaths              = 4096
	DefaultDeltaMaxOutputBytes = 256 << 10
	MaxDeltaOutputBytes        = 1 << 20
	DefaultDeltaTimeoutMS      = int64(150)
	MaxDeltaTimeoutMS          = int64(5000)
)

type DeltaLimits struct {
	MaxPaths        int   `json:"max_paths,omitempty"`
	MaxOutputBytes  int   `json:"max_output_bytes,omitempty"`
	TimeoutMS       int64 `json:"timeout_ms,omitempty"`
	RequireComplete bool  `json:"require_complete,omitempty"`
}

func (l DeltaLimits) Normalize() DeltaLimits {
	if l.MaxPaths == 0 {
		l.MaxPaths = DefaultDeltaMaxPaths
	}
	if l.MaxOutputBytes == 0 {
		l.MaxOutputBytes = DefaultDeltaMaxOutputBytes
	}
	if l.TimeoutMS == 0 {
		l.TimeoutMS = DefaultDeltaTimeoutMS
	}
	return l
}

func (l DeltaLimits) Validate() error {
	if l.MaxPaths < 1 || l.MaxPaths > MaxDeltaPaths {
		return fmt.Errorf("invalid delta max paths")
	}
	if l.MaxOutputBytes < 1 || l.MaxOutputBytes > MaxDeltaOutputBytes {
		return fmt.Errorf("invalid delta max output bytes")
	}
	if l.TimeoutMS < 1 || l.TimeoutMS > MaxDeltaTimeoutMS {
		return fmt.Errorf("invalid delta timeout")
	}
	return nil
}

type SelectionBasis string

const (
	SelectionWorkspaceDirty SelectionBasis = "workspace_dirty"
	SelectionActivityDelta  SelectionBasis = "activity_delta"
)

func (v SelectionBasis) Validate() error {
	switch v {
	case SelectionWorkspaceDirty, SelectionActivityDelta:
		return nil
	default:
		return fmt.Errorf("invalid selection basis %q", v)
	}
}

type SampleFreshness string

const SampleFreshlySampled SampleFreshness = "freshly_sampled"

func (v SampleFreshness) Validate() error {
	if v != SampleFreshlySampled {
		return fmt.Errorf("invalid sample freshness %q", v)
	}
	return nil
}

type SelectionCompleteness string

const (
	SelectionComplete         SelectionCompleteness = "complete"
	SelectionPartial          SelectionCompleteness = "partial"
	SelectionDiverged         SelectionCompleteness = "diverged"
	SelectionPotentiallyStale SelectionCompleteness = "potentially_stale"
	SelectionUnavailable      SelectionCompleteness = "unavailable"
)

func (v SelectionCompleteness) Validate() error {
	switch v {
	case SelectionComplete, SelectionPartial, SelectionDiverged, SelectionPotentiallyStale, SelectionUnavailable:
		return nil
	default:
		return fmt.Errorf("invalid selection completeness %q", v)
	}
}

type ModeState string

const (
	ModeUnknown ModeState = "unknown"
	ModeAbsent  ModeState = "absent"
	ModePresent ModeState = "present"
)

func (v ModeState) Validate() error {
	switch v {
	case "", ModeUnknown, ModeAbsent, ModePresent:
		return nil
	default:
		return fmt.Errorf("invalid repository mode state %q", v)
	}
}

type SelectionModeQuality struct {
	SparseCheckout  ModeState `json:"sparse_checkout,omitempty"`
	AssumeUnchanged ModeState `json:"assume_unchanged,omitempty"`
}

func (q SelectionModeQuality) Validate() error {
	if err := q.SparseCheckout.Validate(); err != nil {
		return err
	}
	return q.AssumeUnchanged.Validate()
}

func (q SelectionModeQuality) PotentiallyIncomplete() bool {
	return q.SparseCheckout == ModePresent || q.AssumeUnchanged == ModePresent
}

type PathTransition string

const (
	PathNone     PathTransition = "none"
	PathAdded    PathTransition = "added"
	PathModified PathTransition = "modified"
	PathDeleted  PathTransition = "deleted"
	PathReplaced PathTransition = "replaced"
	PathUnmerged PathTransition = "unmerged"
)

func (v PathTransition) Validate() error {
	switch v {
	case PathNone, PathAdded, PathModified, PathDeleted, PathReplaced, PathUnmerged:
		return nil
	default:
		return fmt.Errorf("invalid path transition %q", v)
	}
}

type SourceTransition string

const (
	SourceUnchanged           SourceTransition = "unchanged"
	SourceBytesChanged        SourceTransition = "bytes_changed"
	SourceAvailabilityChanged SourceTransition = "availability_changed"
	SourceIdentityChanged     SourceTransition = "identity_changed"
)

func (v SourceTransition) Validate() error {
	switch v {
	case SourceUnchanged, SourceBytesChanged, SourceAvailabilityChanged, SourceIdentityChanged:
		return nil
	default:
		return fmt.Errorf("invalid source transition %q", v)
	}
}

type VCSTransition string

const (
	VCSNone   VCSTransition = "none"
	VCSIndex  VCSTransition = "index"
	VCSHead   VCSTransition = "head"
	VCSRef    VCSTransition = "ref"
	VCSStaged VCSTransition = "staged"
	VCSOther  VCSTransition = "other"
)

func (v VCSTransition) Validate() error {
	switch v {
	case VCSNone, VCSIndex, VCSHead, VCSRef, VCSStaged, VCSOther:
		return nil
	default:
		return fmt.Errorf("invalid vcs transition %q", v)
	}
}

type ChangeRecord struct {
	PathTransition   PathTransition   `json:"path_transition"`
	OldPath          string           `json:"old_path,omitempty"`
	NewPath          string           `json:"new_path,omitempty"`
	SourceTransition SourceTransition `json:"source_transition"`
	VCSTransition    VCSTransition    `json:"vcs_transition"`
	Untracked        bool             `json:"untracked,omitempty"`
	Submodule        bool             `json:"submodule,omitempty"`
	TypeChanged      bool             `json:"type_changed,omitempty"`
}

func (r ChangeRecord) Validate() error {
	if err := r.PathTransition.Validate(); err != nil {
		return err
	}
	if err := r.SourceTransition.Validate(); err != nil {
		return err
	}
	if err := r.VCSTransition.Validate(); err != nil {
		return err
	}
	if r.OldPath != "" && !validChangePath(r.OldPath) {
		return fmt.Errorf("invalid old change path")
	}
	if r.NewPath != "" && !validChangePath(r.NewPath) {
		return fmt.Errorf("invalid new change path")
	}
	if r.OldPath != "" && r.NewPath != "" && r.OldPath == r.NewPath {
		return fmt.Errorf("old and new change paths are identical")
	}
	switch r.PathTransition {
	case PathNone:
		if r.OldPath != "" || r.NewPath != "" {
			return fmt.Errorf("pathless transition carries path")
		}
	case PathAdded:
		if r.OldPath != "" || r.NewPath == "" {
			return fmt.Errorf("added transition requires only new path")
		}
	case PathDeleted:
		if r.OldPath == "" || r.NewPath != "" {
			return fmt.Errorf("deleted transition requires only old path")
		}
	case PathModified, PathUnmerged:
		if r.NewPath == "" {
			return fmt.Errorf("path transition requires new path")
		}
	case PathReplaced:
		if r.OldPath == "" || r.NewPath == "" {
			return fmt.Errorf("replaced transition requires old and new paths")
		}
	}
	return nil
}

type DeltaSample struct {
	SchemaVersion            int                   `json:"schema_version"`
	RepositoryID             RepositoryID          `json:"repository_id"`
	WorkspaceID              WorkspaceID           `json:"workspace_id"`
	Freshness                SampleFreshness       `json:"freshness"`
	Completeness             SelectionCompleteness `json:"selection_completeness"`
	ObservedAt               time.Time             `json:"observed_at"`
	Head                     string                `json:"head,omitempty"`
	Ref                      string                `json:"ref,omitempty"`
	Detached                 bool                  `json:"detached,omitempty"`
	Unborn                   bool                  `json:"unborn,omitempty"`
	Changes                  []ChangeRecord        `json:"changes,omitempty"`
	ResolvedPaths            []string              `json:"resolved_paths,omitempty"`
	SelectionModes           SelectionModeQuality  `json:"selection_modes,omitempty"`
	DiagnosticCode           string                `json:"diagnostic_code,omitempty"`
	BarrierBefore            CoherenceBarrier      `json:"barrier_before"`
	BarrierAfter             CoherenceBarrier      `json:"barrier_after"`
	CacheEligible            bool                  `json:"cache_eligible"`
	SourceViewMayHaveChanged bool                  `json:"source_view_may_have_changed,omitempty"`
	RecordsObserved          int                   `json:"records_observed"`
	BytesObserved            int64                 `json:"bytes_observed"`
}

func (s DeltaSample) Validate() error {
	if s.SchemaVersion != DeltaSampleSchemaVersion || s.ObservedAt.IsZero() {
		return fmt.Errorf("invalid delta sample metadata")
	}
	if _, err := ParseRepositoryID(string(s.RepositoryID)); err != nil {
		return err
	}
	if _, err := ParseWorkspaceID(string(s.WorkspaceID)); err != nil {
		return err
	}
	if err := s.Freshness.Validate(); err != nil {
		return err
	}
	if err := s.Completeness.Validate(); err != nil {
		return err
	}
	if err := s.SelectionModes.Validate(); err != nil {
		return err
	}
	if s.Completeness == SelectionComplete && s.SelectionModes.PotentiallyIncomplete() {
		return fmt.Errorf("complete selection conflicts with repository mode quality")
	}
	if err := s.BarrierBefore.Validate(); err != nil {
		return err
	}
	if err := s.BarrierAfter.Validate(); err != nil {
		return err
	}
	if s.RecordsObserved < len(s.Changes) || s.BytesObserved < 0 {
		return fmt.Errorf("invalid delta sample accounting")
	}
	if s.Completeness == SelectionUnavailable && strings.TrimSpace(s.DiagnosticCode) == "" {
		return fmt.Errorf("unavailable delta sample missing diagnostic")
	}
	if s.CacheEligible && (s.BarrierBefore.DaemonIncarnation != s.BarrierAfter.DaemonIncarnation || s.BarrierBefore.Epoch != s.BarrierAfter.Epoch || s.BarrierBefore.ActiveManagedShellOperations != 0 || s.BarrierAfter.ActiveManagedShellOperations != 0) {
		return fmt.Errorf("cache-eligible delta sample has managed overlap")
	}
	for _, change := range s.Changes {
		if err := change.Validate(); err != nil {
			return err
		}
	}
	seenResolved := make(map[string]struct{}, len(s.ResolvedPaths))
	for _, resolved := range s.ResolvedPaths {
		if !validChangePath(resolved) {
			return fmt.Errorf("invalid resolved path")
		}
		if _, exists := seenResolved[resolved]; exists {
			return fmt.Errorf("duplicate resolved path")
		}
		seenResolved[resolved] = struct{}{}
	}
	return nil
}

func validChangePath(value string) bool {
	if value == "" || strings.ContainsRune(value, 0) || path.IsAbs(value) {
		return false
	}
	clean := path.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && clean == value
}
