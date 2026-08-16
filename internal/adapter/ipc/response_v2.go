package ipc

func clearResponseV2Payload(resp *ResponseV2) {
	resp.View, resp.Result, resp.Checkpoint, resp.Restore, resp.CheckpointInspection = nil, nil, nil, nil, nil
	resp.Server, resp.Project, resp.Readiness = nil, nil, nil
	resp.Workspace, resp.Activity, resp.Events, resp.Structured, resp.Evidence = nil, nil, nil, nil, nil
	resp.Environment, resp.Process, resp.Mutation, resp.MutationScopes = nil, nil, nil, nil
	resp.ActiveMutationScopes, resp.MutationScopeAdvisories = nil, nil
	resp.MutationScopesTruncated, resp.MutationScopeAdvisoriesTruncated = false, false
	resp.Telemetry, resp.Capsule, resp.Repro, resp.Code, resp.OutputView, resp.Sessions = nil, nil, nil, nil, nil, nil
}

func finalizeResponseV2(resp ResponseV2, err error) ResponseV2 {
	resp.OK = err == nil
	if err != nil {
		clearResponseV2Payload(&resp)
		resp.Error = errorEnvelope(err)
	}
	return resp
}
