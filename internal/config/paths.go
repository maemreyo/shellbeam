package config

import (
	"fmt"
	"path/filepath"
)

type Paths struct {
	ConfigFile string
	StateDir   string
	RuntimeDir string
	Socket     string
}

func ResolvePaths(goos string, uid int, home string, env map[string]string) (Paths, error) {
	if uid < 0 || !filepath.IsAbs(home) {
		return Paths{}, fmt.Errorf("invalid uid or home")
	}
	var p Paths
	switch goos {
	case "linux":
		cfg, state := env["XDG_CONFIG_HOME"], env["XDG_STATE_HOME"]
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		if state == "" {
			state = filepath.Join(home, ".local", "state")
		}
		if !filepath.IsAbs(cfg) || !filepath.IsAbs(state) {
			return Paths{}, fmt.Errorf("xdg paths must be absolute")
		}
		p.ConfigFile = filepath.Join(cfg, "shellbeam", "config.toml")
		p.StateDir = filepath.Join(state, "shellbeam")
		runtime := env["XDG_RUNTIME_DIR"]
		if runtime != "" && filepath.IsAbs(runtime) {
			p.RuntimeDir = filepath.Join(runtime, "shellbeam")
		} else {
			p.RuntimeDir = fmt.Sprintf("/tmp/shellbeam-%d", uid)
		}
	case "darwin":
		root := filepath.Join(home, "Library", "Application Support", "ShellBeam")
		p.ConfigFile = filepath.Join(root, "config.toml")
		p.StateDir = root
		p.RuntimeDir = fmt.Sprintf("/tmp/shellbeam-%d", uid)
	default:
		return Paths{}, fmt.Errorf("unsupported OS %q", goos)
	}
	p.Socket = filepath.Join(p.RuntimeDir, "daemon.sock")
	if len([]byte(p.Socket)) > 100 {
		return Paths{}, fmt.Errorf("socket path exceeds 100 bytes")
	}
	return p, nil
}
