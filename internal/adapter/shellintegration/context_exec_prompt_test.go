package shellintegration

import (
	"context"
	"reflect"
	"strings"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestContextHelperArmInstallsOneShotHookWithOnlyFixedHelperInvocation(t *testing.T) {
	for _, family := range []core.ShellFamily{core.ShellFish, core.ShellZsh, core.ShellBash} {
		t.Run(string(family), func(t *testing.T) {
			port := &recordingCommandPort{}
			deps := task5LaunchDeps(t, port, "/tmp/Shell Beam's/bin/shellbeam")
			adapter := task5ArmAdapter(t, family, deps)
			arm := app.ContextHelperArmSpec{
				Shell:          core.ShellIdentity{Family: family, RuntimeID: "runtime_task5a"},
				OpaqueLaunchID: "launch_task5a_01",
			}
			if err := adapter.ArmContextHelper(context.Background(), arm); err != nil {
				t.Fatal(err)
			}
			scripts := port.snapshot()
			if len(scripts) != 1 {
				t.Fatalf("scripts=%#v", scripts)
			}
			script := scripts[0]
			invocation := contextHelperInvocation(deps.Executable, arm.OpaqueLaunchID)
			helperAt := strings.Index(script, invocation)
			if helperAt < 0 || !contextHelperSelfRemovalPrecedes(family, script[:helperAt]) {
				t.Fatalf("hook does not self-remove before fixed helper launch: %s", script)
			}
			for _, forbidden := range []string{"__handoff_notify", "--socket", "--claim", "ctxexec_", "H5_SECRET", " --command"} {
				if strings.Contains(script, forbidden) {
					t.Fatalf("one-shot hook contains forbidden surface %q: %s", forbidden, script)
				}
			}
		})
	}
}

func TestContextHelperAdaptersNoLongerExposeProofThenLaunchAPI(t *testing.T) {
	for _, adapter := range []any{&FishAdapter{}, &ZshAdapter{}, &BashAdapter{}} {
		typ := reflect.TypeOf(adapter)
		if _, ok := typ.MethodByName("LaunchContextHelper"); ok {
			t.Fatalf("%s still exposes LaunchContextHelper", typ)
		}
	}
}

func TestContextHelperArmRejectsUnsafeOpaqueIDBeforeShellWrite(t *testing.T) {
	port := &recordingCommandPort{}
	adapter := task5ArmAdapter(t, core.ShellFish, task5LaunchDeps(t, port, "/opt/shellbeam/bin/shellbeam"))
	err := adapter.ArmContextHelper(context.Background(), app.ContextHelperArmSpec{
		Shell:          core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_task5a"},
		OpaqueLaunchID: "launch_safe; touch /tmp/pwned",
	})
	if err == nil {
		t.Fatal("unsafe launch id accepted")
	}
	if len(port.snapshot()) != 0 {
		t.Fatalf("unsafe arm mutated shell: %#v", port.snapshot())
	}
}

func contextHelperSelfRemovalPrecedes(family core.ShellFamily, prefix string) bool {
	switch family {
	case core.ShellFish:
		return strings.Contains(prefix, "functions --erase ")
	case core.ShellZsh:
		return strings.Contains(prefix, "precmd_functions=(") && strings.Contains(prefix, "unfunction ")
	case core.ShellBash:
		return strings.Contains(prefix, "PROMPT_COMMAND=") && strings.Contains(prefix, "unset -f ")
	default:
		return false
	}
}

func task5ArmAdapter(t *testing.T, family core.ShellFamily, deps Dependencies) app.ContextHelperArmer {
	t.Helper()
	switch family {
	case core.ShellFish:
		adapter, err := NewFishAdapter(deps)
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	case core.ShellZsh:
		adapter, err := NewZshAdapter(deps)
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	case core.ShellBash:
		adapter, err := NewBashAdapter(deps)
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	default:
		t.Fatalf("unsupported family %q", family)
		return nil
	}
}
