package terminalpresentation

import (
	"fmt"
	"strings"
	"time"
)

type Platform string

const (
	PlatformDarwin Platform = "darwin"
	PlatformLinux  Platform = "linux"
)

type TerminalIdentity struct {
	ProviderID      string   `json:"provider_id"`
	ProviderVersion uint32   `json:"provider_version"`
	Platform        Platform `json:"platform"`
	BundleID        string   `json:"bundle_id,omitempty"`
	ExecutableName  string   `json:"executable_name"`
}

func (v TerminalIdentity) Validate() error {
	if !validProviderID(v.ProviderID) {
		return fmt.Errorf("invalid terminal provider id")
	}
	if v.ProviderVersion == 0 {
		return fmt.Errorf("invalid terminal provider version")
	}
	switch v.Platform {
	case PlatformDarwin:
		if !validBundleID(v.BundleID) {
			return fmt.Errorf("invalid terminal bundle id")
		}
	case PlatformLinux:
		if v.BundleID != "" && !validBundleID(v.BundleID) {
			return fmt.Errorf("invalid terminal application id")
		}
	default:
		return fmt.Errorf("invalid terminal platform")
	}
	if !validExecutableName(v.ExecutableName) {
		return fmt.Errorf("invalid terminal executable identity")
	}
	return nil
}

func (v TerminalIdentity) StableKey() string {
	return fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s", v.ProviderID, v.ProviderVersion, v.Platform, v.BundleID, v.ExecutableName)
}

type EvidenceSource string

const (
	SourceExistingClient EvidenceSource = "existing_client"
	SourceActive         EvidenceSource = "active"
	SourceRecent         EvidenceSource = "recent"
	SourceBridgeAffinity EvidenceSource = "bridge_affinity"
	SourceSingleRunning  EvidenceSource = "single_running"
	SourceFallback       EvidenceSource = "fallback"
)

func (v EvidenceSource) Validate() error {
	switch v {
	case SourceExistingClient, SourceActive, SourceRecent, SourceBridgeAffinity, SourceSingleRunning, SourceFallback:
		return nil
	default:
		return fmt.Errorf("invalid terminal evidence source")
	}
}

type EvidenceQuality string

const (
	QualityExact     EvidenceQuality = "exact"
	QualityNative    EvidenceQuality = "native"
	QualityValidated EvidenceQuality = "validated"
	QualityQualified EvidenceQuality = "qualified"
)

func (v EvidenceQuality) Validate() error {
	switch v {
	case QualityExact, QualityNative, QualityValidated, QualityQualified:
		return nil
	default:
		return fmt.Errorf("invalid terminal evidence quality")
	}
}

type Evidence struct {
	Identity   TerminalIdentity `json:"identity"`
	Source     EvidenceSource   `json:"source"`
	ObservedAt time.Time        `json:"observed_at"`
	FreshUntil time.Time        `json:"fresh_until"`
	Quality    EvidenceQuality  `json:"quality"`
}

func (v Evidence) Validate() error {
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if err := v.Quality.Validate(); err != nil {
		return err
	}
	if v.ObservedAt.IsZero() || v.FreshUntil.IsZero() {
		return fmt.Errorf("terminal evidence freshness is required")
	}
	if v.FreshUntil.Before(v.ObservedAt) {
		return fmt.Errorf("terminal evidence freshness precedes observation")
	}
	return nil
}

func (v Evidence) FreshAt(now time.Time) bool {
	if v.Validate() != nil {
		return false
	}
	return !now.Before(v.ObservedAt) && !now.After(v.FreshUntil)
}

type Candidate struct {
	Evidence Evidence `json:"evidence"`
}

func (v Candidate) Validate() error { return v.Evidence.Validate() }

type Resolution struct {
	Selected *Candidate `json:"selected,omitempty"`
}

func validProviderID(value string) bool {
	if value == "" || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func validBundleID(value string) bool {
	if value == "" || len(value) > 255 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func validExecutableName(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\") {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c <= ' ' || c == 0x7f {
			return false
		}
	}
	return true
}
