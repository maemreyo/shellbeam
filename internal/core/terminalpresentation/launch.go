package terminalpresentation

import "fmt"

type LaunchState string

const (
	LaunchNotAttempted            LaunchState = "not_attempted"
	LaunchLaunching               LaunchState = "launching"
	LaunchLaunchedAndClientProven LaunchState = "launched_and_client_proven"
	LaunchFailed                  LaunchState = "launch_failed"
	LaunchOutcomeUnknownState     LaunchState = "launch_outcome_unknown"
)

func (v LaunchState) Validate() error {
	switch v {
	case LaunchNotAttempted, LaunchLaunching, LaunchLaunchedAndClientProven, LaunchFailed, LaunchOutcomeUnknownState:
		return nil
	default:
		return fmt.Errorf("invalid terminal launch state")
	}
}

type LaunchOutcome string

const (
	LaunchOutcomeClientProven LaunchOutcome = "client_proven"
	LaunchOutcomeFailed       LaunchOutcome = "failed"
	LaunchOutcomeUnknown      LaunchOutcome = "unknown"
)

func (v LaunchOutcome) Validate() error {
	switch v {
	case LaunchOutcomeClientProven, LaunchOutcomeFailed, LaunchOutcomeUnknown:
		return nil
	default:
		return fmt.Errorf("invalid terminal launch outcome")
	}
}

func BeginLaunch(current LaunchState) (LaunchState, error) {
	if err := current.Validate(); err != nil {
		return "", err
	}
	if current != LaunchNotAttempted {
		return "", fmt.Errorf("terminal launch already attempted")
	}
	return LaunchLaunching, nil
}

func CompleteLaunch(current LaunchState, outcome LaunchOutcome) (LaunchState, error) {
	if err := current.Validate(); err != nil {
		return "", err
	}
	if err := outcome.Validate(); err != nil {
		return "", err
	}
	if current != LaunchLaunching {
		return "", fmt.Errorf("terminal launch is not reserved")
	}
	switch outcome {
	case LaunchOutcomeClientProven:
		return LaunchLaunchedAndClientProven, nil
	case LaunchOutcomeFailed:
		return LaunchFailed, nil
	case LaunchOutcomeUnknown:
		return LaunchOutcomeUnknownState, nil
	default:
		return "", fmt.Errorf("invalid terminal launch outcome")
	}
}
