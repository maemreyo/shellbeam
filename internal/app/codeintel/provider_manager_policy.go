package codeintel

import (
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (l ProviderManagerLimits) Validate() error {
	if l.MaxInstances < 1 || l.MaxInstances > 64 ||
		l.MaxInFlight < 1 || l.MaxInFlight > 1024 ||
		l.MaxInFlightPerProvider < 1 || l.MaxInFlightPerProvider > l.MaxInFlight ||
		l.MaxQueueDepth < 0 || l.MaxQueueDepth > 1024 ||
		l.IdleTTL <= 0 || l.IdleTTL > 24*time.Hour ||
		l.FailuresBeforeCooldown < 1 || l.FailuresBeforeCooldown > 100 ||
		l.FailureWindow <= 0 || l.FailureWindow > 24*time.Hour ||
		l.Cooldown <= 0 || l.Cooldown > 24*time.Hour {
		return fmt.Errorf("invalid provider manager limits")
	}
	if (l.MaxQueueDepth == 0 && l.QueueWait != 0) ||
		(l.MaxQueueDepth > 0 && (l.QueueWait <= 0 || l.QueueWait > 5*time.Second)) {
		return fmt.Errorf("invalid provider queue limits")
	}
	return nil
}

func (m *ProviderManager) cooldownActiveLocked(key providerKey, now time.Time) bool {
	health := m.health[key]
	if health == nil || health.cooldownUntil.IsZero() {
		return false
	}
	if now.Before(health.cooldownUntil) {
		return true
	}
	health.cooldownUntil = time.Time{}
	health.failures = nil
	return false
}

func (m *ProviderManager) recordFailureLocked(key providerKey, now time.Time) {
	health := m.health[key]
	if health == nil {
		health = &providerHealth{}
		m.health[key] = health
	}
	cutoff := now.Add(-m.limits.FailureWindow)
	kept := health.failures[:0]
	for _, failure := range health.failures {
		if failure.After(cutoff) {
			kept = append(kept, failure)
		}
	}
	health.failures = append(kept, now)
	if len(health.failures) >= m.limits.FailuresBeforeCooldown {
		health.cooldownUntil = now.Add(m.limits.Cooldown)
	}
}

func (m *ProviderManager) collectExpiredIdleLocked(now time.Time) []Provider {
	var evicted []Provider
	for key, instance := range m.instances {
		if instance.starting || instance.inFlight != 0 || now.Sub(instance.lastUsed) < m.limits.IdleTTL {
			continue
		}
		delete(m.instances, key)
		evicted = append(evicted, instance.provider)
	}
	return evicted
}

func (m *ProviderManager) evictIncompatibleIdleLocked(key providerKey) []Provider {
	var evicted []Provider
	for existingKey, instance := range m.instances {
		if existingKey.workspaceID != key.workspaceID || existingKey.providerID != key.providerID ||
			existingKey == key || instance.starting || instance.inFlight != 0 {
			continue
		}
		delete(m.instances, existingKey)
		evicted = append(evicted, instance.provider)
	}
	return evicted
}

func (m *ProviderManager) evictOldestIdleLocked() Provider {
	var oldest *managedProvider
	for _, instance := range m.instances {
		if instance.starting || instance.inFlight != 0 {
			continue
		}
		if oldest == nil || instance.lastUsed.Before(oldest.lastUsed) {
			oldest = instance
		}
	}
	if oldest == nil {
		return nil
	}
	delete(m.instances, oldest.key)
	return oldest.provider
}

func providerCompatibilityKey(workspace workspacecore.Workspace, options ProviderStartOptions) providerKey {
	return providerKey{
		workspaceID: workspace.ID, providerID: options.ProviderID,
		executableIdentity: options.ExecutableIdentity,
		configFingerprint:  options.ConfigFingerprint,
		buildFingerprint:   options.BuildFingerprint,
	}
}

func sameProviderIdentity(a, b core.ProviderMetadata) bool {
	return a.ProviderID == b.ProviderID &&
		a.Incarnation == b.Incarnation &&
		a.ExecutableVersion == b.ExecutableVersion &&
		a.ConfigFingerprint == b.ConfigFingerprint &&
		a.BuildFingerprint == b.BuildFingerprint
}

func validateStartedProvider(metadata core.ProviderMetadata, options ProviderStartOptions) error {
	if err := metadata.Validate(); err != nil {
		return fmt.Errorf("invalid provider metadata: %w", err)
	}
	if metadata.ProviderID != options.ProviderID ||
		metadata.ConfigFingerprint != options.ConfigFingerprint ||
		metadata.BuildFingerprint != options.BuildFingerprint {
		return fmt.Errorf("provider compatibility metadata mismatch")
	}
	return nil
}

func normalizeProviderResponse(response *ProviderResponse, metadata core.ProviderMetadata) error {
	if response.Metadata == (core.ProviderMetadata{}) {
		response.Metadata = metadata
		return nil
	}
	if err := response.Metadata.Validate(); err != nil {
		return fmt.Errorf("invalid provider response metadata: %w", err)
	}
	if !sameProviderIdentity(metadata, response.Metadata) {
		return fmt.Errorf("provider response identity mismatch")
	}
	return nil
}

func validateProviderOptions(options ProviderStartOptions, query core.Query) error {
	for _, value := range []string{
		options.ProviderID, options.ExecutableIdentity, options.ConfigFingerprint, options.BuildFingerprint,
	} {
		if !safeProviderText(value) {
			return fmt.Errorf("invalid provider start options")
		}
	}
	if query.Provider != "" && query.Provider != options.ProviderID {
		return fmt.Errorf("resolved provider does not match query")
	}
	return nil
}

func safeProviderText(value string) bool {
	if value == "" || len(value) > core.MaxProviderTextBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func providerBusyError() error {
	return newError(CodeProviderBusy, true, fmt.Errorf("provider capacity unavailable"))
}

func closeProviders(providers []Provider) {
	for _, provider := range providers {
		if provider != nil {
			_ = provider.Close()
		}
	}
}
