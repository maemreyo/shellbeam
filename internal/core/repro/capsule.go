// Package repro defines immutable reproduction-provenance capsule contracts.
package repro

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	SchemaVersion           = 1
	MaxReferenceDescriptors = 32
	MaxResolvedArgv         = 32
	MaxArgumentBytes        = 1024
)

type DependentDerivationPolicy string

type CaptureState string

type AvailabilityState string

type ResolutionState string

const (
	CaptureCurrent DependentDerivationPolicy = "current"

	CaptureExact         CaptureState = "exact"
	CaptureComplete      CaptureState = "complete"
	CapturePartial       CaptureState = "partial"
	CaptureUnknown       CaptureState = "unknown"
	CaptureUnavailable   CaptureState = "unavailable"
	CaptureNotApplicable CaptureState = "not_applicable"

	AvailabilityPending     AvailabilityState = "pending"
	AvailabilityTerminal    AvailabilityState = "terminal"
	AvailabilityAbsent      AvailabilityState = "absent"
	AvailabilityUnavailable AvailabilityState = "unavailable"

	ResolutionAvailable   ResolutionState = "available"
	ResolutionCompacted   ResolutionState = "compacted"
	ResolutionPurged      ResolutionState = "purged"
	ResolutionUnavailable ResolutionState = "unavailable"
)

var reproIDPattern = regexp.MustCompile(`^repro_[0-9A-HJKMNP-TV-Z]{26}$`)

type CapturePolicy struct {
	DependentDerivations DependentDerivationPolicy `json:"dependent_derivations"`
}

type CreateRequest struct {
	CreateID    string        `json:"repro_create_id"`
	OperationID string        `json:"operation_id"`
	Policy      CapturePolicy `json:"capture_policy"`
}

type ExecutionDescriptor struct {
	OperationID                 string       `json:"operation_id"`
	SessionID                   string       `json:"session_id"`
	ReceiptDigest               string       `json:"receipt_digest"`
	CommandSemanticsFingerprint string       `json:"command_semantics_fingerprint"`
	ProjectCommandID            string       `json:"project_command_id,omitempty"`
	ParameterBindingFingerprint string       `json:"parameter_binding_fingerprint,omitempty"`
	ExecutionMode               string       `json:"execution_mode"`
	Executable                  string       `json:"executable,omitempty"`
	ResolvedArgv                []string     `json:"resolved_argv,omitempty"`
	ShellFingerprint            string       `json:"shell_fingerprint,omitempty"`
	CommandDetails              CaptureState `json:"command_details"`
}

type SourceDescriptor struct {
	RepositoryID        string       `json:"repository_id,omitempty"`
	WorkspaceID         string       `json:"workspace_id,omitempty"`
	SourceContentDigest string       `json:"source_content_digest,omitempty"`
	VCSStateDigest      string       `json:"vcs_state_digest,omitempty"`
	WorkspaceGeneration string       `json:"workspace_generation,omitempty"`
	Quality             CaptureState `json:"quality"`
}

type ProjectDescriptor struct {
	ManifestDigest string       `json:"manifest_digest,omitempty"`
	PolicyDigest   string       `json:"policy_digest,omitempty"`
	Quality        CaptureState `json:"quality,omitempty"`
}

type EnvironmentDescriptor struct {
	EnvironmentFingerprint   string       `json:"environment_fingerprint,omitempty"`
	EnvironmentSchemaVersion int          `json:"environment_schema_version,omitempty"`
	EnvironmentQuality       CaptureState `json:"environment_quality"`
	ToolchainFingerprint     string       `json:"toolchain_fingerprint,omitempty"`
	ToolchainSchemaVersion   int          `json:"toolchain_schema_version,omitempty"`
	ToolchainQuality         CaptureState `json:"toolchain_quality"`
}

type InputDescriptor struct {
	AcceptedBytes   int64        `json:"accepted_bytes"`
	DeliveredBytes  int64        `json:"delivered_bytes"`
	Complete        bool         `json:"complete"`
	ContentIdentity CaptureState `json:"content_identity"`
}

type ReferenceDescriptor struct {
	RefID                string            `json:"ref_id"`
	RecordKind           string            `json:"record_kind"`
	ProducerID           string            `json:"producer_id"`
	ProducerVersion      int               `json:"producer_version"`
	SchemaVersion        int               `json:"schema_version"`
	Digest               string            `json:"digest,omitempty"`
	Summary              string            `json:"summary,omitempty"`
	OriginalAvailability AvailabilityState `json:"original_availability"`
}

type CaptureMatrix struct {
	Source              CaptureState `json:"source"`
	Command             CaptureState `json:"command"`
	Toolchain           CaptureState `json:"toolchain"`
	Environment         CaptureState `json:"environment"`
	FilesystemExternal  CaptureState `json:"filesystem_external"`
	NetworkDependencies CaptureState `json:"network_dependencies"`
	ExternalServices    CaptureState `json:"external_services"`
	TimeRandomness      CaptureState `json:"time_randomness"`
	Input               CaptureState `json:"input"`
	Results             CaptureState `json:"results"`
}

type Capsule struct {
	SchemaVersion    int                   `json:"schema_version"`
	CreateID         string                `json:"repro_create_id"`
	ReproID          string                `json:"repro_id"`
	CreatedAt        time.Time             `json:"created_at"`
	CaptureCutDigest string                `json:"capture_cut_digest"`
	Execution        ExecutionDescriptor   `json:"execution"`
	Source           SourceDescriptor      `json:"source"`
	Project          ProjectDescriptor     `json:"project"`
	Environment      EnvironmentDescriptor `json:"environment"`
	Input            InputDescriptor       `json:"input"`
	Results          []ReferenceDescriptor `json:"results,omitempty"`
	Capture          CaptureMatrix         `json:"capture"`
}

func (request CreateRequest) Fingerprint() (string, error) {
	if err := request.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (request CreateRequest) Validate() error {
	if _, err := operation.ParseID(request.CreateID); err != nil {
		return fmt.Errorf("invalid repro create id")
	}
	if _, err := operation.ParseID(request.OperationID); err != nil {
		return err
	}
	if request.Policy.DependentDerivations != CaptureCurrent {
		return fmt.Errorf("unsupported repro capture policy")
	}
	return nil
}

func (capsule Capsule) Validate() error {
	if capsule.SchemaVersion != SchemaVersion || capsule.CreatedAt.IsZero() || !validDigest(capsule.CaptureCutDigest) {
		return fmt.Errorf("invalid repro capsule metadata")
	}
	if _, err := operation.ParseID(capsule.CreateID); err != nil {
		return fmt.Errorf("invalid repro create id")
	}
	if !reproIDPattern.MatchString(capsule.ReproID) {
		return fmt.Errorf("invalid repro id")
	}
	if err := capsule.Execution.validate(); err != nil {
		return err
	}
	if err := capsule.Source.validate(); err != nil {
		return err
	}
	if err := capsule.Project.validate(); err != nil {
		return err
	}
	if err := capsule.Environment.validate(); err != nil {
		return err
	}
	if err := capsule.Input.validate(); err != nil {
		return err
	}
	if len(capsule.Results) > MaxReferenceDescriptors {
		return fmt.Errorf("too many repro references")
	}
	for _, ref := range capsule.Results {
		if err := ref.validate(); err != nil {
			return err
		}
	}
	if err := capsule.Capture.validate(); err != nil {
		return err
	}
	if capsule.Capture.Command != capsule.Execution.CommandDetails {
		return fmt.Errorf("command capture mismatch")
	}
	if capsule.Capture.Environment == CaptureExact && capsule.Environment.EnvironmentFingerprint == "" {
		return fmt.Errorf("exact environment capture lacks fingerprint")
	}
	if capsule.Capture.Toolchain == CaptureExact && capsule.Environment.ToolchainFingerprint == "" {
		return fmt.Errorf("exact toolchain capture lacks fingerprint")
	}
	return nil
}

func (descriptor ExecutionDescriptor) validate() error {
	if _, err := operation.ParseID(descriptor.OperationID); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(descriptor.SessionID); err != nil {
		return err
	}
	if !validDigest(descriptor.ReceiptDigest) || !validDigest(descriptor.CommandSemanticsFingerprint) {
		return fmt.Errorf("invalid repro execution digest")
	}
	for _, digest := range []string{descriptor.ParameterBindingFingerprint, descriptor.ShellFingerprint} {
		if digest != "" && !validDigest(digest) {
			return fmt.Errorf("invalid repro command digest")
		}
	}
	if descriptor.ProjectCommandID != "" && !safeText(descriptor.ProjectCommandID, 128) {
		return fmt.Errorf("invalid project command id")
	}
	if descriptor.ExecutionMode != "argv" && descriptor.ExecutionMode != "shell" {
		return fmt.Errorf("invalid execution mode")
	}
	if descriptor.Executable != "" && !safeText(descriptor.Executable, 512) {
		return fmt.Errorf("invalid executable identity")
	}
	if len(descriptor.ResolvedArgv) > MaxResolvedArgv || !descriptor.CommandDetails.Valid() {
		return fmt.Errorf("invalid repro command details")
	}
	for _, argument := range descriptor.ResolvedArgv {
		if !safeText(argument, MaxArgumentBytes) {
			return fmt.Errorf("invalid resolved argv")
		}
	}
	if descriptor.CommandDetails == CaptureExact && descriptor.ExecutionMode == "argv" && len(descriptor.ResolvedArgv) == 0 {
		return fmt.Errorf("exact argv capture lacks argv")
	}
	return nil
}

func (descriptor SourceDescriptor) validate() error {
	if descriptor.RepositoryID == "" {
		if descriptor.WorkspaceID != "" {
			return fmt.Errorf("workspace requires repository")
		}
	} else {
		if _, err := workspace.ParseRepositoryID(descriptor.RepositoryID); err != nil {
			return err
		}
		if descriptor.WorkspaceID != "" {
			if _, err := workspace.ParseWorkspaceID(descriptor.WorkspaceID); err != nil {
				return err
			}
		}
	}
	for _, digest := range []string{descriptor.SourceContentDigest, descriptor.VCSStateDigest} {
		if digest != "" && !validDigest(digest) {
			return fmt.Errorf("invalid source digest")
		}
	}
	if descriptor.WorkspaceGeneration != "" && (!strings.HasPrefix(descriptor.WorkspaceGeneration, "gen_") || !validDigest(strings.TrimPrefix(descriptor.WorkspaceGeneration, "gen_"))) {
		return fmt.Errorf("invalid workspace generation")
	}
	if !descriptor.Quality.Valid() {
		return fmt.Errorf("invalid source capture quality")
	}
	return nil
}

func (descriptor ProjectDescriptor) validate() error {
	for _, digest := range []string{descriptor.ManifestDigest, descriptor.PolicyDigest} {
		if digest != "" && !validDigest(digest) {
			return fmt.Errorf("invalid project digest")
		}
	}
	if descriptor.Quality != "" && !descriptor.Quality.Valid() {
		return fmt.Errorf("invalid project capture quality")
	}
	return nil
}

func (descriptor EnvironmentDescriptor) validate() error {
	if err := validateVersionedFingerprint(descriptor.EnvironmentFingerprint, descriptor.EnvironmentSchemaVersion); err != nil {
		return err
	}
	if err := validateVersionedFingerprint(descriptor.ToolchainFingerprint, descriptor.ToolchainSchemaVersion); err != nil {
		return err
	}
	if !descriptor.EnvironmentQuality.Valid() || !descriptor.ToolchainQuality.Valid() {
		return fmt.Errorf("invalid environment capture quality")
	}
	if descriptor.EnvironmentQuality == CaptureExact && descriptor.EnvironmentFingerprint == "" {
		return fmt.Errorf("exact environment quality lacks fingerprint")
	}
	if descriptor.ToolchainQuality == CaptureExact && descriptor.ToolchainFingerprint == "" {
		return fmt.Errorf("exact toolchain quality lacks fingerprint")
	}
	return nil
}

func (descriptor InputDescriptor) validate() error {
	if descriptor.AcceptedBytes < 0 || descriptor.DeliveredBytes < 0 || descriptor.DeliveredBytes > descriptor.AcceptedBytes {
		return fmt.Errorf("invalid repro input counters")
	}
	if descriptor.Complete != (descriptor.DeliveredBytes == descriptor.AcceptedBytes) {
		return fmt.Errorf("invalid repro input completeness")
	}
	if descriptor.ContentIdentity != CaptureUnavailable {
		return fmt.Errorf("stdin content identity must remain unavailable")
	}
	return nil
}

func (descriptor ReferenceDescriptor) validate() error {
	if !safeText(descriptor.RefID, 128) || !safeText(descriptor.RecordKind, 64) || !safeText(descriptor.ProducerID, 128) || descriptor.ProducerVersion < 1 || descriptor.SchemaVersion < 1 {
		return fmt.Errorf("invalid repro reference identity")
	}
	if descriptor.Digest != "" && !validDigest(descriptor.Digest) {
		return fmt.Errorf("invalid repro reference digest")
	}
	if descriptor.Summary != "" && !safeText(descriptor.Summary, 512) {
		return fmt.Errorf("invalid repro reference summary")
	}
	if !descriptor.OriginalAvailability.Valid() {
		return fmt.Errorf("invalid repro reference availability")
	}
	return nil
}

func (matrix CaptureMatrix) validate() error {
	for _, state := range []CaptureState{matrix.Source, matrix.Command, matrix.Toolchain, matrix.Environment, matrix.FilesystemExternal, matrix.NetworkDependencies, matrix.ExternalServices, matrix.TimeRandomness, matrix.Input, matrix.Results} {
		if !state.Valid() {
			return fmt.Errorf("invalid repro capture state")
		}
	}
	return nil
}

func (state CaptureState) Valid() bool {
	switch state {
	case CaptureExact, CaptureComplete, CapturePartial, CaptureUnknown, CaptureUnavailable, CaptureNotApplicable:
		return true
	default:
		return false
	}
}

func (state AvailabilityState) Valid() bool {
	switch state {
	case AvailabilityPending, AvailabilityTerminal, AvailabilityAbsent, AvailabilityUnavailable:
		return true
	default:
		return false
	}
}

func (state ResolutionState) Valid() bool {
	switch state {
	case ResolutionAvailable, ResolutionCompacted, ResolutionPurged, ResolutionUnavailable:
		return true
	default:
		return false
	}
}

func validateVersionedFingerprint(value string, version int) error {
	if value == "" {
		if version != 0 {
			return fmt.Errorf("fingerprint schema without fingerprint")
		}
		return nil
	}
	if !validDigest(value) || version < 1 {
		return fmt.Errorf("invalid versioned fingerprint")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeText(value string, max int) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
