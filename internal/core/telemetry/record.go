// Package telemetry defines bounded empirical execution telemetry contracts.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const SchemaVersion = 1

type MetricQuality string

type Lifecycle string

type Completeness string

type ScopeClass string

const (
	MetricExact            MetricQuality = "exact"
	MetricPlatformReported MetricQuality = "platform_reported"
	MetricSampled          MetricQuality = "sampled"
	MetricUnavailable      MetricQuality = "unavailable"

	LifecycleTerminal Lifecycle = "terminal"

	CompletenessComplete    Completeness = "complete"
	CompletenessPartial     Completeness = "partial"
	CompletenessUnavailable Completeness = "unavailable"

	ScopeShell          ScopeClass = "shell"
	ScopeArgv           ScopeClass = "argv"
	ScopeProjectCommand ScopeClass = "project_command"
)

type Producer struct {
	ProducerID        string `json:"producer_id"`
	ProducerVersion   int    `json:"producer_version"`
	CapabilityVersion int    `json:"capability_version"`
}

type IntMetric struct {
	Quality MetricQuality `json:"quality"`
	Value   *int64        `json:"value,omitempty"`
}

type ResourceMetrics struct {
	CPUUserMS        IntMetric `json:"cpu_user_ms"`
	CPUSystemMS      IntMetric `json:"cpu_system_ms"`
	MaxRSSBytes      IntMetric `json:"max_rss_bytes"`
	ReadBytes        IntMetric `json:"read_bytes"`
	WriteBytes       IntMetric `json:"write_bytes"`
	ProcessCountPeak IntMetric `json:"process_count_peak"`
}

type PerformanceRecord struct {
	SchemaVersion               int             `json:"schema_version"`
	DerivationKey               string          `json:"derivation_key"`
	DerivationSchemaVersion     int             `json:"derivation_schema_version"`
	DerivationConfigDigest      string          `json:"derivation_config_digest"`
	Producer                    Producer        `json:"producer"`
	OperationID                 string          `json:"operation_id"`
	SessionID                   string          `json:"session_id"`
	ReceiptDigest               string          `json:"receipt_digest"`
	RepositoryID                string          `json:"repository_id,omitempty"`
	WorkspaceID                 string          `json:"workspace_id,omitempty"`
	ActivityID                  string          `json:"activity_id,omitempty"`
	ProjectCommandID            string          `json:"project_command_id,omitempty"`
	CommandSemanticsFingerprint string          `json:"command_semantics_fingerprint"`
	CommandDefinitionDigest     string          `json:"command_definition_digest,omitempty"`
	ParameterBindingFingerprint string          `json:"parameter_binding_fingerprint,omitempty"`
	SourceContentDigest         string          `json:"source_content_digest,omitempty"`
	SourceScopeDigest           string          `json:"source_scope_digest,omitempty"`
	EnvironmentFingerprint      string          `json:"environment_fingerprint,omitempty"`
	EnvironmentSchemaVersion    int             `json:"environment_schema_version,omitempty"`
	ToolchainFingerprint        string          `json:"toolchain_fingerprint,omitempty"`
	ToolchainSchemaVersion      int             `json:"toolchain_schema_version,omitempty"`
	ScopeClass                  ScopeClass      `json:"scope_class"`
	Platform                    string          `json:"platform"`
	Architecture                string          `json:"architecture"`
	WallMS                      int64           `json:"wall_ms"`
	OutputBytes                 int64           `json:"output_bytes"`
	InputAcceptedBytes          int64           `json:"input_accepted_bytes"`
	InputDeliveredBytes         int64           `json:"input_delivered_bytes"`
	TerminalOutcome             session.Outcome `json:"terminal_outcome"`
	TimedOut                    bool            `json:"timed_out"`
	CapturedAt                  time.Time       `json:"captured_at"`
	Lifecycle                   Lifecycle       `json:"lifecycle"`
	Completeness                Completeness    `json:"completeness"`
	Resources                   ResourceMetrics `json:"resources"`
}

func DerivationKey(receiptDigest string, producer Producer, schemaVersion int, configDigest string) (string, error) {
	if !validDigest(receiptDigest) || !validDigest(configDigest) || schemaVersion < 1 {
		return "", fmt.Errorf("invalid telemetry derivation identity")
	}
	if err := producer.validate(); err != nil {
		return "", err
	}
	identity := struct {
		ReceiptDigest string   `json:"receipt_digest"`
		Producer      Producer `json:"producer"`
		SchemaVersion int      `json:"schema_version"`
		ConfigDigest  string   `json:"config_digest"`
	}{receiptDigest, producer, schemaVersion, configDigest}
	return digestJSON(identity)
}

func CompatibilityKey(record PerformanceRecord) (string, error) {
	if err := record.Validate(); err != nil {
		return "", err
	}
	scope := "non_repository"
	if record.RepositoryID != "" {
		scope = record.RepositoryID
	}
	identity := struct {
		SchemaVersion               int        `json:"schema_version"`
		RepositoryScope             string     `json:"repository_scope"`
		WorkspaceID                 string     `json:"workspace_id,omitempty"`
		CommandSemanticsFingerprint string     `json:"command_semantics_fingerprint"`
		CommandDefinitionDigest     string     `json:"command_definition_digest,omitempty"`
		ParameterBindingFingerprint string     `json:"parameter_binding_fingerprint,omitempty"`
		EnvironmentFingerprint      string     `json:"environment_fingerprint,omitempty"`
		EnvironmentSchemaVersion    int        `json:"environment_schema_version,omitempty"`
		ToolchainFingerprint        string     `json:"toolchain_fingerprint,omitempty"`
		ToolchainSchemaVersion      int        `json:"toolchain_schema_version,omitempty"`
		ScopeClass                  ScopeClass `json:"scope_class"`
		Platform                    string     `json:"platform"`
		Architecture                string     `json:"architecture"`
	}{
		record.SchemaVersion, scope, record.WorkspaceID, record.CommandSemanticsFingerprint,
		record.CommandDefinitionDigest, record.ParameterBindingFingerprint,
		record.EnvironmentFingerprint, record.EnvironmentSchemaVersion,
		record.ToolchainFingerprint, record.ToolchainSchemaVersion,
		record.ScopeClass, record.Platform, record.Architecture,
	}
	return digestJSON(identity)
}

func (record PerformanceRecord) Validate() error {
	if record.SchemaVersion != SchemaVersion || record.DerivationSchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported telemetry schema")
	}
	if _, err := operation.ParseID(record.OperationID); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(record.SessionID); err != nil {
		return err
	}
	if !validDigest(record.ReceiptDigest) || !validDigest(record.DerivationConfigDigest) || !validDigest(record.CommandSemanticsFingerprint) {
		return fmt.Errorf("invalid telemetry digest")
	}
	if err := record.Producer.validate(); err != nil {
		return err
	}
	expected, err := DerivationKey(record.ReceiptDigest, record.Producer, record.DerivationSchemaVersion, record.DerivationConfigDigest)
	if err != nil || record.DerivationKey != expected {
		return fmt.Errorf("invalid telemetry derivation key")
	}
	if record.RepositoryID == "" {
		if record.WorkspaceID != "" {
			return fmt.Errorf("workspace requires repository")
		}
	} else {
		if _, err := workspace.ParseRepositoryID(record.RepositoryID); err != nil {
			return err
		}
		if record.WorkspaceID != "" {
			if _, err := workspace.ParseWorkspaceID(record.WorkspaceID); err != nil {
				return err
			}
		}
	}
	if record.ActivityID != "" {
		if _, err := activity.ParseID(record.ActivityID); err != nil {
			return err
		}
	}
	if record.ProjectCommandID != "" && !safeOpaque(record.ProjectCommandID, 128) {
		return fmt.Errorf("invalid project command id")
	}
	for _, digest := range []string{record.CommandDefinitionDigest, record.ParameterBindingFingerprint, record.SourceContentDigest, record.SourceScopeDigest} {
		if digest != "" && !validDigest(digest) {
			return fmt.Errorf("invalid optional telemetry digest")
		}
	}
	if err := validateVersionedFingerprint(record.EnvironmentFingerprint, record.EnvironmentSchemaVersion); err != nil {
		return fmt.Errorf("environment fingerprint: %w", err)
	}
	if err := validateVersionedFingerprint(record.ToolchainFingerprint, record.ToolchainSchemaVersion); err != nil {
		return fmt.Errorf("toolchain fingerprint: %w", err)
	}
	if !record.ScopeClass.valid() || !safeOpaque(record.Platform, 64) || !safeOpaque(record.Architecture, 64) {
		return fmt.Errorf("invalid telemetry compatibility scope")
	}
	if record.WallMS < 0 || record.OutputBytes < 0 || record.InputAcceptedBytes < 0 || record.InputDeliveredBytes < 0 || record.InputDeliveredBytes > record.InputAcceptedBytes {
		return fmt.Errorf("invalid telemetry counters")
	}
	if !terminalOutcome(record.TerminalOutcome) || record.TimedOut != (record.TerminalOutcome == session.Timeout) {
		return fmt.Errorf("invalid telemetry terminal outcome")
	}
	if record.CapturedAt.IsZero() || record.Lifecycle != LifecycleTerminal || !record.Completeness.valid() {
		return fmt.Errorf("invalid telemetry lifecycle")
	}
	return record.Resources.validate()
}

func (producer Producer) validate() error {
	if !safeOpaque(producer.ProducerID, 128) || producer.ProducerVersion < 1 || producer.CapabilityVersion < 1 {
		return fmt.Errorf("invalid telemetry producer")
	}
	return nil
}

func (metrics ResourceMetrics) validate() error {
	for _, metric := range []IntMetric{metrics.CPUUserMS, metrics.CPUSystemMS, metrics.MaxRSSBytes, metrics.ReadBytes, metrics.WriteBytes, metrics.ProcessCountPeak} {
		if err := metric.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (metric IntMetric) validate() error {
	switch metric.Quality {
	case MetricUnavailable:
		if metric.Value != nil {
			return fmt.Errorf("unavailable resource metric has value")
		}
	case MetricExact, MetricPlatformReported, MetricSampled:
		if metric.Value == nil || *metric.Value < 0 {
			return fmt.Errorf("observed resource metric lacks non-negative value")
		}
	default:
		return fmt.Errorf("invalid resource metric quality")
	}
	return nil
}

func (value Completeness) valid() bool {
	switch value {
	case CompletenessComplete, CompletenessPartial, CompletenessUnavailable:
		return true
	default:
		return false
	}
}

func (value ScopeClass) valid() bool {
	switch value {
	case ScopeShell, ScopeArgv, ScopeProjectCommand:
		return true
	default:
		return false
	}
}

func terminalOutcome(outcome session.Outcome) bool {
	switch outcome {
	case session.Success, session.Failure, session.Timeout, session.KilledOutcome, session.Ambiguous:
		return true
	default:
		return false
	}
}

func validateVersionedFingerprint(value string, version int) error {
	if value == "" {
		if version != 0 {
			return fmt.Errorf("schema version without fingerprint")
		}
		return nil
	}
	if !validDigest(value) || version < 1 {
		return fmt.Errorf("invalid versioned fingerprint")
	}
	return nil
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeOpaque(value string, max int) bool {
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
