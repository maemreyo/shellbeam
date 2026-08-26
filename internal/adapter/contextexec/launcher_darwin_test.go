//go:build darwin

package contextexec

import (
	"errors"
	"io"
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
	prepared, err := launcher.Prepare(ChildSpec{
		Argv: []string{"/bin/sh", "-c", "printf qualified > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	child, err := prepared.Start()
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
	if !ExecutableMatches(prepared.ResolvedExecutable(), resolved) || !ExecutableMatches(child.ResolvedExecutable, resolved) {
		t.Fatalf("resolved prepared=%q child=%q want=%q", prepared.ResolvedExecutable(), child.ResolvedExecutable, resolved)
	}
}

func TestDarwinPlatformLauncherOutputRemainsReadableAfterWait(t *testing.T) {
	for i := 0; i < 20; i++ {
		launcher := NewPlatformLauncher()
		prepared, err := launcher.Prepare(ChildSpec{Argv: []string{"/bin/echo", "owned-output"}, CWD: t.TempDir(), Env: os.Environ()})
		if err != nil {
			t.Fatal(err)
		}
		child, err := prepared.Start()
		if err != nil {
			_ = prepared.Close()
			t.Fatal(err)
		}
		type waitResult struct {
			exit ChildExit
			err  error
		}
		waited := make(chan waitResult, 1)
		go func() {
			exit, err := child.Wait()
			waited <- waitResult{exit: exit, err: err}
		}()
		var result waitResult
		select {
		case result = <-waited:
		case <-time.After(2 * time.Second):
			if child.KillGroup != nil {
				_ = child.KillGroup()
			}
			result = <-waited
			_ = prepared.Close()
			t.Fatalf("qualified child remained stopped after detach on iteration %d", i)
		}
		if result.err != nil {
			_ = prepared.Close()
			t.Fatal(result.err)
		}
		if !result.exit.Reaped {
			_ = prepared.Close()
			t.Fatalf("exit=%#v", result.exit)
		}
		data, err := io.ReadAll(child.Stdout)
		if err != nil {
			_ = prepared.Close()
			t.Fatalf("stdout after wait: %v", err)
		}
		if string(data) != "owned-output\n" {
			_ = prepared.Close()
			t.Fatalf("stdout=%q", data)
		}
		if err := prepared.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDarwinPlatformLauncherRejectsPathReplacementAfterPrepareBeforeMaliciousFirstInstruction(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	replacement := filepath.Join(dir, "replacement")
	copyDarwinExecutable(t, "/bin/echo", target)
	copyDarwinExecutable(t, "/bin/sh", replacement)
	marker := filepath.Join(dir, "malicious-ran")

	launcher := NewPlatformLauncher()
	prepared, err := launcher.Prepare(ChildSpec{
		Argv: []string{target, "-c", "printf pwned > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := os.Rename(replacement, target); err != nil {
		t.Fatal(err)
	}
	child, err := prepared.Start()
	if err == nil {
		if child != nil && child.KillGroup != nil {
			_ = child.KillGroup()
		}
		if child != nil && child.Wait != nil {
			_, _ = child.Wait()
		}
		t.Fatal("path replacement after Prepare was accepted")
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
		verifyMappedExecutable: func(pid int, expected darwinExecutableIdentity) error {
			if err := os.Rename(originalLink, target); err != nil {
				return err
			}
			return verifyDarwinMappedExecutable(pid, expected)
		},
	}
	prepared, err := launcher.Prepare(ChildSpec{
		Argv: []string{target, "-c", "printf pwned > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := os.Rename(replacement, target); err != nil {
		t.Fatal(err)
	}
	child, err := prepared.Start()
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
	prepared, err := launcher.Prepare(ChildSpec{
		Argv: []string{"/bin/sh", "-c", "printf unsafe > " + marker},
		CWD:  dir,
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	child, err := prepared.Start()
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
	if _, err := launcher.Prepare(ChildSpec{Argv: []string{script}, CWD: dir, Env: os.Environ()}); err == nil {
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
