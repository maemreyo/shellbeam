//go:build linux

package delegatedtmux

import (
	"context"
	"errors"
)

var errProcessAlreadyGone = errors.New("process already gone")
var errProcessWatchUnavailable = errors.New("process exit watch unavailable on unqualified platform")

type processExitWatcher struct{}

func newProcessExitWatcher(int) (*processExitWatcher, error) { return nil, errProcessWatchUnavailable }
func (*processExitWatcher) Wait(context.Context) error       { return errProcessWatchUnavailable }
func (*processExitWatcher) Close() error                     { return nil }
