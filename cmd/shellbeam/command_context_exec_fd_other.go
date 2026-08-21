//go:build !darwin && !linux

package main

func closeContextFD(int) error { return nil }
