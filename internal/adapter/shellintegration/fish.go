package shellintegration

import (
	"context"
	"fmt"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type FishAdapter struct{ deps Dependencies }

func NewFishAdapter(deps Dependencies) (*FishAdapter, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	return &FishAdapter{deps: deps}, nil
}

func (*FishAdapter) Family() core.ShellFamily { return core.ShellFish }

func (a *FishAdapter) Install(_ context.Context, req app.WatchRequest) (app.RequirementWatcher, error) {
	if req.Shell.Family != core.ShellFish {
		return nil, fmt.Errorf("fish adapter shell mismatch")
	}
	watcher, _, err := newOneShotWatcher(req, a.deps, fishScripts)
	return watcher, err
}

func fishScripts(req app.WatchRequest, eventID, trueNotify, falseNotify string) (string, string) {
	name := "__shellbeam_handoff_" + eventID
	armed := name + "_armed"
	variable := req.Requirement.Name
	install := fmt.Sprintf("set -g %s 0\nfunction %s --on-event fish_prompt\n  if test \"$%s\" = 0\n    set -g %s 1\n    return\n  end\n  if set --query --export %s; and test -n \"$%s\"\n    %s\n  else\n    %s\n  end\n  functions --erase %s\n  set -e %s\nend", armed, name, armed, armed, variable, variable, trueNotify, falseNotify, name, armed)
	cleanup := fmt.Sprintf("functions --erase %s 2>/dev/null; or true; set -e %s 2>/dev/null; or true", name, armed)
	return install, cleanup
}

func (a *FishAdapter) ArmContextHelper(ctx context.Context, arm app.ContextHelperArmSpec) error {
	if a == nil {
		return fmt.Errorf("fish context helper armer unavailable")
	}
	return armContextHelper(ctx, a.deps, core.ShellFish, arm, fishContextHelperArmScript)
}

func fishContextHelperArmScript(name, invocation string) string {
	return fmt.Sprintf("function %s --on-event fish_prompt\n  functions --erase %s\n  %s\nend", name, name, invocation)
}
