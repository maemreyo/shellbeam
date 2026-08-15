package evidence

import environment "github.com/maemreyo/shellbeam/internal/core/environment"

func cloneEnvironmentBinding(binding *environment.Binding) *environment.Binding {
	if binding == nil {
		return nil
	}
	copy := *binding
	return &copy
}
