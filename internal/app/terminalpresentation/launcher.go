package terminalpresentation

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"

	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type LaunchRequest struct {
	Identity   core.TerminalIdentity
	attachArgv []string
}

func BuildAttachArgv(executable, handoffID string) ([]string, error) {
	if !validInstalledExecutable(executable) {
		return nil, errors.New("invalid shellbeam executable identity")
	}
	if err := handoff.ValidateHandoffID(handoffID); err != nil {
		return nil, err
	}
	return []string{executable, "session", "attach", "--handoff-id", handoffID}, nil
}

func ValidateAttachArgv(argv []string) error {
	if len(argv) != 5 {
		return errors.New("invalid terminal attach argv shape")
	}
	if !validInstalledExecutable(argv[0]) {
		return errors.New("invalid shellbeam executable identity")
	}
	if argv[1] != "session" || argv[2] != "attach" || argv[3] != "--handoff-id" {
		return errors.New("invalid terminal attach argv contract")
	}
	if err := handoff.ValidateHandoffID(argv[4]); err != nil {
		return err
	}
	return nil
}

func NewLaunchRequest(identity core.TerminalIdentity, attachArgv []string) (LaunchRequest, error) {
	if err := identity.Validate(); err != nil {
		return LaunchRequest{}, err
	}
	if err := ValidateAttachArgv(attachArgv); err != nil {
		return LaunchRequest{}, err
	}
	return LaunchRequest{Identity: identity, attachArgv: append([]string(nil), attachArgv...)}, nil
}

func (r LaunchRequest) Validate() error {
	if err := r.Identity.Validate(); err != nil {
		return err
	}
	return ValidateAttachArgv(r.attachArgv)
}

func (r LaunchRequest) AttachArgv() []string {
	return append([]string(nil), r.attachArgv...)
}

type LaunchResult struct {
	Attempted  bool               `json:"attempted"`
	Outcome    core.LaunchOutcome `json:"outcome"`
	ProviderID string             `json:"provider_id"`
	Reason     string             `json:"reason"`
}

func (r LaunchResult) Validate() error {
	if err := r.Outcome.Validate(); err != nil {
		return err
	}
	if !validResultToken(r.ProviderID) || !validResultToken(r.Reason) {
		return errors.New("invalid terminal launch result metadata")
	}
	if !r.Attempted && r.Outcome != core.LaunchOutcomeFailed {
		return errors.New("unattempted terminal launch cannot be unknown or proven")
	}
	if r.Attempted && r.Outcome == core.LaunchOutcomeFailed {
		return errors.New("attempted terminal launch cannot be known unstarted failure")
	}
	return nil
}

func validInstalledExecutable(value string) bool {
	if !filepath.IsAbs(value) || value == filepath.Clean(string(filepath.Separator)) || strings.IndexByte(value, 0) >= 0 {
		return false
	}
	if filepath.Base(filepath.Clean(value)) != "shellbeam" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validResultToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
