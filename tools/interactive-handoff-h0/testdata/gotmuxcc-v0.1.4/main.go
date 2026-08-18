//go:build h0_gotmuxcc_probe

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/atomicstack/gotmuxcc/gotmuxcc"
)

type result struct {
	Version                string `json:"version"`
	ClientPID              int    `json:"client_pid"`
	ReconnectPID           int    `json:"reconnect_pid"`
	ReconnectOK            bool   `json:"reconnect_ok"`
	PaneToggleOK           bool   `json:"pane_toggle_ok"`
	PaneDisableError       string `json:"pane_disable_error,omitempty"`
	PaneEnableError        string `json:"pane_enable_error,omitempty"`
	CommandErrorPropagated bool   `json:"command_error_propagated"`
	LargeOutputBytes       int    `json:"large_output_bytes"`
}

type probeConfig struct {
	socket           string
	pane             string
	constructorReady string
	allowPrivate     string
	privateReady     string
	allowFinish      string
}

func parseProbeConfig() (probeConfig, error) {
	var cfg probeConfig
	flag.StringVar(&cfg.socket, "socket", "", "tmux socket")
	flag.StringVar(&cfg.pane, "pane", "", "target pane")
	flag.StringVar(&cfg.constructorReady, "constructor-ready", "", "constructor-ready sentinel")
	flag.StringVar(&cfg.allowPrivate, "allow-private", "", "allow-private sentinel")
	flag.StringVar(&cfg.privateReady, "private-ready", "", "private-ready sentinel")
	flag.StringVar(&cfg.allowFinish, "allow-finish", "", "allow-finish sentinel")
	flag.Parse()
	if cfg.socket == "" || cfg.pane == "" || cfg.constructorReady == "" || cfg.allowPrivate == "" || cfg.privateReady == "" || cfg.allowFinish == "" {
		return probeConfig{}, fmt.Errorf("all flags are required")
	}
	return cfg, nil
}

func main() {
	cfg, err := parseProbeConfig()
	if err != nil {
		fatal(err)
	}
	t, err := gotmuxcc.NewTmux(cfg.socket)
	if err != nil {
		fatal(err)
	}
	version, err := t.Command("display-message", "-p", "#{version}")
	if err != nil {
		fatal(err)
	}
	pid, err := commandPID(t)
	if err != nil {
		fatal(err)
	}
	if err := touch(cfg.constructorReady); err != nil {
		fatal(err)
	}
	if err := waitFile(cfg.allowPrivate, 10*time.Second); err != nil {
		fatal(err)
	}
	if err := t.SetControlFlags("no-output"); err != nil {
		fatal(err)
	}
	if err := touch(cfg.privateReady); err != nil {
		fatal(err)
	}
	if err := waitFile(cfg.allowFinish, 10*time.Second); err != nil {
		fatal(err)
	}
	if err := t.SetControlFlags("!no-output"); err != nil {
		fatal(err)
	}

	disableErr := t.DisablePaneOutput(cfg.pane)
	enableErr := t.EnablePaneOutput(cfg.pane)
	paneToggleOK := disableErr == nil && enableErr == nil
	large, err := t.Command("show-options", "-g")
	if err != nil {
		fatal(err)
	}
	_, invalidErr := t.Command("shellbeam-h0-definitely-invalid-command")
	if err := t.Close(); err != nil {
		fatal(err)
	}

	reconnected, err := gotmuxcc.NewTmux(cfg.socket)
	if err != nil {
		fatal(err)
	}
	reconnectPID, err := commandPID(reconnected)
	if err != nil {
		fatal(err)
	}
	_, reconnectErr := reconnected.Command("display-message", "-p", "#{version}")
	if err := reconnected.Close(); err != nil {
		fatal(err)
	}

	out := result{Version: strings.TrimSpace(version), ClientPID: pid, ReconnectPID: reconnectPID, ReconnectOK: reconnectErr == nil, PaneToggleOK: paneToggleOK, CommandErrorPropagated: invalidErr != nil, LargeOutputBytes: len(large)}
	if disableErr != nil {
		out.PaneDisableError = disableErr.Error()
	}
	if enableErr != nil {
		out.PaneEnableError = enableErr.Error()
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fatal(err)
	}
}

func commandPID(t *gotmuxcc.Tmux) (int, error) {
	raw, err := t.Command("display-message", "-p", "#{client_pid}")
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid client pid %q", raw)
	}
	return pid, nil
}

func touch(path string) error { return os.WriteFile(path, []byte("ready\n"), 0o600) }

func waitFile(path string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for %s", path)
		case <-ticker.C:
		}
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
