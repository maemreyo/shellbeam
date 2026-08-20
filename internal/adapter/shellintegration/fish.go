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
	variable := req.Requirement.Name
	install := fmt.Sprintf("function %s --on-event fish_prompt\n  if set --query --export %s; and test -n \"$%s\"\n    %s\n  else\n    %s\n  end\n  functions --erase %s\nend", name, variable, variable, trueNotify, falseNotify, name)
	cleanup := fmt.Sprintf("functions --erase %s 2>/dev/null; or true", name)
	return install, cleanup
}
