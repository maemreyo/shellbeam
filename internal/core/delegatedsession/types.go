package delegatedsession

import (
	"fmt"
	"time"
)

const (
	ModeDelegatedInteractive = "delegated_interactive"
	MaxProviderIDBytes       = 128
	BindingSchemaVersion     = 1
	ProviderRefSchemaVersion = 1
)

func ValidateMode(mode string) error {
	if mode != ModeDelegatedInteractive {
		return fmt.Errorf("invalid delegated session mode")
	}
	return nil
}

type ProviderIdentity struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

func (p ProviderIdentity) Validate() error {
	if !validOpaque(p.ID, MaxProviderIDBytes) || p.Version < 1 {
		return fmt.Errorf("invalid delegated provider identity")
	}
	return nil
}

type Lifecycle string

const (
	LifecycleProvisioning Lifecycle = "provisioning"
	LifecycleLive         Lifecycle = "live"
	LifecycleTerminal     Lifecycle = "terminal"
	LifecycleLost         Lifecycle = "lost"
)

func (l Lifecycle) Validate() error {
	switch l {
	case LifecycleProvisioning, LifecycleLive, LifecycleTerminal, LifecycleLost:
		return nil
	default:
		return fmt.Errorf("invalid delegated session lifecycle")
	}
}

type Binding struct {
	SchemaVersion   int            `json:"schema_version"`
	SessionID       string         `json:"session_id"`
	OperationID     string         `json:"operation_id"`
	SessionName     string         `json:"session_name,omitempty"`
	SessionMode     string         `json:"session_mode"`
	AuthorityEpoch  AuthorityEpoch `json:"authority_epoch"`
	DesiredOwner    Owner          `json:"desired_owner"`
	ProviderID      string         `json:"provider_id"`
	ProviderVersion int            `json:"provider_version"`
	Lifecycle       Lifecycle      `json:"lifecycle"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (b Binding) ProviderIdentity() ProviderIdentity {
	return ProviderIdentity{ID: b.ProviderID, Version: b.ProviderVersion}
}

func (b Binding) Validate() error {
	if b.SchemaVersion != BindingSchemaVersion || !validOpaque(b.SessionID, 128) || !validOpaque(b.OperationID, 128) {
		return fmt.Errorf("invalid delegated session binding")
	}
	if b.SessionName != "" && !validSessionName(b.SessionName) {
		return fmt.Errorf("invalid delegated session name")
	}
	if err := ValidateMode(b.SessionMode); err != nil {
		return err
	}
	if err := b.ProviderIdentity().Validate(); err != nil {
		return err
	}
	if err := b.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if err := b.DesiredOwner.Validate(); err != nil {
		return err
	}
	if err := b.Lifecycle.Validate(); err != nil {
		return err
	}
	if b.CreatedAt.IsZero() || b.UpdatedAt.IsZero() || b.UpdatedAt.Before(b.CreatedAt) {
		return fmt.Errorf("invalid delegated session timestamps")
	}
	return nil
}

type ProviderRef struct {
	SchemaVersion   int       `json:"schema_version"`
	SessionID       string    `json:"session_id"`
	ProviderID      string    `json:"provider_id"`
	ProviderVersion int       `json:"provider_version"`
	Ref             string    `json:"ref"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (r ProviderRef) Validate() error {
	if r.SchemaVersion != ProviderRefSchemaVersion || !validOpaque(r.SessionID, 128) || !validOpaque(r.Ref, 256) {
		return fmt.Errorf("invalid delegated provider ref")
	}
	if err := (ProviderIdentity{ID: r.ProviderID, Version: r.ProviderVersion}).Validate(); err != nil {
		return err
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return fmt.Errorf("invalid delegated provider ref timestamps")
	}
	return nil
}

func validSessionName(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func validOpaque(v string, max int) bool {
	if len(v) < 1 || len(v) > max {
		return false
	}
	for i := 0; i < len(v); i++ {
		b := v[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}
