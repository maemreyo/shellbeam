package delegatedtmux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const privateStateSchemaVersion = 1

type privateState struct {
	SchemaVersion      int       `json:"schema_version"`
	Ref                string    `json:"ref"`
	SessionID          string    `json:"session_id"`
	SocketPath         string    `json:"socket_path"`
	TmuxSession        string    `json:"tmux_session"`
	SessionInternalID  string    `json:"session_internal_id"`
	WindowID           string    `json:"window_id"`
	PaneID             string    `json:"pane_id"`
	ProviderGeneration string    `json:"provider_generation"`
	StartGatePath      string    `json:"start_gate_path"`
	StartReleased      bool      `json:"start_released"`
	ServerPID          int       `json:"server_pid"`
	PanePID            int       `json:"pane_pid"`
	TmuxVersion        string    `json:"tmux_version"`
	TmuxSHA256         string    `json:"tmux_sha256"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (s privateState) validate() error {
	if s.SchemaVersion != privateStateSchemaVersion || !safeOpaque(s.Ref, 128) || !safeOpaque(s.SessionID, 128) || !filepath.IsAbs(s.SocketPath) || !safeOpaque(s.TmuxSession, 128) || !prefixedOpaque(s.SessionInternalID, '$') || !prefixedOpaque(s.WindowID, '@') || !prefixedOpaque(s.PaneID, '%') || !safeOpaque(s.ProviderGeneration, 128) || !filepath.IsAbs(s.StartGatePath) || s.ServerPID <= 0 || s.PanePID <= 0 || s.TmuxVersion == "" || !validDigest(s.TmuxSHA256) || s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return fmt.Errorf("invalid delegated tmux private state")
	}
	return nil
}

type privateStateStore struct{ root string }

func (s privateStateStore) path(ref string) string {
	if !safeOpaque(ref, 128) {
		return ""
	}
	return filepath.Join(s.root, "provider-state", ref+".json")
}
func (s privateStateStore) save(state privateState) error {
	if err := state.validate(); err != nil {
		return err
	}
	path := s.path(state.Ref)
	if path == "" {
		return fmt.Errorf("invalid provider ref")
	}
	dir := filepath.Dir(path)
	if err := ensurePrivateDirectory(dir); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s privateStateStore) load(ref string) (privateState, error) {
	var out privateState
	path := s.path(ref)
	if path == "" {
		return out, fmt.Errorf("invalid provider ref")
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	if err := out.validate(); err != nil {
		return out, err
	}
	if out.Ref != ref {
		return out, fmt.Errorf("provider ref mismatch")
	}
	return out, nil
}

func safeOpaque(v string, max int) bool {
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
func prefixedOpaque(v string, prefix byte) bool {
	return len(v) >= 2 && v[0] == prefix && safeOpaque(v[1:], 127)
}

func (s privateStateStore) remove(ref string) error {
	path := s.path(ref)
	if path == "" {
		return fmt.Errorf("invalid provider ref")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	if f, err := os.Open(dir); err == nil {
		defer f.Close()
		return f.Sync()
	}
	return nil
}
