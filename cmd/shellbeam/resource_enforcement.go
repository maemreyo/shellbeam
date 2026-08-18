package main

import (
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

// resourceOwnerFactory couples the executable process owner to the capability
// proof produced by the same qualification attempt. Keeping those two values in
// one result prevents inspect.server from advertising a provider different from
// the one that will actually own child processes.
type resourceOwnerFactory func() (daemonapp.ProcessOwner, *capability.ResourceEnforcementSupport, error)

func composeResourceEnforcement(catalog capability.Catalog, factory resourceOwnerFactory) (daemonapp.ProcessOwner, capability.Catalog) {
	ordinary := daemonapp.ProcessOwner(processadapter.Owner{})
	if factory == nil {
		factory = defaultResourceOwnerFactory
	}
	owner, support, err := factory()
	if err != nil || owner == nil || support == nil || !support.ValidV1() {
		return ordinary, catalog
	}
	advertised := catalog.WithResourceEnforcement(*support)
	if advertised.Features[capability.FeatureResourceEnforcement] != capability.Available || advertised.ResourceEnforcement == nil {
		return ordinary, catalog
	}
	return owner, advertised
}

func defaultResourceOwnerFactory() (daemonapp.ProcessOwner, *capability.ResourceEnforcementSupport, error) {
	owner, support, err := processadapter.NewOwnerFromEnvironment()
	if err != nil {
		return nil, nil, err
	}
	return owner, support, nil
}
