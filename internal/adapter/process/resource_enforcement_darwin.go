//go:build darwin

package process

import (
	"os/exec"

	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func newResourceProviderFromEnvironment() (resourceProvider, *capability.ResourceEnforcementSupport, error) {
	return nil, nil, nil
}

func bindResourceDomainToCommand(string, *exec.Cmd) (resourceSpawnBinding, error) {
	return nil, resourceProviderFailure("atomic_placement_unsupported")
}
