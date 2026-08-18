package delegatedsession

import "fmt"

const (
	ModeDelegatedInteractive = "delegated_interactive"
	MaxProviderIDBytes       = 128
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

type Binding struct {
	SessionID    string           `json:"session_id"`
	OperationID  string           `json:"operation_id"`
	Provider     ProviderIdentity `json:"provider"`
	Epoch        AuthorityEpoch   `json:"authority_epoch"`
	DesiredOwner Owner            `json:"desired_owner"`
}

func (b Binding) Validate() error {
	if !validOpaque(b.SessionID, 128) || !validOpaque(b.OperationID, 128) {
		return fmt.Errorf("invalid delegated session binding")
	}
	if err := b.Provider.Validate(); err != nil {
		return err
	}
	if err := b.Epoch.Validate(); err != nil {
		return err
	}
	if err := b.DesiredOwner.Validate(); err != nil {
		return err
	}
	return nil
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
