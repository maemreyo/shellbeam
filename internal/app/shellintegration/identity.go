package shellintegration

import (
	"fmt"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type IdentityState string

const (
	IdentityExact   IdentityState = "exact"
	IdentityChanged IdentityState = "changed"
	IdentityUnknown IdentityState = "unknown"
)

func (v IdentityState) Validate() error {
	switch v {
	case IdentityExact, IdentityChanged, IdentityUnknown:
		return nil
	default:
		return fmt.Errorf("invalid shell identity state")
	}
}

type ProviderProcessFacts struct {
	SessionID          string
	ProviderID         string
	ProviderVersion    int
	ProviderGeneration string
	PanePID            int
	CurrentCommand     string
	LoginShell         string
}

func (v ProviderProcessFacts) Validate() error {
	if !validFactID(v.SessionID, 128) || !validFactID(v.ProviderID, 128) || v.ProviderVersion < 1 || !validFactID(v.ProviderGeneration, 128) || v.PanePID <= 1 {
		return fmt.Errorf("invalid provider process identity")
	}
	if !validProcessFact(v.CurrentCommand, 256) {
		return fmt.Errorf("invalid current shell process fact")
	}
	if v.LoginShell != "" && !validProcessFact(v.LoginShell, 1024) {
		return fmt.Errorf("invalid login shell fact")
	}
	return nil
}

type ShellIdentityObservation struct {
	Identity   core.ShellIdentity
	State      IdentityState
	ObservedAt time.Time
}

func (v ShellIdentityObservation) Validate() error {
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if v.ObservedAt.IsZero() {
		return fmt.Errorf("shell identity observation missing")
	}
	if v.State == IdentityExact && v.Identity.Family == core.ShellUnknown {
		return fmt.Errorf("exact shell identity is unknown")
	}
	if v.State != IdentityExact && v.Identity.Family != core.ShellUnknown {
		return fmt.Errorf("non-exact shell identity claims adapter family")
	}
	return nil
}

func (v ShellIdentityObservation) AdapterEligible() bool {
	return v.Validate() == nil && v.State == IdentityExact && v.Identity.Family != core.ShellUnknown
}

func validFactID(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}

func validProcessFact(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}
