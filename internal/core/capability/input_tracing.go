package capability

import trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"

type InputTracingSupport struct {
	SchemaVersions        []int                       `json:"schema_versions"`
	Provider              trace.ProviderIdentity      `json:"provider"`
	Platform              string                      `json:"platform"`
	Maturity              string                      `json:"maturity"`
	PreExecCoverage       bool                        `json:"pre_exec_coverage"`
	InstrumentationEffect trace.InstrumentationEffect `json:"instrumentation_effect"`
	Coverage              trace.CoverageMatrix        `json:"coverage"`
	Authority             trace.Authority             `json:"authority"`
}

func (c Catalog) WithInputTracing(provider trace.ProviderIdentity, platform string, preExec bool, effect trace.InstrumentationEffect, matrix trace.CoverageMatrix) Catalog {
	out := c.Clone()
	if provider.Validate() != nil || platform == "" || len(platform) > 64 || !effect.Valid() || matrix.Validate(preExec) != nil {
		return out
	}
	out.Features[FeatureInputTracing] = Available
	out.InputTracing = &InputTracingSupport{
		SchemaVersions: []int{trace.SchemaVersion}, Provider: provider, Platform: platform, Maturity: "experimental",
		PreExecCoverage: preExec, InstrumentationEffect: effect, Coverage: matrix, Authority: trace.AuthorityAdvisory,
	}
	out.Limits.InputTraceRawEvents = trace.MaxRawEvents
	out.Limits.InputTraceUniqueResources = trace.MaxUniqueResources
	out.Limits.InputTracePublicResources = trace.MaxPublicResources
	out.Limits.InputTraceExternalResources = trace.MaxExternalResources
	out.Limits.InputTraceRawEventBytes = trace.MaxRawEventBytes
	out.Limits.InputTracePrivateRawBytes = trace.MaxPrivateRawBytes
	out.Limits.InputTracePublicRecordBytes = trace.MaxPublicRecordBytes
	out.Limits.InputTraceRetainedRecords = trace.MaxRetainedTraceRecords
	out.Limits.InputTraceCaptureDurationMS = trace.MaxTraceCaptureDuration.Milliseconds()
	out.Limits.InputTraceStartupBudgetMS = trace.TraceStartupBudget.Milliseconds()
	out.Limits.InputTraceWorkerQueueDepth = trace.WorkerQueueDepth
	return out
}
