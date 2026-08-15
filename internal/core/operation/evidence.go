package operation

import "github.com/maemreyo/shellbeam/internal/core/project"

// EvidenceEligible reports whether a persisted reservation contains enough frozen
// verification metadata for terminal evidence derivation. It is intentionally
// structural: callers must validate the reservation separately.
func (r Reservation) EvidenceEligible() bool {
	if r.Evidence != nil {
		return true
	}
	if r.Intent != nil && evidenceIntentKind(r.Intent.Kind) {
		return true
	}
	if r.ProjectCommand == nil || r.ProjectCommand.SchemaVersion != project.BindingSchemaVersion {
		return false
	}
	if len(r.ProjectCommand.ExpectedOutputs) > 0 {
		return true
	}
	return evidenceProjectKind(r.ProjectCommand.Kind)
}

func evidenceIntentKind(kind IntentKind) bool {
	switch kind {
	case IntentKindFormat, IntentKindTest, IntentKindBuild, IntentKindGenerate, IntentKindRelease:
		return true
	default:
		return false
	}
}

func evidenceProjectKind(kind string) bool {
	switch kind {
	case "format", "test", "build", "generate", "release":
		return true
	default:
		return false
	}
}
