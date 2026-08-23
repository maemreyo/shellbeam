package shellintegration

import (
	"context"
	"strings"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestNushellAdapterUsesComposablePrePromptHookAndExactRemoval(t *testing.T) {
	port := &recordingCommandPort{}
	adapter, err := NewNushellAdapter(task6Deps(t, port))
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := adapter.Install(t.Context(), task6WatchRequest(core.ShellNushell))
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	scripts := port.snapshot()
	if len(scripts) != 2 {
		t.Fatalf("scripts=%#v", scripts)
	}
	install := scripts[0]
	assertMinimalNotifierInvocation(t, install)
	for _, want := range []string{
		"$env.config.hooks.pre_prompt = ($env.config.hooks.pre_prompt | append $__shellbeam_hook)",
		"view source $h",
		"$env.CONTROL_PLANE_API_KEY?",
		"hook_installed",
		"__handoff_notify",
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("nushell install missing %q: %s", want, install)
		}
	}
	for _, forbidden := range []string{"eval ", "precmd_functions", "PROMPT_COMMAND", "fish_prompt", "config nu", "config.nu", "env.nu", "CONTROL_PLANE_API_KEY="} {
		if strings.Contains(install, forbidden) {
			t.Fatalf("nushell install contains foreign/persistent syntax %q: %s", forbidden, install)
		}
	}
	cleanup := scripts[1]
	if !strings.Contains(cleanup, "$env.config.hooks.pre_prompt") || !strings.Contains(cleanup, "view source $h") || !strings.Contains(cleanup, "hide-env") {
		t.Fatalf("nushell cleanup is not exact session-local removal: %s", cleanup)
	}
}

func TestNushellContextHelperArmIsOneShotScopedAndNuQuoted(t *testing.T) {
	port := &recordingCommandPort{}
	executable := `/tmp/Shell Beam's "quoted"\bin/shellbeam`
	deps := task5LaunchDeps(t, port, executable)
	adapter, err := NewNushellAdapter(deps)
	if err != nil {
		t.Fatal(err)
	}
	arm := app.ContextHelperArmSpec{
		Shell:          core.ShellIdentity{Family: core.ShellNushell, RuntimeID: "runtime_nushell_task9"},
		OpaqueLaunchID: "launch_task9_nushell_01",
	}
	if err := adapter.ArmContextHelper(context.Background(), arm); err != nil {
		t.Fatal(err)
	}
	scripts := port.snapshot()
	if len(scripts) != 1 {
		t.Fatalf("scripts=%#v", scripts)
	}
	script := scripts[0]
	helperAt := strings.Index(script, "__context_exec_helper")
	removalAt := strings.Index(script, "view source $h")
	if helperAt < 0 || removalAt < 0 || removalAt > helperAt {
		t.Fatalf("nushell hook does not self-remove before helper: %s", script)
	}
	for _, want := range []string{"with-env", "SHELLBEAM_CONTEXT_EXEC_RUNTIME_DIR", "launch_task9_nushell_01", `^"/tmp/Shell Beam's \"quoted\"\\bin/shellbeam"`} {
		if !strings.Contains(script, want) {
			t.Fatalf("nushell helper hook missing %q: %s", want, script)
		}
	}
	for _, forbidden := range []string{"__handoff_notify", "--socket", "--claim", "ctxexec_", " --command", "eval "} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("nushell helper hook contains forbidden surface %q: %s", forbidden, script)
		}
	}
}

func TestNushellAdapterRejectsForeignShellBeforeWrite(t *testing.T) {
	port := &recordingCommandPort{}
	adapter, err := NewNushellAdapter(task6Deps(t, port))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Install(t.Context(), task6WatchRequest(core.ShellFish)); err == nil {
		t.Fatal("nushell adapter accepted fish watcher")
	}
	if len(port.snapshot()) != 0 {
		t.Fatalf("foreign shell mutated session: %#v", port.snapshot())
	}
}

func TestNushellLiteralEscapesControlWhitespaceWithoutChangingValue(t *testing.T) {
	got, err := nushellLiteral("a\tb\nc\rd")
	if err != nil {
		t.Fatal(err)
	}
	want := "\"a\\tb\\nc\\rd\""
	if got != want {
		t.Fatalf("literal=%q want=%q", got, want)
	}
}

func TestNushellLiteralRejectsOtherControlCharacters(t *testing.T) {
	if _, err := nushellLiteral("a\x01b"); err == nil {
		t.Fatal("Nushell literal accepted unsupported control byte")
	}
}
