package mcp

import (
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func verificationSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	switch action {
	case "inspect.verification":
		if out.Verification == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "verification inspection missing", false)
		}
		body["verification"] = out.Verification
		return "inspect.verification: " + string(out.Verification.PolicyState), nil
	case "verification.policy.preview":
		if out.VerificationPolicyPreview == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "verification policy preview missing", false)
		}
		body["verification_policy_preview"] = out.VerificationPolicyPreview
		return "verification.policy.preview: " + string(out.VerificationPolicyPreview.State), nil
	case "verification.policy.activate":
		if out.VerificationActivation == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "verification activation result missing", false)
		}
		body["verification_activation"] = out.VerificationActivation
		return "verification.policy.activate", nil
	case "verification.waiver.set":
		if out.VerificationWaiver == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "verification waiver result missing", false)
		}
		body["verification_waiver"] = out.VerificationWaiver
		return "verification.waiver.set", nil
	case "verification.waiver.revoke":
		if out.VerificationRevocation == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "verification revocation result missing", false)
		}
		body["verification_revocation"] = out.VerificationRevocation
		return "verification.waiver.revoke", nil
	default:
		return "", toolErrorV2(action, "invalid_daemon_response", "verification response action invalid", false)
	}
}

func validVerificationPolicyDigest(v string) bool {
	return len(v) == 68 && v[:4] == "pol_" && validVerificationHex(v[4:])
}
func validVerificationGeneration(v string) bool {
	return len(v) == 68 && v[:4] == "gen_" && validVerificationHex(v[4:])
}
func validVerificationHex(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func boundedVerificationText(v string, max int) bool { return v != "" && len(v) <= max }
