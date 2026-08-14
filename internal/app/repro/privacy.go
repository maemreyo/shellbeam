package repro

import (
	"strings"
	"unicode"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
)

func commandDescriptor(reservation operation.Reservation, executionFingerprint, receiptMode, receiptExecutable string) (core.ExecutionDescriptor, error) {
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
	if mode != string(operation.ExecutionModeArgv) {
		return descriptor, nil
	}
	if len(reservation.Argv) == 0 || len(reservation.Argv) > core.MaxResolvedArgv {
		return descriptor, nil
	}
	argv := make([]string, 0, len(reservation.Argv))
	for _, arg := range reservation.Argv {
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
