//go:build darwin

package process

import "github.com/maemreyo/shellbeam/internal/core/capability"

func newResourceProviderFromEnvironment() (resourceProvider, *capability.ResourceEnforcementSupport, error) {
	return nil, nil, nil
}
