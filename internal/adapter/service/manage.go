package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Runner interface {
	Run(context.Context, string, ...string) error
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}
func Install(ctx context.Context, goos, home, executable, config string, runner Runner) error {
	var path, body string
	var err error
	switch goos {
	case "linux":
		path = filepath.Join(home, ".config", "systemd", "user", "shellbeam.service")
		body, err = SystemdUnit(executable, config)
	case "darwin":
		path = filepath.Join(home, "Library", "LaunchAgents", "com.shellbeam.daemon.plist")
		body, err = LaunchdPlist(executable, config)
	default:
		return fmt.Errorf("unsupported OS")
	}
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	rollback, err := writeReversible(path, []byte(body))
	if err != nil {
		return err
	}
	if goos == "linux" {
		if err = runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err == nil {
			err = runner.Run(ctx, "systemctl", "--user", "enable", "--now", "shellbeam.service")
		}
	} else {
		err = runner.Run(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), path)
	}
	if err != nil {
		_ = rollback()
		return err
	}
	return nil
}
func Uninstall(ctx context.Context, goos, home string, runner Runner) error {
	var path string
	if goos == "linux" {
		path = filepath.Join(home, ".config", "systemd", "user", "shellbeam.service")
		_ = runner.Run(ctx, "systemctl", "--user", "disable", "--now", "shellbeam.service")
	} else if goos == "darwin" {
		path = filepath.Join(home, "Library", "LaunchAgents", "com.shellbeam.daemon.plist")
		_ = runner.Run(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), path)
	} else {
		return fmt.Errorf("unsupported OS")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if goos == "linux" {
		_ = runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	}
	return nil
}

func writeReversible(path string, b []byte) (func() error, error) {
	old, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if info, e := os.Lstat(path); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("service path is symlink")
	}
	if err = atomicWrite(path, b); err != nil {
		return nil, err
	}
	return func() error {
		if existed {
			return atomicWrite(path, old)
		}
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}, nil
}
func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".shellbeam-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
