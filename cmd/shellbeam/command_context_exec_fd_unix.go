//go:build darwin || linux

package main

import "golang.org/x/sys/unix"

func closeContextFD(fd int) error { return unix.Close(fd) }
