package shellintegration

import (
	"context"
	"fmt"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type BashAdapter struct{ deps Dependencies }

func NewBashAdapter(deps Dependencies) (*BashAdapter, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	return &BashAdapter{deps: deps}, nil
}

func (*BashAdapter) Family() core.ShellFamily { return core.ShellBash }

func (a *BashAdapter) Install(_ context.Context, req app.WatchRequest) (app.RequirementWatcher, error) {
	if req.Shell.Family != core.ShellBash {
		return nil, fmt.Errorf("bash adapter shell mismatch")
	}
	watcher, _, err := newOneShotWatcher(req, a.deps, bashScripts)
	return watcher, err
}

func bashScripts(req app.WatchRequest, eventID, trueNotify, falseNotify string) (string, string) {
	name := "__shellbeam_handoff_" + eventID
	variable := req.Requirement.Name
	install := fmt.Sprintf("%s() {\n  local __shellbeam_exported=false __shellbeam_name\n  for __shellbeam_name in $(compgen -A export -- %s); do\n    if [ \"$__shellbeam_name\" = %s ]; then __shellbeam_exported=true; break; fi\n  done\n  if [ \"$__shellbeam_exported\" = true ] && [ -n \"${%s}\" ]; then\n    %s\n  else\n    %s\n  fi\n  PROMPT_COMMAND=\"${PROMPT_COMMAND%%;%s}\"\n  unset -f %s\n}\nPROMPT_COMMAND=\"${PROMPT_COMMAND:+$PROMPT_COMMAND;}%s\"", name, variable, variable, variable, trueNotify, falseNotify, name, name, name)
	cleanup := fmt.Sprintf("PROMPT_COMMAND=\"${PROMPT_COMMAND%%;%s}\"; unset -f %s 2>/dev/null || true", name, name)
	return install, cleanup
}
