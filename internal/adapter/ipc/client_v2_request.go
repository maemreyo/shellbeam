//go:build linux || darwin

package ipc

import bridge "github.com/maemreyo/shellbeam/internal/app/bridge"

func requestV2FromBridge(in bridge.Request) RequestV2 {
	req := RequestV2{IPVersion: 2, Kind: "request", RequestID: "bridge", Action: in.Action}
	if in.Action == "capabilities.negotiate" || in.Action == "read_media" {
		applyBridgeMediaV2(&req, bridgeMediaRequest{ConsumerMedia: in.ConsumerMedia, MediaContractFingerprint: in.MediaContractFingerprint, Media: in.Media})
		return req
	}
	applyBridgeRequestV2(&req, in)
	return req
}

func applyBridgeRequestV2(req *RequestV2, in bridge.Request) {
	switch in.Action {
	case "start":
		applyStartV2(req, in)
	case "handoff.request", "handoff.wait", "handoff.abort", "inspect.handoff":
		applyBridgeHandoffV2(req, in)
	case "poll":
		req.SessionID = in.Poll.SessionID
		req.Cursor = in.Poll.Cursor
		req.YieldMS = in.Poll.YieldMS
		req.MaxOutputBytes = in.Poll.MaxOutputBytes
	case "read_output":
		req.SessionID = in.OutputRead.SessionID
		selector := in.OutputRead.Selector
		req.Selector = &selector
		req.Continuation = in.OutputRead.Continuation
	case "checkpoint_create", "checkpoint_restore", "checkpoint_inspect":
		applyCheckpointBridgeRequestV2(req, in)
	case "write":
		applyBridgeWriteV2(req, in)
	case "inspect.project", "inspect.workspace", "inspect.readiness":
		req.WorkspaceID = in.WorkspaceID
	case "inspect.activity":
		req.ActivityID = in.ActivityID
	case "inspect.sessions":
		req.SessionName = in.SessionInspect.SessionName
		req.ActivityID = in.SessionInspect.ActivityID
		req.WorkspaceID = in.SessionInspect.WorkspaceID
		req.State = in.SessionInspect.State
		if in.SessionInspect.PersistentOnly != nil {
			value := *in.SessionInspect.PersistentOnly
			req.PersistentOnly = &value
		}
		req.MaxRecords = in.SessionInspect.Limit
		req.Continuation = in.SessionInspect.Cursor
	case "mutation_scope.set", "mutation_scope.release", "inspect.mutation_scopes":
		applyMutationScopeV2(req, in)
	case "inspect.events":
		target := in.EventInspect.Target
		req.Target = &target
		req.AfterEventCursor = in.EventInspect.AfterEventCursor
		req.MaxEvents = in.EventInspect.MaxEvents
	case "inspect.code":
		req.WorkspaceID = in.WorkspaceID
		req.ActivityID = in.ActivityID
		if in.CodeQuery != nil {
			query := *in.CodeQuery
			req.CodeQuery = &query
		}
	case "inspect.structured":
		req.OperationID = in.StructuredInspect.OperationID
		req.RecordKind = in.StructuredInspect.Filter.RecordKind
		req.Severity = in.StructuredInspect.Filter.Severity
		req.Path = in.StructuredInspect.Filter.Path
		req.TestStatus = in.StructuredInspect.Filter.TestStatus
		req.Continuation = in.StructuredInspect.Continuation
		req.MaxRecords = in.StructuredInspect.MaxRecords
	case "inspect.evidence":
		applyEvidenceInspectV2(req, in)
	case "inspect.environment", "inspect.process":
		applyObservationInspectV2(req, in)
	case "inspect.telemetry":
		req.OperationID = in.TelemetryInspect.OperationID
		req.MaxSamples = in.TelemetryInspect.MaxSamples
	case "repro.create":
		req.ReproCreateID = in.ReproCreate.CreateID
		req.OperationID = in.ReproCreate.OperationID
		policy := in.ReproCreate.Policy
		req.CapturePolicy = &policy
	case "inspect.repro":
		req.ReproID = in.ReproID
	case "kill":
		applyBridgeKillV2(req, in)
	}

}

func applyBridgeHandoffV2(req *RequestV2, in bridge.Request) {
	switch in.Action {
	case "handoff.request":
		req.HandoffID = in.HandoffRequest.HandoffID
		req.SessionID = in.HandoffRequest.SessionID
		req.HandoffReason = in.HandoffRequest.Reason
		req.HandoffPrivacy = in.HandoffRequest.Privacy
		completion := in.HandoffRequest.Completion
		req.HandoffCompletion = &completion
	case "handoff.wait":
		req.HandoffID = in.HandoffWait.HandoffID
		req.YieldMS = in.HandoffWait.Yield.Milliseconds()
	case "handoff.abort", "inspect.handoff":
		req.HandoffID = in.HandoffID
	}
}

func applyBridgeWriteV2(req *RequestV2, in bridge.Request) {
	req.SessionID = in.Write.SessionID
	req.AuthorityEpoch = in.Write.AuthorityEpoch
	req.InputOffset = in.Write.InputOffset
	req.Chars = in.Write.Chars
	req.EOF = in.Write.EOF
}

func applyBridgeKillV2(req *RequestV2, in bridge.Request) {
	req.SessionID = in.Kill.SessionID
	req.AuthorityEpoch = in.Kill.AuthorityEpoch
	req.KillID = in.Kill.KillID
	req.Signal = in.Kill.Signal
}

func applyCheckpointBridgeRequestV2(req *RequestV2, in bridge.Request) {
	switch in.Action {
	case "checkpoint_create":
		req.CheckpointCreateID = in.CheckpointCreate.CreateID
		req.WorkspaceID = in.CheckpointCreate.WorkspaceID
		req.ActivityID = in.CheckpointCreate.ActivityID
		req.Paths = append([]string(nil), in.CheckpointCreate.Paths...)
	case "checkpoint_restore":
		req.RestoreID = in.CheckpointRestore.RestoreID
		req.CheckpointID = in.CheckpointRestore.CheckpointID
		req.Paths = append([]string(nil), in.CheckpointRestore.Paths...)
	case "checkpoint_inspect":
		req.CheckpointID = in.CheckpointID
	}
}
