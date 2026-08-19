package bridge

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type TerminalLaunchContext struct {
	ObservedAt          time.Time
	Environment         map[string]string
	AncestorExecutables []string
}

func CaptureTerminalAffinity(context TerminalLaunchContext, known []core.TerminalIdentity, freshness time.Duration) (*core.BridgeAffinityHint, error) {
	if context.ObservedAt.IsZero() {
		return nil, errors.New("bridge terminal affinity observation time is required")
	}
	if freshness <= 0 || freshness > core.MaxBridgeAffinityFreshness {
		return nil, errors.New("invalid bridge terminal affinity freshness")
	}
	providers, err := validatedAffinityProviders(known)
	if err != nil {
		return nil, err
	}
	ancestor, ok := uniqueAncestorProvider(context.AncestorExecutables, providers)
	if !ok {
		return nil, nil
	}
	if environmentProvider, environmentKnown := knownEnvironmentProvider(context.Environment, providers); environmentKnown && environmentProvider.StableKey() != ancestor.StableKey() {
		return nil, nil
	}
	hint, err := core.NewBridgeAffinityHint(ancestor, context.ObservedAt, freshness)
	if err != nil {
		return nil, err
	}
	return &hint, nil
}

func validatedAffinityProviders(known []core.TerminalIdentity) ([]core.TerminalIdentity, error) {
	if len(known) == 0 {
		return nil, nil
	}
	result := make([]core.TerminalIdentity, len(known))
	seen := make(map[string]struct{}, len(known))
	for i, identity := range known {
		if err := identity.Validate(); err != nil {
			return nil, err
		}
		key := identity.StableKey()
		if _, exists := seen[key]; exists {
			return nil, errors.New("duplicate bridge terminal affinity provider")
		}
		seen[key] = struct{}{}
		result[i] = identity
	}
	return result, nil
}

func uniqueAncestorProvider(ancestors []string, providers []core.TerminalIdentity) (core.TerminalIdentity, bool) {
	matched := make(map[string]core.TerminalIdentity)
	for _, executable := range ancestors {
		base := strings.ToLower(filepath.Base(strings.TrimSpace(executable)))
		if base == "" || base == "." {
			continue
		}
		for _, identity := range providers {
			if base == strings.ToLower(identity.ExecutableName) {
				matched[identity.StableKey()] = identity
			}
		}
	}
	if len(matched) != 1 {
		return core.TerminalIdentity{}, false
	}
	for _, identity := range matched {
		return identity, true
	}
	return core.TerminalIdentity{}, false
}

func knownEnvironmentProvider(environment map[string]string, providers []core.TerminalIdentity) (core.TerminalIdentity, bool) {
	value := strings.ToLower(strings.TrimSpace(environment["TERM_PROGRAM"]))
	if value == "" {
		return core.TerminalIdentity{}, false
	}
	var matched *core.TerminalIdentity
	for _, identity := range providers {
		providerID := strings.ToLower(identity.ProviderID)
		executable := strings.ToLower(identity.ExecutableName)
		if value != providerID && value != executable {
			continue
		}
		if matched != nil && matched.StableKey() != identity.StableKey() {
			return core.TerminalIdentity{}, false
		}
		copy := identity
		matched = &copy
	}
	if matched == nil {
		return core.TerminalIdentity{}, false
	}
	return *matched, true
}

func (h *Handler) SetTerminalAffinity(hint core.BridgeAffinityHint) error {
	if h == nil {
		return errors.New("nil bridge handler")
	}
	if err := hint.Validate(); err != nil {
		return err
	}
	copy := hint
	h.terminalAffinityMu.Lock()
	h.terminalAffinity = &copy
	h.terminalAffinityMu.Unlock()
	return nil
}

func (h *Handler) TerminalAffinity() *core.BridgeAffinityHint {
	if h == nil {
		return nil
	}
	h.terminalAffinityMu.RLock()
	defer h.terminalAffinityMu.RUnlock()
	if h.terminalAffinity == nil {
		return nil
	}
	copy := *h.terminalAffinity
	return &copy
}
