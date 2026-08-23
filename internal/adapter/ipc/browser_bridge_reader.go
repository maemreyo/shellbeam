package ipc

import (
	"context"

	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

// BrowserBridgeReader maps the Browser Bridge typed read port onto IPC v2.
//
// It lives in the ipc adapter itself so no application package imports adapter
// wire types and no adapter package imports a sibling adapter.
type BrowserBridgeReader struct {
	client *Client
}

func NewBrowserBridgeReader(socket string) *BrowserBridgeReader {
	return &BrowserBridgeReader{client: NewClient(socket)}
}

func (r *BrowserBridgeReader) Activity(ctx context.Context, activityID string) (*activitycore.Activity, bool, error) {
	resp, err := r.client.CallV2(ctx, RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "bb-activity",
		Action: "inspect.activity", ActivityID: activityID,
	})
	if err != nil {
		return nil, false, err
	}
	return resp.Activity, resp.OK && resp.Activity != nil, nil
}

func (r *BrowserBridgeReader) Sessions(ctx context.Context, activityID string, maxRecords int) (*persistent.InspectPage, bool, error) {
	persistentOnly := false
	resp, err := r.client.CallV2(ctx, RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "bb-sessions",
		Action: "inspect.sessions", ActivityID: activityID, PersistentOnly: &persistentOnly, MaxRecords: maxRecords,
	})
	if err != nil {
		return nil, false, err
	}
	return resp.Sessions, resp.OK && resp.Sessions != nil, nil
}

func (r *BrowserBridgeReader) Events(ctx context.Context, target observationcore.Target, afterCursor string, maxEvents int) (*observationapp.InspectResult, bool, error) {
	resp, err := r.client.CallV2(ctx, RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "bb-events",
		Action: "inspect.events", Target: &target, AfterEventCursor: afterCursor, MaxEvents: maxEvents,
	})
	if err != nil {
		return nil, false, err
	}
	return resp.Events, resp.OK && resp.Events != nil, nil
}

func (r *BrowserBridgeReader) Verification(ctx context.Context, workspaceID, activityID string) (*verificationapp.Inspection, bool, error) {
	resp, err := r.client.CallV2(ctx, RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "bb-verification",
		Action: "inspect.verification", WorkspaceID: workspaceID, ActivityID: activityID,
		VerificationRequestV2Fields: VerificationRequestV2Fields{Phase: verificationcore.PhaseCheckpoint},
	})
	if err != nil {
		return nil, false, err
	}
	return resp.Verification, resp.OK && resp.Verification != nil, nil
}

func (r *BrowserBridgeReader) Structured(ctx context.Context, operationID string, testStatus structuredcore.TestStatus, maxRecords int) (*structuredapp.InspectResult, bool, error) {
	resp, err := r.client.CallV2(ctx, RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "bb-structured",
		Action: "inspect.structured", OperationID: operationID,
		TestStatus: testStatus, MaxRecords: maxRecords,
	})
	if err != nil {
		return nil, false, err
	}
	return resp.Structured, resp.OK && resp.Structured != nil, nil
}
