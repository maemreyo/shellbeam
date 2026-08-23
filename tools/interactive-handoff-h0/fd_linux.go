//go:build linux

package main

func selfFDCount() (int, error) { return countFDDirectory("/proc/self/fd") }
