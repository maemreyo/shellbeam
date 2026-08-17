package inputtrace

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	traceIDPattern    = regexp.MustCompile(`^trace_[0-9A-HJKMNP-TV-Z]{26}$`)
	externalPattern   = regexp.MustCompile(`^external-[1-9][0-9]*$`)
)

func NormalizeMode(mode Mode) (Mode, error) {
	if mode == "" {
		return ModeOff, nil
	}
	switch mode {
	case ModeOff, ModeBestEffort, ModeRequired:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid trace mode")
	}
}

func (p ProviderIdentity) Validate() error {
	if !providerIDPattern.MatchString(p.ID) || p.Version < 1 || p.CapabilityVersion < 1 {
		return fmt.Errorf("invalid input trace provider identity")
	}
	return nil
}

func (e InstrumentationEffect) Valid() bool {
	switch e {
	case EffectNone, EffectNonInvasive, EffectEnvironmentAffecting:
		return true
	default:
		return false
	}
}

func (m CoverageMatrix) Validate(preExec bool) error {
	values := []Coverage{m.FilesystemReads, m.FilesystemMetadataQueries, m.DirectoryEnumerations, m.FilesystemWrites, m.ExecutedBinaries, m.LoadedLibraries, m.EnvironmentNamesObserved, m.NetworkAttempts, m.ChildProcesses}
	for _, value := range values {
		if !validCoverage(value) {
			return fmt.Errorf("invalid input trace coverage")
		}
		if value == CoverageCompleteForOwnedTree && (!preExec || m.ChildProcesses != CoverageCompleteForOwnedTree) {
			return fmt.Errorf("complete owned-tree coverage requires pre-exec full child coverage")
		}
	}
	return nil
}

func validCoverage(value Coverage) bool {
	switch value {
	case CoverageUnsupported, CoverageUnknown, CoveragePartial, CoverageCompleteForOwnedTree:
		return true
	default:
		return false
	}
}

func (b InstrumentationBinding) Validate() error {
	if b.SchemaVersion != SchemaVersion || !traceIDPattern.MatchString(b.TraceID) {
		return fmt.Errorf("invalid input trace binding identity")
	}
	mode, err := NormalizeMode(b.Mode)
	if err != nil || mode == ModeOff || b.Status != BindingActive {
		return fmt.Errorf("invalid active input trace binding")
	}
	if err := b.Provider.Validate(); err != nil || !safeWord(b.Platform, 64) || !validDigest(b.InstrumentationFingerprint) || !b.InstrumentationEffect.Valid() {
		return fmt.Errorf("invalid input trace instrumentation binding")
	}
	if mode == ModeRequired && !b.PreExecCoverageEstablished {
		return fmt.Errorf("required input tracing lacks pre-exec coverage")
	}
	return b.Coverage.Validate(b.PreExecCoverageEstablished)
}

func (r Resource) Validate() error {
	if !validObservationClass(r.ObservationClass) || len(r.Identity) < 1 || len(r.Identity) > 1024 || !utf8.ValidString(r.Identity) || hasControl(r.Identity) {
		return fmt.Errorf("invalid input trace resource")
	}
	switch r.PathClass {
	case PathRepoRelative:
		if strings.ContainsRune(r.Identity, '\\') || strings.HasPrefix(r.Identity, "/") || r.Identity == "." || path.Clean(r.Identity) != r.Identity || strings.HasPrefix(r.Identity, "../") || r.Identity == ".." {
			return fmt.Errorf("invalid repo-relative input trace resource")
		}
	case PathWorkspaceExternalRedacted:
		if !externalPattern.MatchString(r.Identity) {
			return fmt.Errorf("external input trace resource is not redacted")
		}
	case PathSystemClassified:
		if strings.ContainsAny(r.Identity, `/\\`) {
			return fmt.Errorf("system input trace resource must be classified")
		}
	default:
		return fmt.Errorf("invalid input trace path class")
	}
	return nil
}

func validObservationClass(class ObservationClass) bool {
	switch class {
	case ClassFilesystemReads, ClassFilesystemMetadataQueries, ClassDirectoryEnumerations, ClassFilesystemWrites, ClassExecutedBinaries, ClassLoadedLibraries, ClassEnvironmentNamesObserved, ClassNetworkAttempts, ClassChildProcesses:
		return true
	default:
		return false
	}
}

func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersion || !validDigest(r.DerivationKey) || !traceIDPattern.MatchString(r.TraceID) || !safeWord(r.OperationID, 128) || !safeWord(r.SessionID, 128) || !validDigest(r.ReceiptDigest) {
		return fmt.Errorf("invalid input trace record identity")
	}
	mode, err := NormalizeMode(r.Mode)
	if err != nil || mode == ModeOff || r.Provider.Validate() != nil || !safeWord(r.Platform, 64) || !validDigest(r.InstrumentationFingerprint) || !r.InstrumentationEffect.Valid() {
		return fmt.Errorf("invalid input trace record provenance")
	}
	if r.Authority != AuthorityAdvisory || r.ScopeKind != ScopeObservedInput || !r.MayHaveUnobservedDependencies {
		return fmt.Errorf("input trace record overclaims authority")
	}
	if err := r.Coverage.Validate(r.PreExecCoverageEstablished); err != nil {
		return err
	}
	if (r.CaptureStart.IsZero()) != (r.CaptureEnd.IsZero()) || (!r.CaptureStart.IsZero() && (r.CaptureEnd.Before(r.CaptureStart) || r.CaptureEnd.Sub(r.CaptureStart) > MaxTraceCaptureDuration)) {
		return fmt.Errorf("invalid input trace capture interval")
	}
	switch r.Outcome {
	case OutcomeComplete, OutcomePartial, OutcomeUnavailable:
	default:
		return fmt.Errorf("invalid input trace outcome")
	}
	if r.GapReason != "" && r.GapReason != GapOwnershipLost {
		return fmt.Errorf("invalid input trace gap reason")
	}
	if r.GapReason != "" && r.Outcome != OutcomePartial {
		return fmt.Errorf("input trace gap must be partial")
	}
	if r.Truncated && r.Outcome != OutcomePartial {
		return fmt.Errorf("truncated input trace must be partial")
	}
	if len(r.Resources) > MaxPublicResources || r.Summary.ResourcesReturned < 0 || r.Summary.ResourcesObserved < 0 || (r.Summary.ResourcesReturned != 0 && r.Summary.ResourcesReturned != len(r.Resources)) || r.Summary.ResourcesObserved < r.Summary.ResourcesReturned {
		return fmt.Errorf("invalid input trace resource summary")
	}
	external := 0
	seen := make(map[string]struct{}, len(r.Resources))
	for _, resource := range r.Resources {
		if err := resource.Validate(); err != nil {
			return err
		}
		if resource.PathClass == PathWorkspaceExternalRedacted {
			external++
		}
		key := string(resource.ObservationClass) + "\x00" + string(resource.PathClass) + "\x00" + resource.Identity
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate input trace resource")
		}
		seen[key] = struct{}{}
	}
	if external > MaxExternalResources {
		return fmt.Errorf("input trace external resource limit exceeded")
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > MaxPublicRecordBytes {
		return fmt.Errorf("input trace public record limit exceeded")
	}
	return nil
}

func safeWord(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || hasControl(value) || strings.ContainsAny(value, `/\\`) {
		return false
	}
	return true
}

func hasControl(value string) bool {
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
