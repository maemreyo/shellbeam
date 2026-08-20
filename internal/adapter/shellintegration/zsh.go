package shellintegration

import (
	"context"
	"fmt"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ZshAdapter struct{ deps Dependencies }

func NewZshAdapter(deps Dependencies) (*ZshAdapter, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	return &ZshAdapter{deps: deps}, nil
}

func (*ZshAdapter) Family() core.ShellFamily { return core.ShellZsh }

func (a *ZshAdapter) Install(_ context.Context, req app.WatchRequest) (app.RequirementWatcher, error) {
	if req.Shell.Family != core.ShellZsh {
		return nil, fmt.Errorf("zsh adapter shell mismatch")
	}
	watcher, _, err := newOneShotWatcher(req, a.deps, zshScripts)
	return watcher, err
}

func zshScripts(req app.WatchRequest, eventID, trueNotify, falseNotify string) (string, string) {
	name := "__shellbeam_handoff_" + eventID
	variable := req.Requirement.Name
	install := fmt.Sprintf("typeset -ga precmd_functions\nfunction %s() {\n  if [[ ${parameters[%s]-} == *export* && -n \"${%s}\" ]]; then\n    %s\n  else\n    %s\n  fi\n  precmd_functions=(${precmd_functions:#%s})\n  unfunction %s\n}\nprecmd_functions+=(%s)", name, variable, variable, trueNotify, falseNotify, name, name, name)
	cleanup := fmt.Sprintf("typeset -ga precmd_functions; precmd_functions=(${precmd_functions:#%s}); unfunction %s 2>/dev/null || true", name, name)
	return install, cleanup
}
