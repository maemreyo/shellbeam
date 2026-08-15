package repro

import (
	"strings"
	"unicode"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
)

func commandDescriptor(reservation operation.Reservation, executionFingerprint, receiptMode, receiptExecutable string, binding *project.CommandBinding) (core.ExecutionDescriptor, error) {
	mode := string(reservation.ExecutionMode)
	if receiptMode != "" {
		mode = receiptMode
	}
	descriptor := core.ExecutionDescriptor{
		OperationID:                 string(reservation.OperationID),
		SessionID:                   string(reservation.SessionID),
		CommandSemanticsFingerprint: executionFingerprint,
		ExecutionMode:               mode,
		CommandDetails:              core.CapturePartial,
	}
	executable := reservation.Executable
	if receiptExecutable != "" {
		executable = receiptExecutable
	}
	if safeCommandAtom(executable, 512) {
		descriptor.Executable = executable
	}
	argvSource := reservation.Argv
	if binding != nil {
		if err := binding.Validate(); err != nil {
			return core.ExecutionDescriptor{}, err
		}
		bindingDigest, err := binding.Digest()
		if err != nil {
			return core.ExecutionDescriptor{}, err
		}
		descriptor.ProjectCommandBindingDigest = bindingDigest
		descriptor.ProjectManifestDigest = binding.ManifestDigest
		descriptor.ProjectCommandID = binding.CommandID
		descriptor.ParameterBindingFingerprint = binding.ParameterFingerprint
		for _, parameter := range binding.Parameters {
			if parameter.ProviderID == "" {
				continue
			}
			descriptor.ParameterProviders = append(descriptor.ParameterProviders, core.ParameterProviderDescriptor{ParameterID: parameter.ID, ProviderID: parameter.ProviderID, ProviderVersion: parameter.ProviderVersion})
		}
		argvSource = binding.ResolvedArgv
	}
	if mode != string(operation.ExecutionModeArgv) {
		return descriptor, nil
	}
	if len(argvSource) == 0 || len(argvSource) > core.MaxResolvedArgv {
		return descriptor, nil
	}
	argv := make([]string, 0, len(argvSource))
	for _, arg := range argvSource {
		if !safeCommandAtom(arg, core.MaxArgumentBytes) {
			return descriptor, nil
		}
		argv = append(argv, arg)
	}
	descriptor.ResolvedArgv = argv
	descriptor.CommandDetails = core.CaptureExact
	return descriptor, nil
}

func safeCommandAtom(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"token=", "password=", "passwd=", "secret=", "api_key=", "apikey=",
		"authorization:", "bearer ", "begin private key", "private_key=",
	} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	if strings.HasPrefix(value, "/Users/") || strings.HasPrefix(value, "/home/") || strings.HasPrefix(value, "~/") {
		return false
	}
	return true
}
