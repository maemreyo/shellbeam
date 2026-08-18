package main

import (
	"os"
	"strconv"
)

func countFDDirectory(path string) (int, error) {
	dir, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	self := strconv.FormatUint(uint64(dir.Fd()), 10)
	names, readErr := dir.Readdirnames(-1)
	closeErr := dir.Close()
	if readErr != nil {
		return 0, readErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	count := 0
	for _, name := range names {
		if name == self {
			continue
		}
		if _, err := strconv.Atoi(name); err == nil {
			count++
		}
	}
	return count, nil
}
