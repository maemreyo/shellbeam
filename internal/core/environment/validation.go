package environment

import (
	"encoding/hex"
	"fmt"
	"regexp"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func (s Snapshot) Validate() error {
	if s.SchemaVersion != SnapshotSchemaVersion || !validPrefixedDigest(s.SnapshotID, "env_") || s.CapturedAt.IsZero() {
		return fmt.Errorf("invalid environment snapshot identity")
	}
	if !validQuality(s.Quality) {
		return fmt.Errorf("invalid environment snapshot quality")
	}
	if s.Platform.OS == "" || s.Platform.Architecture == "" {
		return fmt.Errorf("invalid platform")
	}
	if err := validateExecution(s.Execution); err != nil {
		return err
	}
	if err := validatePath(s.Path); err != nil {
		return err
	}
	if len(s.VariablePresence) > MaxRelevantVariables {
		return fmt.Errorf("too many relevant environment variables")
	}
	seenVariables := make(map[string]struct{}, len(s.VariablePresence))
	for _, presence := range s.VariablePresence {
		if !envNamePattern.MatchString(presence.Name) {
			return fmt.Errorf("invalid environment variable name")
		}
		if _, ok := seenVariables[presence.Name]; ok {
			return fmt.Errorf("duplicate environment variable name")
		}
		seenVariables[presence.Name] = struct{}{}
	}
	if s.ToolchainManager != nil && (s.ToolchainManager.Kind == "" || s.ToolchainManager.Identity == "") {
		return fmt.Errorf("invalid toolchain manager")
	}
	if len(s.Toolchains) > MaxToolchainProbes {
		return fmt.Errorf("too many toolchain observations")
	}
	seenToolchains := make(map[string]struct{}, len(s.Toolchains))
	for _, toolchain := range s.Toolchains {
		if err := validateToolchainObservation(toolchain); err != nil {
			return err
		}
		if _, ok := seenToolchains[toolchain.Kind]; ok {
			return fmt.Errorf("duplicate toolchain kind")
		}
		seenToolchains[toolchain.Kind] = struct{}{}
	}
	if s.Quality != QualityUnavailable {
		if s.FingerprintVersion != FingerprintVersion || !validDigest(s.EnvironmentFingerprint) {
			return fmt.Errorf("invalid environment fingerprint")
		}
	} else if s.EnvironmentFingerprint != "" {
		return fmt.Errorf("unavailable snapshot claims environment fingerprint")
	}
	if len(s.Toolchains) > 0 {
		if s.ToolchainFingerprintVersion != ToolchainFingerprintVersion || !validDigest(s.ToolchainFingerprint) {
			return fmt.Errorf("invalid toolchain fingerprint")
		}
	} else if s.ToolchainFingerprint != "" || s.ToolchainFingerprintVersion != 0 {
		return fmt.Errorf("empty toolchains claim fingerprint")
	}
	return nil
}

func (b Binding) Validate() error {
	if !validPrefixedDigest(b.SnapshotID, "env_") || b.CapturedAt.IsZero() ||
		b.EnvironmentFingerprintVersion != FingerprintVersion || !validDigest(b.EnvironmentFingerprint) {
		return fmt.Errorf("invalid environment binding")
	}
	if b.ToolchainFingerprint == "" {
		if b.ToolchainFingerprintVersion != 0 {
			return fmt.Errorf("invalid empty toolchain binding")
		}
		return nil
	}
	if b.ToolchainFingerprintVersion != ToolchainFingerprintVersion || !validDigest(b.ToolchainFingerprint) {
		return fmt.Errorf("invalid toolchain binding")
	}
	return nil
}

func validateFingerprintInput(input FingerprintInput) error {
	if input.Platform.OS == "" || input.Platform.Architecture == "" {
		return fmt.Errorf("invalid platform")
	}
	if err := validateExecution(input.Execution); err != nil {
		return err
	}
	if err := validatePath(input.Path); err != nil {
		return err
	}
	if len(input.VariablePresence) > MaxRelevantVariables {
		return fmt.Errorf("too many relevant environment variables")
	}
	seen := make(map[string]struct{}, len(input.VariablePresence))
	for _, presence := range input.VariablePresence {
		if !envNamePattern.MatchString(presence.Name) {
			return fmt.Errorf("invalid environment variable name")
		}
		if _, ok := seen[presence.Name]; ok {
			return fmt.Errorf("duplicate environment variable name")
		}
		seen[presence.Name] = struct{}{}
	}
	if input.ToolchainManager != nil && (input.ToolchainManager.Kind == "" || input.ToolchainManager.Identity == "") {
		return fmt.Errorf("invalid toolchain manager")
	}
	return nil
}

func validateExecution(execution ExecutionContext) error {
	switch execution.Mode {
	case "shell", "argv":
	default:
		return fmt.Errorf("invalid execution mode")
	}
	if execution.Identity == "" {
		return fmt.Errorf("missing execution identity")
	}
	return nil
}

func validatePath(path PathObservation) error {
	switch path.Quality {
	case QualityComplete, QualityPartial:
		if !validDigest(path.Digest) || path.EntryCount < 0 {
			return fmt.Errorf("invalid path observation")
		}
	case QualityUnavailable:
		if path.Digest != "" || path.EntryCount != 0 {
			return fmt.Errorf("unavailable path claims facts")
		}
	default:
		return fmt.Errorf("invalid path quality")
	}
	return nil
}

func validateToolchainObservation(observation ToolchainObservation) error {
	if !supportedToolchain(observation.Kind) || observation.RequestedIdentity == "" {
		return fmt.Errorf("invalid toolchain observation")
	}
	switch observation.Quality {
	case ProbeComplete:
		if observation.ObservedIdentity == "" || observation.Version == "" {
			return fmt.Errorf("complete toolchain observation missing identity/version")
		}
	case ProbeUnavailable:
		if observation.ObservedIdentity != "" || observation.Version != "" {
			return fmt.Errorf("unavailable toolchain observation claims identity/version")
		}
	default:
		return fmt.Errorf("invalid toolchain quality")
	}
	return nil
}

func supportedToolchain(kind string) bool {
	switch kind {
	case "go", "node", "python", "java", "rust":
		return true
	default:
		return false
	}
}

func SupportedToolchains() []string {
	return []string{"go", "node", "python", "java", "rust"}
}

func validQuality(quality Quality) bool {
	return quality == QualityComplete || quality == QualityPartial || quality == QualityUnavailable
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPrefixedDigest(value, prefix string) bool {
	if len(value) != len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	return validDigest(value[len(prefix):])
}
