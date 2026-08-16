//go:build linux || darwin

package main

import (
	"context"
	"fmt"
	"os"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	supervisor "github.com/maemreyo/shellbeam/internal/adapter/supervisor"
)

const (
	supervisorBootstrapFD  = 3
	supervisorCapabilityFD = 4
)

func runSupervisor(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	bootstrapFile := os.NewFile(uintptr(supervisorBootstrapFD), "shellbeam-supervisor-bootstrap")
	if bootstrapFile == nil {
		return fmt.Errorf("supervisor bootstrap unavailable")
	}
	defer bootstrapFile.Close()
	bootstrap, err := supervisor.DecodeBootstrap(bootstrapFile)
	if err != nil {
		return err
	}
	capabilityFile := os.NewFile(uintptr(supervisorCapabilityFD), "shellbeam-supervisor-capability")
	if capabilityFile == nil {
		return fmt.Errorf("supervisor capability unavailable")
	}
	defer capabilityFile.Close()
	capability, err := supervisor.DecodeCapability(capabilityFile)
	if err != nil {
		return err
	}
	return supervisor.Run(ctx, bootstrap, capability, processadapter.FrozenOwner{})
}
