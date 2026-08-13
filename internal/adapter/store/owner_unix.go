//go:build linux || darwin

package store

import (
	"os"
	"syscall"
)

func ownedByCurrent(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
