package bridge

func IsVerificationAction(action string) bool {
	switch action {
	case "inspect.verification", "verification.policy.preview", "verification.policy.activate", "verification.waiver.set", "verification.waiver.revoke":
		return true
	default:
		return false
	}
}
