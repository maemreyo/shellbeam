package browserbridge

import (
	"context"

	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type stubDaemonReader struct{}

var _ DaemonReader = stubDaemonReader{}

func (stubDaemonReader) Activity(context.Context, string) (*activitycore.Activity, bool, error) {
	return nil, false, nil
}

func (stubDaemonReader) Sessions(context.Context, string, int) (*persistent.InspectPage, bool, error) {
	return nil, false, nil
}

func (stubDaemonReader) Events(context.Context, observationcore.Target, string, int) (*observationapp.InspectResult, bool, error) {
	return nil, false, nil
}

func (stubDaemonReader) Verification(context.Context, string, string) (*verificationapp.Inspection, bool, error) {
	return nil, false, nil
}

func (stubDaemonReader) Structured(context.Context, string, structuredcore.TestStatus, int) (*structuredapp.InspectResult, bool, error) {
	return nil, false, nil
}
