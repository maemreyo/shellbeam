//go:build !darwin && !linux

package main

import (
	"errors"
	"runtime"
)

func selfFDCount() (int, error) {
	return 0, errors.New("self FD accounting unsupported on " + runtime.GOOS)
}
