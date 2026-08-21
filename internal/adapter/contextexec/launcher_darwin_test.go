//go:build darwin

package contextexec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDarwinPlatformLauncherExecutesOnlyAfterMappedExecutableIdentityMatches(t *testing.T) {
	launcher := NewPlatformLauncher()
	if !launcher.Qualified() {
		t.Fatal("Darwin platform-equivalent executor did not qualify")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	child, err := launcher.Launch(ChildSpec{
		Argv: []string{"/bin/sh", "-c", "printf qualified > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatal(err)
	}
	exit, err := child.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !exit.Reaped || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit=%#v", exit)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("child never reached first instruction after qualification: %v", err)
	}
	resolved, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if !ExecutableMatches(child.ResolvedExecutable, resolved) {
		t.Fatalf("resolved executable=%q want %q", child.ResolvedExecutable, resolved)
	}
}

func TestDarwinPlatformLauncherRejectsPathReplacementBeforeMaliciousFirstInstruction(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	replacement := filepath.Join(dir, "replacement")
	copyDarwinExecutable(t, "/bin/echo", target)
	copyDarwinExecutable(t, "/bin/sh", replacement)
	marker := filepath.Join(dir, "malicious-ran")

	launcher := darwinPlatformLauncher{
		afterOpen: func(string) error {
			return os.Rename(replacement, target)
		},
	}
	child, err := launcher.Launch(ChildSpec{
		Argv: []string{target, "-c", "printf pwned > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err == nil {
		if child != nil && child.KillGroup != nil {
			_ = child.KillGroup()
		}
		if child != nil && child.Wait != nil {
			_, _ = child.Wait()
		}
		t.Fatal("path replacement was accepted")
	}
	time.Sleep(50 * time.Millisecond)
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("malicious replacement executed before rejection: %v", statErr)
	}
}

func TestDarwinPlatformLauncherRejectsReplacementEvenWhenPathIsRestoredBeforeVerification(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	originalLink := filepath.Join(dir, "original-link")
	replacement := filepath.Join(dir, "replacement")
	copyDarwinExecutable(t, "/bin/echo", target)
	if err := os.Link(target, originalLink); err != nil {
		t.Fatal(err)
	}
	copyDarwinExecutable(t, "/bin/sh", replacement)
	marker := filepath.Join(dir, "malicious-ran")

	launcher := darwinPlatformLauncher{
		afterOpen: func(string) error {
			return os.Rename(replacement, target)
		},
		verifyMappedExecutable: func(pid int, expected darwinExecutableIdentity) error {
			if err := os.Rename(originalLink, target); err != nil {
				return err
			}
			return verifyDarwinMappedExecutable(pid, expected)
		},
	}
	child, err := launcher.Launch(ChildSpec{
		Argv: []string{target, "-c", "printf pwned > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err == nil {
		if child != nil && child.KillGroup != nil {
			_ = child.KillGroup()
		}
		if child != nil && child.Wait != nil {
			_, _ = child.Wait()
		}
		t.Fatal("path-restored replacement was accepted")
	}
	time.Sleep(50 * time.Millisecond)
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("malicious replacement executed before vnode rejection: %v", statErr)
	}
}

func TestDarwinPlatformLauncherFailsClosedWhenMappedIdentityCannotBeObserved(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	launcher := darwinPlatformLauncher{
		verifyMappedExecutable: func(int, darwinExecutableIdentity) error {
			return errors.New("proc info unavailable")
		},
	}
	child, err := launcher.Launch(ChildSpec{
		Argv: []string{"/bin/sh", "-c", "printf unsafe > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err == nil {
		if child != nil && child.KillGroup != nil {
			_ = child.KillGroup()
		}
		if child != nil && child.Wait != nil {
			_, _ = child.Wait()
		}
		t.Fatal("unobservable executable identity was accepted")
	}
	time.Sleep(50 * time.Millisecond)
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("child executed despite unobservable identity: %v", statErr)
	}
}

func TestDarwinPlatformLauncherRejectsShebangUntilInterpreterChainIsQualified(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "script-ran")
	script := filepath.Join(dir, "script")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf script > "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := NewPlatformLauncher()
	if _, err := launcher.Launch(ChildSpec{Argv: []string{script}, CWD: dir, Env: os.Environ()}); err == nil {
		t.Fatal("unqualified interpreter chain accepted")
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("script executed despite fail-closed policy: %v", statErr)
	}
}

func copyDarwinExecutable(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
}
