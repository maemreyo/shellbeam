// Package inputtrace defines provider-neutral E27 dynamic input tracing facts.
// Public types intentionally carry only bounded metadata, never captured payloads.
package inputtrace

import "time"

const (
	SchemaVersion = 1

	MaxRawEvents            = 32768
	MaxUniqueResources      = 2048
	MaxPublicResources      = 512
	MaxExternalResources    = 128
	MaxRawEventBytes        = 4096
	MaxPrivateRawBytes      = 8 << 20
	MaxPublicRecordBytes    = 512 << 10
	MaxRetainedTraceRecords = 128
	WorkerQueueDepth        = 32
)

const (
	MaxTraceCaptureDuration = time.Hour
	TraceStartupBudget      = 2 * time.Second
)

type Mode string

const (
	ModeOff        Mode = "off"
	ModeBestEffort Mode = "best_effort"
	ModeRequired   Mode = "required"
)

type ObservationClass string

const (
	ClassFilesystemReads           ObservationClass = "filesystem_reads"
	ClassFilesystemMetadataQueries ObservationClass = "filesystem_metadata_queries"
	ClassDirectoryEnumerations     ObservationClass = "directory_enumerations"
	ClassFilesystemWrites          ObservationClass = "filesystem_writes"
	ClassExecutedBinaries          ObservationClass = "executed_binaries"
	ClassLoadedLibraries           ObservationClass = "loaded_libraries"
	ClassEnvironmentNamesObserved  ObservationClass = "environment_names_observed"
	ClassNetworkAttempts           ObservationClass = "network_attempts"
	ClassChildProcesses            ObservationClass = "child_processes"
)

type Coverage string

const (
	CoverageUnsupported          Coverage = "unsupported"
	CoverageUnknown              Coverage = "unknown"
	CoveragePartial              Coverage = "partial"
	CoverageCompleteForOwnedTree Coverage = "complete_for_owned_tree"
)

type CoverageMatrix struct {
	FilesystemReads           Coverage `json:"filesystem_reads"`
	FilesystemMetadataQueries Coverage `json:"filesystem_metadata_queries"`
	DirectoryEnumerations     Coverage `json:"directory_enumerations"`
	FilesystemWrites          Coverage `json:"filesystem_writes"`
	ExecutedBinaries          Coverage `json:"executed_binaries"`
	LoadedLibraries           Coverage `json:"loaded_libraries"`
	EnvironmentNamesObserved  Coverage `json:"environment_names_observed"`
	NetworkAttempts           Coverage `json:"network_attempts"`
	ChildProcesses            Coverage `json:"child_processes"`
}

type ProviderIdentity struct {
	ID                string `json:"provider_id"`
	Version           int    `json:"provider_version"`
	CapabilityVersion int    `json:"capability_version"`
}

type InstrumentationEffect string

const (
	EffectNone                 InstrumentationEffect = "none"
	EffectNonInvasive          InstrumentationEffect = "non_invasive"
	EffectEnvironmentAffecting InstrumentationEffect = "environment_affecting"
)

type BindingStatus string

const (
	BindingActive      BindingStatus = "active"
	BindingUnavailable BindingStatus = "unavailable"
)

type InstrumentationBinding struct {
	SchemaVersion              int                   `json:"schema_version"`
	TraceID                    string                `json:"trace_id"`
	Mode                       Mode                  `json:"mode"`
	Status                     BindingStatus         `json:"status"`
	Provider                   ProviderIdentity      `json:"provider"`
	Platform                   string                `json:"platform"`
	InstrumentationFingerprint string                `json:"instrumentation_fingerprint"`
	InstrumentationEffect      InstrumentationEffect `json:"instrumentation_effect"`
	PreExecCoverageEstablished bool                  `json:"pre_exec_coverage_established"`
	Coverage                   CoverageMatrix        `json:"coverage"`
}

type PathClass string

const (
	PathRepoRelative              PathClass = "repo_relative"
	PathWorkspaceExternalRedacted PathClass = "workspace_external_redacted"
	PathSystemClassified          PathClass = "system_classified"
)

type Resource struct {
	ObservationClass ObservationClass `json:"observation_class"`
	PathClass        PathClass        `json:"path_class"`
	Identity         string           `json:"identity"`
}

type Authority string

const AuthorityAdvisory Authority = "advisory"

type ScopeKind string

const ScopeObservedInput ScopeKind = "observed_input_scope"

type Outcome string

const (
	OutcomeComplete    Outcome = "complete"
	OutcomePartial     Outcome = "partial"
	OutcomeUnavailable Outcome = "unavailable"
)

type Summary struct {
	ResourcesReturned int `json:"resources_returned"`
	ResourcesObserved int `json:"resources_observed"`
}

type Record struct {
	SchemaVersion                 int                   `json:"schema_version"`
	DerivationKey                 string                `json:"derivation_key"`
	TraceID                       string                `json:"trace_id"`
	OperationID                   string                `json:"operation_id"`
	SessionID                     string                `json:"session_id"`
	ReceiptDigest                 string                `json:"receipt_digest"`
	Mode                          Mode                  `json:"mode"`
	Provider                      ProviderIdentity      `json:"provider"`
	Platform                      string                `json:"platform"`
	InstrumentationFingerprint    string                `json:"instrumentation_fingerprint"`
	InstrumentationEffect         InstrumentationEffect `json:"instrumentation_effect"`
	Authority                     Authority             `json:"authority"`
	ScopeKind                     ScopeKind             `json:"scope_kind"`
	MayHaveUnobservedDependencies bool                  `json:"may_have_unobserved_dependencies"`
	CaptureStart                  time.Time             `json:"capture_start,omitempty"`
	CaptureEnd                    time.Time             `json:"capture_end,omitempty"`
	PreExecCoverageEstablished    bool                  `json:"pre_exec_coverage_established"`
	Coverage                      CoverageMatrix        `json:"coverage"`
	Outcome                       Outcome               `json:"outcome"`
	Truncated                     bool                  `json:"trace_truncated,omitempty"`
	Resources                     []Resource            `json:"resources,omitempty"`
	Summary                       Summary               `json:"summary"`
}

type Inspection struct {
	SchemaVersion int     `json:"schema_version"`
	Status        string  `json:"status"`
	OperationID   string  `json:"operation_id"`
	TraceID       string  `json:"trace_id,omitempty"`
	Record        *Record `json:"record,omitempty"`
}

type Request struct {
	Mode Mode `json:"trace_mode,omitempty"`
}
