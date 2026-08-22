package contextexec

import (
	"fmt"
	"path/filepath"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

type ShellContinuityExpectation struct {
	SessionID                string
	AuthorityEpoch           delegated.AuthorityEpoch
	ProviderGeneration       string
	ShellRuntimeIdentity     string
	PaneShellPID             int
	PaneShellProcessIdentity string
	PaneTTY                  string
	HelperExecutableIdentity string
}

func (e ShellContinuityExpectation) Validate() error {
	if !validOpaque(e.SessionID, MaxSessionIDBytes) {
		return fmt.Errorf("invalid shell continuity session identity")
	}
	if err := e.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if !validOpaque(e.ProviderGeneration, MaxOpaqueRefBytes) {
		return fmt.Errorf("invalid shell continuity provider generation")
	}
	if !validOpaque(e.ShellRuntimeIdentity, MaxIdentityBytes) {
		return fmt.Errorf("invalid shell continuity runtime identity")
	}
	if e.PaneShellPID <= 1 {
		return fmt.Errorf("invalid shell continuity pane pid")
	}
	if !validOpaque(e.PaneShellProcessIdentity, MaxIdentityBytes) {
		return fmt.Errorf("invalid shell continuity process identity")
	}
	if e.PaneTTY == "" || len(e.PaneTTY) > MaxPathBytes || !filepath.IsAbs(e.PaneTTY) {
		return fmt.Errorf("invalid shell continuity pane tty")
	}
	if e.HelperExecutableIdentity == "" || len(e.HelperExecutableIdentity) > MaxPathBytes || !filepath.IsAbs(e.HelperExecutableIdentity) {
		return fmt.Errorf("invalid shell continuity helper identity")
	}
	return nil
}

type ShellContinuityProof struct {
	SessionID                string
	AuthorityEpoch           delegated.AuthorityEpoch
	ProviderGeneration       string
	ShellRuntimeIdentity     string
	PaneShellPID             int
	PaneShellProcessIdentity string
	PaneTTY                  string
	HelperPID                int
	HelperExecutableIdentity string
	ForegroundProven         bool
	ObservedAt               time.Time
}

func (p ShellContinuityProof) Validate() error {
	expectation := ShellContinuityExpectation{
		SessionID:                p.SessionID,
		AuthorityEpoch:           p.AuthorityEpoch,
		ProviderGeneration:       p.ProviderGeneration,
		ShellRuntimeIdentity:     p.ShellRuntimeIdentity,
		PaneShellPID:             p.PaneShellPID,
		PaneShellProcessIdentity: p.PaneShellProcessIdentity,
		PaneTTY:                  p.PaneTTY,
		HelperExecutableIdentity: p.HelperExecutableIdentity,
	}
	if err := expectation.Validate(); err != nil {
		return err
	}
	if p.HelperPID <= 1 {
		return fmt.Errorf("invalid shell continuity helper pid")
	}
	if !p.ForegroundProven {
		return fmt.Errorf("shell continuity foreground unproven")
	}
	if p.ObservedAt.IsZero() {
		return fmt.Errorf("shell continuity observation time missing")
	}
	return nil
}

func (p ShellContinuityProof) ValidateFor(e ShellContinuityExpectation) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.SessionID != e.SessionID ||
		p.AuthorityEpoch != e.AuthorityEpoch ||
		p.ProviderGeneration != e.ProviderGeneration ||
		p.ShellRuntimeIdentity != e.ShellRuntimeIdentity ||
		p.PaneShellPID != e.PaneShellPID ||
		p.PaneShellProcessIdentity != e.PaneShellProcessIdentity ||
		filepath.Clean(p.PaneTTY) != filepath.Clean(e.PaneTTY) ||
		filepath.Clean(p.HelperExecutableIdentity) != filepath.Clean(e.HelperExecutableIdentity) {
		return fmt.Errorf("context shell continuity mismatch")
	}
	return nil
}
