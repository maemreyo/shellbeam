package shellintegration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestBashNativeNonExportedVariableIsNotSatisfied(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "result")
	req := task6WatchRequest(core.ShellBash)
	install, _ := bashScripts(req, "evt_native_bash", nativeMarker(marker, true), nativeMarker(marker, false))
	script := "unset CONTROL_PLANE_API_KEY\nCONTROL_PLANE_API_KEY=task6_native_secret\n" + install + "\neval \"$PROMPT_COMMAND\"\neval \"$PROMPT_COMMAND\"\n"
	runNativeShell(t, "/bin/bash", []string{"--noprofile", "--norc", "-c", script})
	assertNativeMarkerAbsent(t, marker)
}

func TestZshNativeNonExportedVariableIsNotSatisfied(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "result")
	req := task6WatchRequest(core.ShellZsh)
	install, _ := zshScripts(req, "evt_native_zsh", nativeMarker(marker, true), nativeMarker(marker, false))
	script := "unset CONTROL_PLANE_API_KEY\nCONTROL_PLANE_API_KEY=task6_native_secret\n" + install + "\n__shellbeam_handoff_evt_native_zsh\n__shellbeam_handoff_evt_native_zsh\n"
	runNativeShell(t, "/bin/zsh", []string{"-f", "-c", script})
	assertNativeMarkerAbsent(t, marker)
}

func nativeMarker(path string, value bool) string {
	word := "false"
	if value {
		word = "true"
	}
	return "printf '%s\\n' " + shellQuote(word) + " > " + shellQuote(path)
}

func runNativeShell(t *testing.T, path string, args []string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("native shell unavailable: %s", path)
	}
	cmd := exec.Command(path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native shell %s failed: %v\n%s", path, err, out)
	}
}

func assertNativeMarker(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("marker=%q want=%q", got, want)
	}
}

func assertNativeMarkerAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected readiness notification marker %q: %v", path, err)
	}
}

func TestZshNativeHookCompositionDoesNotDependOnFpath(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "result")
	hooks := filepath.Join(root, "hooks")
	user := filepath.Join(root, "user")
	req := task6WatchRequest(core.ShellZsh)
	install, _ := zshScripts(req, "evt_native_fpath", nativeMarker(marker, true), nativeMarker(marker, false))
	script := "set -e\nfpath=(/definitely/missing)\ntypeset -ga precmd_functions\nfunction user_pre() { printf '%s\\n' user >> " + shellQuote(user) + "; }\nprecmd_functions=(user_pre)\nexport CONTROL_PLANE_API_KEY=task6_native_secret\n" + install + "\n__shellbeam_handoff_evt_native_fpath\n__shellbeam_handoff_evt_native_fpath\nprintf '%s\\n' \"${precmd_functions[@]}\" > " + shellQuote(hooks) + "\nuser_pre\n"
	runNativeShell(t, "/bin/zsh", []string{"-f", "-c", script})
	assertNativeMarker(t, marker, "true")
	data, err := os.ReadFile(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "user_pre" {
		t.Fatalf("precmd_functions=%q", got)
	}
	assertNativeMarker(t, user, "user")
}

type nativeShellCase struct {
	name   string
	family core.ShellFamily
	path   string
	args   func(string) []string
	setup  func(string) string
	body   func(root, install, eventID string) string
}

func TestNativeShellPredicateMatrixPreservesExistingHooks(t *testing.T) {
	states := []struct {
		name      string
		satisfied bool
	}{
		{name: "unset"},
		{name: "empty"},
		{name: "nonexport"},
		{name: "exported", satisfied: true},
	}
	for _, shell := range nativeShellCases() {
		shell := shell
		t.Run(shell.name, func(t *testing.T) {
			logNativeShellVersion(t, shell.path)
			for _, state := range states {
				state := state
				t.Run(state.name, func(t *testing.T) {
					root := t.TempDir()
					result := filepath.Join(root, "result")
					eventID := "evt_matrix_" + shell.name + "_" + state.name
					req := task6WatchRequest(shell.family)
					install, _ := nativeShellScripts(req, eventID, nativeAppendMarker(result, true), nativeAppendMarker(result, false))
					script := shell.setup(state.name) + "\n" + shell.body(root, install, eventID)
					runNativeShell(t, shell.path, shell.args(script))
					if state.satisfied {
						assertMarkerLines(t, result, []string{"true"})
					} else {
						assertNativeMarkerAbsent(t, result)
					}
					wantUser := []string{"user", "user"}
					if shell.family == core.ShellBash {
						wantUser = []string{"user", "user", "user"}
					}
					assertMarkerLines(t, filepath.Join(root, "user"), wantUser)
				})
			}
		})
	}
}

func TestNativeNotifierInvocationDoesNotInheritWatchedSecret(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "sb-hn-native-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	envCapture := filepath.Join(root, "env")
	argvCapture := filepath.Join(root, "argv")
	fake := filepath.Join(root, "shellbeam")
	content := "#!/bin/sh\n/usr/bin/env > " + shellQuote(envCapture) + "\nprintf '%s\\n' \"$@\" > " + shellQuote(argvCapture) + "\n"
	if err := os.WriteFile(fake, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	req := task6WatchRequest(core.ShellBash)
	invocation := notifierInvocation(fake, filepath.Join(root, "notify.sock"), req, "evt_native_env", true)
	secret := "task6-native-secret-canary-K9P4"
	cmd := exec.Command("/bin/bash", "--noprofile", "--norc", "-c", invocation)
	cmd.Env = append(os.Environ(), "CONTROL_PLANE_API_KEY="+secret, "UNRELATED_SECRET="+secret)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("notifier invocation failed: %v\n%s", err, out)
	}
	for _, path := range []string{envCapture, argvCapture} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, secret) || strings.Contains(text, "CONTROL_PLANE_API_KEY") || strings.Contains(text, "UNRELATED_SECRET") {
			t.Fatalf("secret-bearing parent environment leaked into %s: %s", filepath.Base(path), text)
		}
	}
}

func TestNativeShellHundredInstallRemoveCyclesPreserveUserHooks(t *testing.T) {
	for _, shell := range nativeShellCases() {
		shell := shell
		t.Run(shell.name, func(t *testing.T) {
			root := t.TempDir()
			var b strings.Builder
			b.WriteString(nativeCyclePrelude(shell, root))
			for i := 0; i < 100; i++ {
				eventID := fmt.Sprintf("evt_cycle_%s_%03d", shell.name, i)
				req := task6WatchRequest(shell.family)
				install, cleanup := nativeShellScripts(req, eventID, ":", ":")
				b.WriteString("\n")
				b.WriteString(install)
				b.WriteString("\n")
				b.WriteString(cleanup)
				b.WriteString("\n")
			}
			b.WriteString(nativeCycleEpilogue(shell, root))
			runNativeShell(t, shell.path, shell.args(b.String()))
			assertMarkerLines(t, filepath.Join(root, "user"), []string{"user"})
		})
	}
}

func nativeShellCases() []nativeShellCase {
	return []nativeShellCase{
		{
			name: "fish", family: core.ShellFish, path: "/opt/homebrew/bin/fish",
			args:  func(script string) []string { return []string{"-c", script} },
			setup: func(state string) string { return fishStateSetup(state) },
			body: func(root, install, eventID string) string {
				user := shellQuote(filepath.Join(root, "user"))
				return "function user_prompt --on-event fish_prompt\n  printf '%s\\n' user >> " + user + "\nend\n" + install + "\nemit fish_prompt\nemit fish_prompt\n"
			},
		},
		{
			name: "zsh", family: core.ShellZsh, path: "/bin/zsh",
			args:  func(script string) []string { return []string{"-f", "-c", script} },
			setup: func(state string) string { return bourneStateSetup(state) },
			body: func(root, install, eventID string) string {
				user := shellQuote(filepath.Join(root, "user"))
				before := shellQuote(filepath.Join(root, "before_hooks"))
				after := shellQuote(filepath.Join(root, "after_hooks"))
				name := "__shellbeam_handoff_" + eventID
				return "fpath=(/definitely/missing)\ntypeset -ga precmd_functions\nfunction user_pre() { printf '%s\\n' user >> " + user + "; }\nprecmd_functions=(user_pre)\n" + install + "\nprintf '%s\\n' \"${precmd_functions[@]}\" > " + before + "\nuser_pre\n" + name + "\n" + name + "\nprintf '%s\\n' \"${precmd_functions[@]}\" > " + after + "\nuser_pre\n" +
					"grep -qx user_pre " + after + "\ngrep -q " + shellQuote(name) + " " + before + "\n"
			},
		},
		{
			name: "bash", family: core.ShellBash, path: "/bin/bash",
			args:  func(script string) []string { return []string{"--noprofile", "--norc", "-c", script} },
			setup: func(state string) string { return bourneStateSetup(state) },
			body: func(root, install, eventID string) string {
				user := shellQuote(filepath.Join(root, "user"))
				name := "__shellbeam_handoff_" + eventID
				after := shellQuote(filepath.Join(root, "after_hooks"))
				return "set -e\nuser_hook(){ printf '%s\\n' user >> " + user + "; }\nPROMPT_COMMAND=user_hook\n" + install + "\ncase \"$PROMPT_COMMAND\" in *" + name + "*) : ;; *) exit 91 ;; esac\neval \"$PROMPT_COMMAND\"\ncase \"$PROMPT_COMMAND\" in *" + name + "*) : ;; *) exit 92 ;; esac\neval \"$PROMPT_COMMAND\"\nprintf '%s\\n' \"$PROMPT_COMMAND\" > " + after + "\nuser_hook\n"
			},
		},
	}
}

func nativeShellScripts(req app.WatchRequest, eventID, trueNotify, falseNotify string) (string, string) {
	switch req.Shell.Family {
	case core.ShellFish:
		return fishScripts(req, eventID, trueNotify, falseNotify)
	case core.ShellZsh:
		return zshScripts(req, eventID, trueNotify, falseNotify)
	case core.ShellBash:
		return bashScripts(req, eventID, trueNotify, falseNotify)
	default:
		panic("unsupported native shell test family")
	}
}

func fishStateSetup(state string) string {
	switch state {
	case "unset":
		return "set -e CONTROL_PLANE_API_KEY"
	case "empty":
		return "set -e CONTROL_PLANE_API_KEY; set -gx CONTROL_PLANE_API_KEY ''"
	case "nonexport":
		return "set -e CONTROL_PLANE_API_KEY; set -g CONTROL_PLANE_API_KEY task6_native_secret"
	case "exported":
		return "set -e CONTROL_PLANE_API_KEY; set -gx CONTROL_PLANE_API_KEY task6_native_secret"
	default:
		panic("bad state")
	}
}

func bourneStateSetup(state string) string {
	switch state {
	case "unset":
		return "unset CONTROL_PLANE_API_KEY"
	case "empty":
		return "unset CONTROL_PLANE_API_KEY; export CONTROL_PLANE_API_KEY=''"
	case "nonexport":
		return "unset CONTROL_PLANE_API_KEY; CONTROL_PLANE_API_KEY=task6_native_secret"
	case "exported":
		return "unset CONTROL_PLANE_API_KEY; export CONTROL_PLANE_API_KEY=task6_native_secret"
	default:
		panic("bad state")
	}
}

func nativeAppendMarker(path string, value bool) string {
	word := "false"
	if value {
		word = "true"
	}
	return "printf '%s\\n' " + shellQuote(word) + " >> " + shellQuote(path)
}

func assertMarkerLines(t *testing.T, path string, want []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(data))
	var got []string
	if text != "" {
		got = strings.Split(text, "\n")
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s lines=%v want=%v", filepath.Base(path), got, want)
	}
}

func logNativeShellVersion(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err == nil {
		t.Log(strings.TrimSpace(string(out)))
	}
}

func nativeCyclePrelude(shell nativeShellCase, root string) string {
	user := shellQuote(filepath.Join(root, "user"))
	switch shell.family {
	case core.ShellFish:
		return "function user_prompt --on-event fish_prompt\n  printf '%s\\n' user >> " + user + "\nend"
	case core.ShellZsh:
		return "fpath=(/definitely/missing)\ntypeset -ga precmd_functions\nfunction user_pre() { printf '%s\\n' user >> " + user + "; }\nprecmd_functions=(user_pre)"
	case core.ShellBash:
		return "user_hook(){ printf '%s\\n' user >> " + user + "; }\nPROMPT_COMMAND=user_hook"
	default:
		panic("bad shell")
	}
}

func nativeCycleEpilogue(shell nativeShellCase, root string) string {
	switch shell.family {
	case core.ShellFish:
		return "\nemit fish_prompt\n"
	case core.ShellZsh:
		return "\n[ \"${#precmd_functions[@]}\" -eq 1 ] && [ \"${precmd_functions[1]}\" = user_pre ]\nuser_pre\n"
	case core.ShellBash:
		return "\n[ \"$PROMPT_COMMAND\" = user_hook ]\neval \"$PROMPT_COMMAND\"\n"
	default:
		panic("bad shell")
	}
}

func TestNativeShellWatcherSkipsInstallationPromptThenEvaluatesNextPrompt(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		family     core.ShellFamily
		args       func(string) []string
		body       func(marker, install, eventID string) string
	}{
		{
			name: "fish", path: "/opt/homebrew/bin/fish", family: core.ShellFish,
			args: func(script string) []string { return []string{"-c", script} },
			body: func(marker, install, eventID string) string {
				return "set -e CONTROL_PLANE_API_KEY\n" + install +
					"\nemit fish_prompt\ntest ! -e " + shellQuote(marker) +
					"\nset -gx CONTROL_PLANE_API_KEY task7_secret\nemit fish_prompt\n"
			},
		},
		{
			name: "zsh", path: "/bin/zsh", family: core.ShellZsh,
			args: func(script string) []string { return []string{"-f", "-c", script} },
			body: func(marker, install, eventID string) string {
				name := "__shellbeam_handoff_" + eventID
				return "unset CONTROL_PLANE_API_KEY\n" + install +
					"\n" + name + "\n[[ ! -e " + shellQuote(marker) + " ]]" +
					"\nexport CONTROL_PLANE_API_KEY=task7_secret\n" + name + "\n"
			},
		},
		{
			name: "bash", path: "/bin/bash", family: core.ShellBash,
			args: func(script string) []string { return []string{"--noprofile", "--norc", "-c", script} },
			body: func(marker, install, eventID string) string {
				return "unset CONTROL_PLANE_API_KEY\n" + install +
					"\neval \"$PROMPT_COMMAND\"\ntest ! -e " + shellQuote(marker) +
					"\nexport CONTROL_PLANE_API_KEY=task7_secret\neval \"$PROMPT_COMMAND\"\n"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "result")
			eventID := "evt_skip_install_" + tc.name
			req := task6WatchRequest(tc.family)
			install, _ := nativeShellScripts(req, eventID, nativeMarker(marker, true), nativeMarker(marker, false))
			runNativeShell(t, tc.path, tc.args(tc.body(marker, install, eventID)))
			assertNativeMarker(t, marker, "true")
		})
	}
}

func TestNativeShellWatcherIgnoresUnsatisfiedPromptsUntilRequirementBecomesTrue(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		family     core.ShellFamily
		args       func(string) []string
		body       func(marker, install, eventID string) string
	}{
		{
			name: "fish", path: "/opt/homebrew/bin/fish", family: core.ShellFish,
			args: func(script string) []string { return []string{"-c", script} },
			body: func(marker, install, eventID string) string {
				return "set -e CONTROL_PLANE_API_KEY\n" + install +
					"\nemit fish_prompt\nemit fish_prompt\ntest ! -e " + shellQuote(marker) +
					"\nset -gx CONTROL_PLANE_API_KEY task8_secret\nemit fish_prompt\n"
			},
		},
		{
			name: "zsh", path: "/bin/zsh", family: core.ShellZsh,
			args: func(script string) []string { return []string{"-f", "-c", script} },
			body: func(marker, install, eventID string) string {
				name := "__shellbeam_handoff_" + eventID
				return "unset CONTROL_PLANE_API_KEY\n" + install +
					"\n" + name + "\n" + name + "\n[[ ! -e " + shellQuote(marker) + " ]]" +
					"\nexport CONTROL_PLANE_API_KEY=task8_secret\n" + name + "\n"
			},
		},
		{
			name: "bash", path: "/bin/bash", family: core.ShellBash,
			args: func(script string) []string { return []string{"--noprofile", "--norc", "-c", script} },
			body: func(marker, install, eventID string) string {
				return "unset CONTROL_PLANE_API_KEY\n" + install +
					"\neval \"$PROMPT_COMMAND\"\neval \"$PROMPT_COMMAND\"\ntest ! -e " + shellQuote(marker) +
					"\nexport CONTROL_PLANE_API_KEY=task8_secret\neval \"$PROMPT_COMMAND\"\n"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "result")
			eventID := "evt_wait_until_satisfied_" + tc.name
			req := task6WatchRequest(tc.family)
			install, _ := nativeShellScripts(req, eventID, nativeMarker(marker, true), nativeMarker(marker, false))
			runNativeShell(t, tc.path, tc.args(tc.body(marker, install, eventID)))
			assertNativeMarker(t, marker, "true")
		})
	}
}
