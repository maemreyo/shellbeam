//go:build darwin

package main

func selfFDCount() (int, error) { return countFDDirectory("/dev/fd") }
