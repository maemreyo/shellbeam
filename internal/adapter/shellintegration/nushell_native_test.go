//go:build darwin

package shellintegration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func task9NativeTools(t *testing.T) (string, string) {
	t.Helper()
	nu := os.Getenv("SHELLBEAM_TASK9_NU")
	tmux := os.Getenv("SHELLBEAM_H0_TMUX")
	if nu == "" || tmux == "" {
		t.Skip("set SHELLBEAM_TASK9_NU and SHELLBEAM_H0_TMUX")
	}
	if !filepath.IsAbs(nu) || !filepath.IsAbs(tmux) {
		t.Fatal("native qualification paths must be absolute")
	}
	if out, err := exec.Command(nu, "--version").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "0.115.0" {
		t.Fatalf("qualified Nushell version: err=%v out=%q", err, out)
	}
	return nu, tmux
}

func TestNushellQualifiedDarwinNativePromptAndHelper(t *testing.T) {
	nu, tmux := task9NativeTools(t)
	root, err := os.MkdirTemp("/tmp", "sb-task9-nu-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home, work := filepath.Join(root, "home"), filepath.Join(root, "work")
	configDir := filepath.Join(home, "config", "nushell")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	userLog := filepath.Join(root, "user.log")
	config := "$env.config.show_banner = false\n$env.config.hooks.pre_prompt = [{|| \"user\" | save --append " + mustNushellLiteral(userLog) + " }]\n"
	configPath := filepath.Join(configDir, "config.nu")
	envPath := filepath.Join(configDir, "env.nu")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	beforeConfig, beforeEnv := mustReadTask9(t, configPath), mustReadTask9(t, envPath)

	socket, session := filepath.Join(root, "tmux.sock"), "task9-nushell"
	target := session + ":0.0"
	start := strings.Join([]string{"exec", "/usr/bin/env", "-i", shellQuote("HOME=" + home), shellQuote("XDG_CONFIG_HOME=" + filepath.Join(home, "config")), shellQuote("PATH=/usr/bin:/bin"), shellQuote("TERM=xterm-256color"), shellQuote(nu)}, " ")
	task9NativeTmux(t, tmux, socket, "new-session", "-d", "-s", session, "-c", work, start)
	t.Cleanup(func() { _ = exec.Command(tmux, "-S", socket, "kill-server").Run() })
	waitTask9NuPane(t, tmux, socket, target, work)
	waitTask9Path(t, userLog)
	userHookBefore, err := os.Stat(userLog)
	if err != nil {
		t.Fatal(err)
	}

	eventID := "evt_task9_native_nushell"
	ready := filepath.Join(root, "ready")
	req := task6WatchRequest(core.ShellNushell)
	install, cleanup := nushellScripts(req, eventID, nushellExternalInvocation("/usr/bin/touch", ready), nushellExternalInvocation("/usr/bin/false"))
	sendTask9Nu(t, tmux, socket, target, nushellWatcherDelivery(install, "null"))
	waitTask9PathGrowth(t, userLog, userHookBefore.Size())
	armed := "__shellbeam_handoff_" + eventID + "_armed"
	first := filepath.Join(root, "first")
	sendTask9Nu(t, tmux, socket, target, "("+mustNushellLiteral(armed)+" in $env) | save -f "+mustNushellLiteral(first))
	waitTask9Path(t, first)
	if trimTask9(t, first) != "true" {
		t.Fatal("installation prompt did not arm watcher")
	}
	if _, err := os.Stat(ready); !os.IsNotExist(err) {
		t.Fatal("installation prompt satisfied watcher")
	}
	canary := "SB_TASK9_NUSHELL_SECRET_7Q4M9K"
	sendTask9Nu(t, tmux, socket, target, "$env.CONTROL_PLANE_API_KEY = "+mustNushellLiteral(canary))
	waitTask9Path(t, ready)
	state := filepath.Join(root, "state")
	sendTask9Nu(t, tmux, socket, target, "$env.config.hooks.pre_prompt | length; ("+mustNushellLiteral(armed)+" in $env) | save -f "+mustNushellLiteral(state))
	waitTask9Path(t, state)
	if trimTask9(t, state) != "false" {
		t.Fatal("readiness sentinel lingered")
	}
	sendTask9Nu(t, tmux, socket, target, cleanup)

	assertTask9NativeNotifier(t, nu, root, req, eventID, canary)
	assertTask9NativeHelper(t, tmux, socket, target, work, root, canary)
	if string(mustReadTask9(t, configPath)) != string(beforeConfig) || string(mustReadTask9(t, envPath)) != string(beforeEnv) {
		t.Fatal("Nushell persistent config changed")
	}
	if len(strings.Fields(strings.TrimSpace(string(mustReadTask9(t, userLog))))) == 0 {
		t.Fatal("pre-existing user prompt hook was not preserved")
	}
}

func assertTask9NativeNotifier(t *testing.T, nu, root string, req app.WatchRequest, eventID, canary string) {
	t.Helper()
	envPath, argvPath := filepath.Join(root, "notify.env"), filepath.Join(root, "notify.argv")
	fake := filepath.Join(root, "fake notifier")
	body := "#!/bin/sh\n/usr/bin/env > " + shellQuote(envPath) + "\nprintf '%s\\n' \"$@\" > " + shellQuote(argvPath) + "\n"
	if err := os.WriteFile(fake, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	invocation := nushellNotifierInvocationForEvent(fake, filepath.Join(root, "notify.sock"), req, eventID, NotificationPromptBoundary, true)
	cmd := exec.Command(nu, "--no-config-file", "-c", invocation)
	cmd.Env = append(os.Environ(), "CONTROL_PLANE_API_KEY="+canary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("notifier: %v %s", err, out)
	}
	envText := string(mustReadTask9(t, envPath))
	if strings.Contains(envText, canary) || strings.Contains(envText, "CONTROL_PLANE_API_KEY=") {
		t.Fatal("private notifier inherited watched secret")
	}
	argv := strings.Split(strings.TrimSpace(string(mustReadTask9(t, argvPath))), "\n")
	if len(argv) < 2 || argv[0] != "__handoff_notify" || argv[len(argv)-2] != "--satisfied" || argv[len(argv)-1] != "true" {
		t.Fatalf("notifier argv=%q", argv)
	}
}

func assertTask9NativeHelper(t *testing.T, tmux, socket, target, work, root, canary string) {
	t.Helper()
	envPath, argvPath := filepath.Join(root, "helper.env"), filepath.Join(root, "helper.argv")
	cwdPath, pgidPath, tpgidPath := filepath.Join(root, "helper.cwd"), filepath.Join(root, "helper.pgid"), filepath.Join(root, "helper.tpgid")
	helper := filepath.Join(root, "fake helper")
	body := "#!/bin/sh\n/usr/bin/env > " + shellQuote(envPath) + "\nprintf '%s\\n' \"$@\" > " + shellQuote(argvPath) + "\npwd > " + shellQuote(cwdPath) + "\nps -o pgid= -p $$ > " + shellQuote(pgidPath) + "\nps -o tpgid= -p $$ > " + shellQuote(tpgidPath) + "\n"
	if err := os.WriteFile(helper, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	port := &recordingCommandPort{}
	adapter, err := NewNushellAdapter(Dependencies{Executable: helper, RuntimeDir: runtimeDir, Command: port})
	if err != nil {
		t.Fatal(err)
	}
	launchID := "launch_task9_native_nushell"
	if err := adapter.ArmContextHelper(t.Context(), app.ContextHelperArmSpec{Shell: core.ShellIdentity{Family: core.ShellNushell, RuntimeID: "runtime_task9_native"}, OpaqueLaunchID: launchID}); err != nil {
		t.Fatal(err)
	}
	sendTask9Nu(t, tmux, socket, target, port.snapshot()[0])
	for _, path := range []string{argvPath, envPath, cwdPath, pgidPath, tpgidPath} {
		waitTask9Path(t, path)
	}
	if got := strings.Fields(string(mustReadTask9(t, argvPath))); strings.Join(got, "|") != "__context_exec_helper|"+launchID {
		t.Fatalf("helper argv=%q", got)
	}
	envText := string(mustReadTask9(t, envPath))
	if strings.Count(envText, "CONTROL_PLANE_API_KEY="+canary) != 1 || strings.Count(envText, "SHELLBEAM_CONTEXT_EXEC_RUNTIME_DIR="+runtimeDir) != 1 {
		t.Fatal("helper environment mismatch")
	}
	gotInfo, err := os.Stat(strings.TrimSpace(string(mustReadTask9(t, cwdPath))))
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(work)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatal("helper cwd changed")
	}
	pgid := trimTask9(t, pgidPath)
	tpgid := trimTask9(t, tpgidPath)
	if pgid == "" || pgid != tpgid {
		t.Fatalf("helper was not foreground: pgid=%q tpgid=%q", pgid, tpgid)
	}
	hookState := filepath.Join(root, "helper-state")
	sendTask9Nu(t, tmux, socket, target, "(($env.config.hooks.pre_prompt | length) == 1) and (not (\"SHELLBEAM_CONTEXT_EXEC_RUNTIME_DIR\" in $env)) | save -f "+mustNushellLiteral(hookState))
	waitTask9Path(t, hookState)
	if trimTask9(t, hookState) != "true" {
		t.Fatal("helper hook/runtime env lingered")
	}
}

func waitTask9NuPane(t *testing.T, tmux, socket, target, work string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command(tmux, "-S", socket, "display-message", "-p", "-t", target, "#{pane_current_command}|#{pane_pid}|#{pane_current_path}").CombinedOutput()
		if err == nil {
			p := strings.Split(strings.TrimSpace(string(out)), "|")
			if len(p) == 3 && p[0] == "nu" {
				pid, _ := strconv.Atoi(p[1])
				proc, _ := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "comm=").CombinedOutput()
				a, e1 := os.Stat(p[2])
				b, e2 := os.Stat(work)
				if pid > 1 && filepath.Base(strings.TrimSpace(string(proc))) == "nu" && e1 == nil && e2 == nil && os.SameFile(a, b) {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("Nushell pane facts not current")
}
func sendTask9Nu(t *testing.T, tmux, socket, target, script string) {
	t.Helper()
	task9NativeTmux(t, tmux, socket, "send-keys", "-l", "-t", target, script)
	task9NativeTmux(t, tmux, socket, "send-keys", "-t", target, "Enter")
}
func task9NativeTmux(t *testing.T, tmux, socket string, args ...string) {
	t.Helper()
	full := append([]string{"-S", socket}, args...)
	out, err := exec.Command(tmux, full...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %q: %v %s", args, err, out)
	}
}
func waitTask9Path(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", filepath.Base(path))
}
func waitTask9PathGrowth(t *testing.T, path string, before int64) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > before {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s growth", filepath.Base(path))
}

func trimTask9(t *testing.T, path string) string {
	t.Helper()
	return strings.TrimSpace(string(mustReadTask9(t, path)))
}
func mustReadTask9(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
