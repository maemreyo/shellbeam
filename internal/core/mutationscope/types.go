package mutationscope

import (
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	SchemaVersion               = 1
	MaxScopeIDBytes             = 128
	MaxMutationIDBytes          = 128
	MaxActiveScopesPerActivity  = 16
	MaxActiveScopesPerWorkspace = 64
	MaxPathsPerScope            = 16
	MaxSelectorBytes            = 256
	MaxAdvisories               = 32
	MaxOverlapExamples          = 4
	DefaultTTL                  = 15 * time.Minute
	MinTTL                      = time.Second
	MaxTTL                      = 30 * time.Minute
)

type Mode string

const (
	ModeRead   Mode = "read"
	ModeMutate Mode = "mutate"
)

type SetEffect string

const (
	SetEffectCreated  SetEffect = "created"
	SetEffectReplaced SetEffect = "replaced"
)

type MutationResult string

const (
	ResultSet           MutationResult = "set"
	ResultReleased      MutationResult = "released"
	ResultAlreadyAbsent MutationResult = "already_absent"
)

type ConflictKind string

const (
	ConflictReadMutate   ConflictKind = "read_mutate"
	ConflictMutateMutate ConflictKind = "mutate_mutate"
)

type Scope struct {
	SchemaVersion int                   `json:"schema_version"`
	ScopeID       string                `json:"scope_id"`
	ActivityID    string                `json:"activity_id"`
	WorkspaceID   workspace.WorkspaceID `json:"workspace_id"`
	Mode          Mode                  `json:"mode"`
	Paths         []string              `json:"paths"`
	DeclaredAt    time.Time             `json:"declared_at"`
	ExpiresAt     time.Time             `json:"expires_at"`
	RevisionID    string                `json:"revision_id"`
}

type ScopeIdentity struct {
	SchemaVersion int                   `json:"schema_version"`
	ScopeID       string                `json:"scope_id"`
	ActivityID    string                `json:"activity_id"`
	WorkspaceID   workspace.WorkspaceID `json:"workspace_id"`
	BoundAt       time.Time             `json:"bound_at"`
}

type MutationReceipt struct {
	SchemaVersion      int            `json:"schema_version"`
	MutationID         string         `json:"mutation_id"`
	RequestFingerprint string         `json:"request_fingerprint"`
	Result             MutationResult `json:"result"`
	SetEffect          SetEffect      `json:"set_effect,omitempty"`
	ScopeID            string         `json:"scope_id"`
	CommittedAt        time.Time      `json:"committed_at"`
	ExpiresAt          time.Time      `json:"expires_at,omitempty"`
}

type OverlapExample struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type Advisory struct {
	Code                     string                `json:"code"`
	WorkspaceID              workspace.WorkspaceID `json:"workspace_id"`
	ScopeIDs                 [2]string             `json:"scope_ids"`
	ActivityIDs              []string              `json:"activity_ids"`
	Modes                    [2]Mode               `json:"modes"`
	ConflictKind             ConflictKind          `json:"conflict_kind"`
	OverlapExamples          []OverlapExample      `json:"overlap_examples,omitempty"`
	OverlapExamplesTruncated bool                  `json:"overlap_examples_truncated,omitempty"`
	CauseFingerprint         string                `json:"cause_fingerprint"`
}

type InspectResult struct {
	ActiveScopes        []Scope    `json:"active_scopes"`
	Advisories          []Advisory `json:"advisories"`
	ActiveCount         int        `json:"active_count"`
	AdvisoryCount       int        `json:"advisory_count"`
	ActiveScopeLimit    int        `json:"active_scope_limit"`
	AdvisoryLimit       int        `json:"advisory_limit"`
	ScopesTruncated     bool       `json:"scopes_truncated,omitempty"`
	AdvisoriesTruncated bool       `json:"advisories_truncated,omitempty"`
}
