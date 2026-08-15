package project

// ValidateExpectedOutputs canonicalizes per-command/caller expected-output declarations
// through the same validator used by project manifests. Required is already explicit
// because the public Output type cannot distinguish omitted false from explicit false.
func ValidateExpectedOutputs(outputs []Output) ([]Output, error) {
	if len(outputs) > MaxExpectedOutputs {
		return nil, projectError(CodeLimitExceeded, "too many expected outputs")
	}
	out := make([]Output, 0, len(outputs))
	for _, value := range outputs {
		required := value.Required
		converted, err := validateOutput(rawOutput{
			Path: value.Path, Kind: value.Kind, Digest: value.Digest, Role: value.Role, Required: &required,
		}, false)
		if err != nil {
			return nil, err
		}
		out = append(out, converted)
	}
	return out, nil
}
