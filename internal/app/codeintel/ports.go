package codeintel

import (
	"context"
	"errors"

	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type SourceRefState string

const (
	SourceRefCurrent     SourceRefState = "current"
	SourceRefExpired     SourceRefState = "source_ref_expired"
	SourceRefUnavailable SourceRefState = "source_ref_unavailable"

	CodeQueryBudgetExceeded  = "code_intelligence_query_budget_exceeded"
	CodeSourceChanged        = "code_intelligence_source_changed_during_query"
	CodePositionInvalid      = "code_intelligence_position_invalid"
	CodeSourceRefUnavailable = "source_ref_unavailable"
	CodeUnsupportedEncoding  = "unsupported_source_encoding"
	CodeProviderUnavailable  = "code_intelligence_provider_unavailable"
	CodeProviderFailed       = "code_intelligence_provider_failed"
	CodeProviderBusy         = "code_intelligence_provider_busy"
	CodeProviderCooldown     = "code_intelligence_provider_cooldown"
)

type BoundSource struct {
	Ref   core.SourceRef `json:"ref"`
	Bytes []byte         `json:"-"`
}

type SourceBinder interface {
	Bind(context.Context, workspace.Workspace, string) (BoundSource, error)
	Resolve(core.SourceRefID) (BoundSource, SourceRefState)
}

type SourceRetention interface {
	Retain(core.SourceRef, []byte) (BoundSource, error)
	Resolve(core.SourceRefID) (BoundSource, SourceRefState)
}

type WorkspaceLookup interface {
	Inspect(context.Context, string) (workspace.Workspace, error)
}

type WorkspaceSampler interface {
	Sample(context.Context, workspace.WorkspaceID, workspace.DeltaLimits) workspace.DeltaSample
}

type ActivitySelector interface {
	CompareWorkspace(context.Context, string, workspace.DeltaSample) (activitycore.Comparison, error)
}

type CoherenceSource interface {
	CaptureBarrier() workspace.CoherenceBarrier
}

type ProviderPool interface {
	Query(context.Context, ProviderRequest) (ProviderResponse, error)
}

type ProviderStartOptions struct {
	ProviderID         string
	ExecutableIdentity string
	ConfigFingerprint  string
	BuildFingerprint   string
}

type ProviderOptionsResolver interface {
	Resolve(context.Context, workspace.Workspace, core.Query) (ProviderStartOptions, error)
}

type Provider interface {
	Metadata() core.ProviderMetadata
	Query(context.Context, ProviderRequest) (ProviderResponse, error)
	Close() error
}

type ProviderFactory interface {
	Start(context.Context, workspace.Workspace, ProviderStartOptions) (Provider, error)
}

type ProviderRequest struct {
	Workspace       workspace.Workspace
	Sample          workspace.DeltaSample
	SelectedSources []BoundSource
	Query           core.Query
}

type ProviderResponse struct {
	Status      core.ResultStatus
	Metadata    core.ProviderMetadata
	Diagnostics []ProviderDiagnostic
	Symbols     []ProviderSymbol
	Locations   []ProviderLocation
	TypeSummary string
}

type ProviderDiagnostic struct {
	Severity         core.Severity
	Code             string
	Message          string
	Location         core.SourceLocation
	ProviderSource   string
	RelatedLocations []core.SourceLocation
	Authority        core.Authority
	Completeness     core.RecordCompleteness
}

type ProviderSymbol struct {
	Name         string
	Kind         string
	Location     core.SourceLocation
	Authority    core.Authority
	Completeness core.RecordCompleteness
}

type ProviderLocation struct {
	Name         string
	Relationship string
	Location     core.SourceLocation
	Authority    core.Authority
	Completeness core.RecordCompleteness
}

type Error struct {
	Code      string
	Retryable bool
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

func Retryable(err error) bool {
	var target *Error
	return errors.As(err, &target) && target.Retryable
}

func newError(code string, retryable bool, cause error) error {
	return &Error{Code: code, Retryable: retryable, Cause: cause}
}
