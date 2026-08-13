package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	SnapshotSchemaVersion      = 1
	ExactSnapshotSchemaVersion = 1
)

type ObservationQuality string

const (
	QualityFresh       ObservationQuality = "fresh"
	QualityCached      ObservationQuality = "cached"
	QualityStale       ObservationQuality = "stale"
	QualityUnavailable ObservationQuality = "unavailable"
)

type DirtySummary struct {
	Dirty      bool   `json:"dirty"`
	Modified   int    `json:"modified"`
	Added      int    `json:"added"`
	Deleted    int    `json:"deleted"`
	Renamed    int    `json:"renamed"`
	Untracked  int    `json:"untracked"`
	Conflicted int    `json:"conflicted"`
	Digest     string `json:"digest,omitempty"`
}

type TransientState struct {
	Merge      bool `json:"merge"`
	Rebase     bool `json:"rebase"`
	CherryPick bool `json:"cherry_pick"`
	Revert     bool `json:"revert"`
	Bisect     bool `json:"bisect"`
}

type FastSnapshot struct {
	SchemaVersion   int                `json:"schema_version"`
	RepositoryID    RepositoryID       `json:"repository_id,omitempty"`
	WorkspaceID     WorkspaceID        `json:"workspace_id,omitempty"`
	Generation      string             `json:"generation,omitempty"`
	Head            string             `json:"head,omitempty"`
	Ref             string             `json:"ref,omitempty"`
	Detached        bool               `json:"detached"`
	Upstream        string             `json:"upstream,omitempty"`
	Ahead           int                `json:"ahead"`
	Behind          int                `json:"behind"`
	UpstreamQuality ObservationQuality `json:"upstream_quality,omitempty"`
	Dirty           DirtySummary       `json:"dirty"`
	Transient       TransientState     `json:"transient"`
	Quality         ObservationQuality `json:"quality"`
	ObservedAt      time.Time          `json:"observed_at"`
	CacheAgeMS      int64              `json:"cache_age_ms"`
	DiagnosticCode  string             `json:"diagnostic_code,omitempty"`
}

type ExactSourceSnapshot struct {
	SchemaVersion       int                `json:"schema_version"`
	SourceContentDigest string             `json:"source_content_digest,omitempty"`
	VCSStateDigest      string             `json:"vcs_state_digest,omitempty"`
	SourceView          string             `json:"source_view,omitempty"`
	Quality             ObservationQuality `json:"quality"`
	ObservedAt          time.Time          `json:"observed_at"`
	DiagnosticCode      string             `json:"diagnostic_code,omitempty"`
}

func (s FastSnapshot) Validate() error {
	if s.SchemaVersion != SnapshotSchemaVersion || !validQuality(s.Quality) || s.ObservedAt.IsZero() || s.CacheAgeMS < 0 {
		return fmt.Errorf("invalid fast snapshot metadata")
	}
	if s.Quality == QualityUnavailable {
		return nil
	}
	if _, err := ParseRepositoryID(string(s.RepositoryID)); err != nil {
		return err
	}
	if _, err := ParseWorkspaceID(string(s.WorkspaceID)); err != nil {
		return err
	}
	if !validObjectID(s.Head) || !validDigest(s.Dirty.Digest) || !validGeneration(s.Generation) {
		return fmt.Errorf("invalid fast snapshot identity")
	}
	if s.Ahead < 0 || s.Behind < 0 || s.Dirty.Modified < 0 || s.Dirty.Added < 0 || s.Dirty.Deleted < 0 || s.Dirty.Renamed < 0 || s.Dirty.Untracked < 0 || s.Dirty.Conflicted < 0 {
		return fmt.Errorf("invalid fast snapshot counters")
	}
	if s.Detached && s.Ref != "" {
		return fmt.Errorf("detached snapshot has ref")
	}
	if s.Upstream == "" && s.UpstreamQuality != "" && s.UpstreamQuality != QualityUnavailable {
		return fmt.Errorf("upstream quality without upstream")
	}
	if s.UpstreamQuality != "" && !validQuality(s.UpstreamQuality) {
		return fmt.Errorf("invalid upstream quality")
	}
	return nil
}

func WithGeneration(snapshot FastSnapshot) (FastSnapshot, error) {
	if snapshot.Quality == QualityUnavailable {
		return snapshot, nil
	}
	facts := struct {
		RepositoryID RepositoryID   `json:"repository_id"`
		WorkspaceID  WorkspaceID    `json:"workspace_id"`
		Head         string         `json:"head"`
		Ref          string         `json:"ref"`
		Detached     bool           `json:"detached"`
		Upstream     string         `json:"upstream"`
		Ahead        int            `json:"ahead"`
		Behind       int            `json:"behind"`
		Dirty        DirtySummary   `json:"dirty"`
		Transient    TransientState `json:"transient"`
	}{snapshot.RepositoryID, snapshot.WorkspaceID, snapshot.Head, snapshot.Ref, snapshot.Detached, snapshot.Upstream, snapshot.Ahead, snapshot.Behind, snapshot.Dirty, snapshot.Transient}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return FastSnapshot{}, err
	}
	sum := sha256.Sum256(encoded)
	snapshot.Generation = "gen_" + hex.EncodeToString(sum[:])
	if err := snapshot.Validate(); err != nil {
		return FastSnapshot{}, err
	}
	return snapshot, nil
}

func (s ExactSourceSnapshot) Validate() error {
	if s.SchemaVersion != ExactSnapshotSchemaVersion || !validQuality(s.Quality) || s.ObservedAt.IsZero() {
		return fmt.Errorf("invalid exact snapshot metadata")
	}
	if s.Quality == QualityUnavailable {
		return nil
	}
	if !validDigest(s.SourceContentDigest) || !validDigest(s.VCSStateDigest) {
		return fmt.Errorf("invalid exact snapshot digest")
	}
	if s.SourceView != "worktree" && s.SourceView != "commit_tree" {
		return fmt.Errorf("invalid exact source view")
	}
	return nil
}

func validQuality(q ObservationQuality) bool {
	switch q {
	case QualityFresh, QualityCached, QualityStale, QualityUnavailable:
		return true
	default:
		return false
	}
}

func validObjectID(v string) bool {
	return len(v) >= 40 && len(v) <= 64 && validHex(v)
}
func validDigest(v string) bool { return len(v) == 64 && validHex(v) }
func validGeneration(v string) bool {
	return strings.HasPrefix(v, "gen_") && validDigest(strings.TrimPrefix(v, "gen_"))
}
func validHex(v string) bool {
	_, err := hex.DecodeString(v)
	return err == nil
}
