package shellintegration

import "fmt"

type ShellFamily string

const (
	ShellFish    ShellFamily = "fish"
	ShellZsh     ShellFamily = "zsh"
	ShellBash    ShellFamily = "bash"
	ShellUnknown ShellFamily = "unknown"
)

func (v ShellFamily) Validate() error {
	switch v {
	case ShellFish, ShellZsh, ShellBash, ShellUnknown:
		return nil
	default:
		return fmt.Errorf("invalid shell family")
	}
}

type ShellIdentity struct {
	Family    ShellFamily `json:"family"`
	RuntimeID string      `json:"runtime_id"`
}

func (v ShellIdentity) Validate() error {
	if err := v.Family.Validate(); err != nil {
		return err
	}
	if !validOpaque(v.RuntimeID, 256) {
		return fmt.Errorf("invalid shell runtime identity")
	}
	return nil
}

type CapabilityLevel string

const (
	CapabilityPTYOnly          CapabilityLevel = "pty_only"
	CapabilityInteractive      CapabilityLevel = "interactive"
	CapabilityShellAware       CapabilityLevel = "shell_aware"
	CapabilityRequirementAware CapabilityLevel = "requirement_aware"
	CapabilityFullHandoff      CapabilityLevel = "full_handoff"
)

func (v CapabilityLevel) Validate() error {
	switch v {
	case CapabilityPTYOnly, CapabilityInteractive, CapabilityShellAware, CapabilityRequirementAware, CapabilityFullHandoff:
		return nil
	default:
		return fmt.Errorf("invalid shell capability level")
	}
}

func validOpaque(value string, max int) bool {
	if value == "" || len(value) > max || !isAlphaNumeric(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !isAlphaNumeric(c) && c != '_' && c != '-' && c != '.' && c != ':' {
			return false
		}
	}
	return true
}

func isAlphaNumeric(c byte) bool { return isAlpha(c) || c >= '0' && c <= '9' }
func isAlpha(c byte) bool        { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
