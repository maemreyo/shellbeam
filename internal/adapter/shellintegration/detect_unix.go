//go:build darwin || linux

package shellintegration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type UnixProbe struct{}

func NewUnixProbe() *UnixProbe { return &UnixProbe{} }

func (*UnixProbe) Probe(ctx context.Context, req app.ProbeRequest) (app.ShellIdentityObservation, error) {
	if err := ctx.Err(); err != nil {
		return app.ShellIdentityObservation{}, err
	}
	if err := req.Validate(); err != nil {
		return app.ShellIdentityObservation{}, err
	}
	command := normalizedCurrentCommand(req.Facts.CurrentCommand)
	family := shellFamily(command)
	runtimeID := shellRuntimeID(req.Facts, command)
	identity := core.ShellIdentity{Family: family, RuntimeID: runtimeID}
	state := app.IdentityExact
	if family == core.ShellUnknown {
		state = app.IdentityUnknown
	}
	if req.Expected != nil && *req.Expected != identity {
		identity.Family = core.ShellUnknown
		state = app.IdentityChanged
	}
	observation := app.ShellIdentityObservation{Identity: identity, State: state, ObservedAt: time.Now().UTC()}
	if err := observation.Validate(); err != nil {
		return app.ShellIdentityObservation{}, err
	}
	return observation, nil
}

func normalizedCurrentCommand(value string) string {
	value = strings.TrimSpace(value)
	base := filepath.Base(value)
	base = strings.TrimLeft(base, "-")
	return base
}

func shellFamily(command string) core.ShellFamily {
	switch command {
	case "fish":
		return core.ShellFish
	case "zsh":
		return core.ShellZsh
	case "bash":
		return core.ShellBash
	case "nu":
		return core.ShellNushell
	default:
		return core.ShellUnknown
	}
}

func shellRuntimeID(facts app.ProviderProcessFacts, command string) string {
	h := sha256.New()
	parts := []string{facts.SessionID, facts.ProviderID, strconv.Itoa(facts.ProviderVersion), facts.ProviderGeneration, strconv.Itoa(facts.PanePID), command}
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("runtime_%s", hex.EncodeToString(h.Sum(nil)))
}
