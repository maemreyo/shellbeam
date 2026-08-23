package verification

import "fmt"

type CostQuality string

const (
	CostQualityExact            CostQuality = "exact"
	CostQualityPlatformReported CostQuality = "platform_reported"
	CostQualitySampled          CostQuality = "sampled"
	CostQualityUnavailable      CostQuality = "unavailable"
)

type CostMetric struct {
	Quality CostQuality `json:"quality"`
	Latest  *int64      `json:"latest,omitempty"`
	P50     *int64      `json:"p50,omitempty"`
	P95     *int64      `json:"p95,omitempty"`
	Samples int         `json:"samples,omitempty"`
}

type VerificationCost struct {
	ProjectCommandID string     `json:"project_command_id,omitempty"`
	WallMS           CostMetric `json:"wall_ms"`
	OutputBytes      CostMetric `json:"output_bytes"`
	CPUUserMS        CostMetric `json:"cpu_user_ms"`
	CPUSystemMS      CostMetric `json:"cpu_system_ms"`
	MaxRSSBytes      CostMetric `json:"max_rss_bytes"`
	ProcessPeak      CostMetric `json:"process_count_peak"`
	ProviderCost     CostMetric `json:"provider_cost,omitempty"`
	ModelCost        CostMetric `json:"model_cost,omitempty"`
}

type BoundRequirementCost struct {
	ObligationID     string                     `json:"obligation_id"`
	RequirementID    string                     `json:"requirement_id"`
	ProviderClass    ProviderClass              `json:"provider_class"`
	ProjectCommandID string                     `json:"project_command_id,omitempty"`
	Execution        ProviderExecutionSemantics `json:"execution"`
	Cost             VerificationCost           `json:"cost"`
}

func (q CostQuality) Validate() error {
	switch q {
	case CostQualityExact, CostQualityPlatformReported, CostQualitySampled, CostQualityUnavailable:
		return nil
	default:
		return fmt.Errorf("invalid cost quality")
	}
}
func (m CostMetric) Validate() error {
	if m.Quality.Validate() != nil || m.Samples < 0 {
		return fmt.Errorf("invalid cost metric")
	}
	values := []*int64{m.Latest, m.P50, m.P95}
	for _, v := range values {
		if v != nil && *v < 0 {
			return fmt.Errorf("invalid cost metric value")
		}
	}
	if m.Quality == CostQualityUnavailable {
		if m.Latest != nil || m.P50 != nil || m.P95 != nil || m.Samples != 0 {
			return fmt.Errorf("unavailable cost has value")
		}
		return nil
	}
	if m.Latest == nil && m.P50 == nil && m.P95 == nil {
		return fmt.Errorf("observed cost lacks value")
	}
	if m.Samples < 1 {
		return fmt.Errorf("observed cost lacks samples")
	}
	return nil
}
func (c VerificationCost) Validate() error {
	if c.ProjectCommandID != "" && !candidateProjectCommandIDPattern.MatchString(c.ProjectCommandID) {
		return fmt.Errorf("invalid cost project command")
	}
	for _, m := range []CostMetric{c.WallMS, c.OutputBytes, c.CPUUserMS, c.CPUSystemMS, c.MaxRSSBytes, c.ProcessPeak, c.ProviderCost, c.ModelCost} {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (c BoundRequirementCost) Validate() error {
	if !isDerivedID(c.ObligationID, "obl_") || !boundedToken(c.RequirementID, 128) || c.ProviderClass.Validate() != nil {
		return fmt.Errorf("invalid bound requirement cost")
	}
	if c.ProjectCommandID != "" && !candidateProjectCommandIDPattern.MatchString(c.ProjectCommandID) {
		return fmt.Errorf("invalid bound cost command")
	}
	if err := c.Execution.Validate(); err != nil {
		return err
	}
	if c.Cost.ProjectCommandID != c.ProjectCommandID {
		return fmt.Errorf("bound cost project command mismatch")
	}
	return c.Cost.Validate()
}
func UnavailableVerificationCost() VerificationCost {
	u := CostMetric{Quality: CostQualityUnavailable}
	return VerificationCost{WallMS: u, OutputBytes: u, CPUUserMS: u, CPUSystemMS: u, MaxRSSBytes: u, ProcessPeak: u, ProviderCost: u, ModelCost: u}
}
