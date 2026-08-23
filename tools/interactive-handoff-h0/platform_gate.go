package main

import (
	"fmt"
	"sort"
)

func derivePlatformH1Qualifications(reports []BoundReport) []PlatformH1Qualification {
	out := make([]PlatformH1Qualification, 0, len(reports))
	for _, bound := range reports {
		out = append(out, derivePlatformH1Qualification(bound.Report))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GOOS < out[j].GOOS })
	return out
}

func derivePlatformH1Qualification(report Report) PlatformH1Qualification {
	qualification := PlatformH1Qualification{
		GOOS:                report.GOOS,
		InputFenceMechanism: "unqualified",
		ObservationTopology: "unqualified",
	}
	if validateReport(report) != nil || report.Verdict != StatusPass {
		return qualification
	}
	fence := ""
	for _, result := range report.Results {
		if result.ID == "P3" {
			fence = result.Facts["input_fence_mechanism"]
			break
		}
	}
	if fence == "" || fence == "unqualified" {
		return qualification
	}
	topology := qualifiedObservationTopology(report)
	if topology == "" || topology == "unqualified" {
		return qualification
	}
	for _, gateID := range genuineGateIDs {
		passed := false
		for _, result := range report.Results {
			if result.ID == gateID {
				passed = result.Status == StatusPass
				break
			}
		}
		if !passed {
			return qualification
		}
	}
	qualification.Allowed = true
	qualification.InputFenceMechanism = fence
	qualification.ObservationTopology = topology
	return qualification
}

func platformH1Qualification(gate QualificationGate, goos string) (PlatformH1Qualification, bool) {
	for _, qualification := range gate.PlatformH1 {
		if qualification.GOOS == goos {
			return qualification, true
		}
	}
	return PlatformH1Qualification{}, false
}

func equalPlatformH1(a, b []PlatformH1Qualification) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func verifyH1ForPlatform(gate QualificationGate, reports []BoundReport, platform string) error {
	if err := verifyGate(gate, reports); err != nil {
		return err
	}
	qualification, ok := platformH1Qualification(gate, platform)
	if !ok {
		return fmt.Errorf("platform %q is not bound by H0 gate", platform)
	}
	if !qualification.Allowed {
		return fmt.Errorf("H1 is not allowed on platform %q", platform)
	}
	return nil
}
