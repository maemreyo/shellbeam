//go:build linux || darwin

package main

import (
	"context"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/config"
)

// housekeeping is the background maintenance that starts behind readiness.
//
// These settings travel together because they share a rule: nothing a caller
// asks for depends on any of them, so none of them may run in front of the
// daemon serving, and none of them may refuse work. Grouping them keeps that
// rule attached to the settings rather than restated at each call.
type housekeeping struct {
	terminalRetentionHours int
	stateDir               string
	minFreeSpaceBytes      int64
}

// newHousekeeping reads the maintenance settings out of resolved configuration.
func newHousekeeping(cfg config.Config, paths config.Paths) housekeeping {
	return housekeeping{
		terminalRetentionHours: cfg.TerminalRetentionHours,
		stateDir:               paths.StateDir,
		minFreeSpaceBytes:      cfg.MinFreeSpaceBytes,
	}
}

// startHousekeeping begins collecting expired history and reporting free space.
func startHousekeeping(ctx context.Context, store *storeadapter.Repository, svc *daemonapp.Service, keep housekeeping) {
	startRetention(ctx, store, keep.terminalRetentionHours)
	startObservationRetention(ctx, store)
	startHandoffExpiry(ctx, store, svc)
	startFreeSpaceWatch(ctx, keep.stateDir, keep.minFreeSpaceBytes)
}
