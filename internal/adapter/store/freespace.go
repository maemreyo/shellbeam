//go:build linux || darwin

package store

import "golang.org/x/sys/unix"

// AvailableBytes reports how much room the filesystem holding dir still offers
// this user.
//
// The byte budget the store admits against counts what the store itself wrote,
// which says nothing about the disk underneath it: every other process on the
// machine writes to that same filesystem, so a store well inside its budget can
// still be sitting on a volume with nothing left. This is the only figure that
// answers the second question, and it is deliberately separate from the first.
//
// It reads the blocks available to an unprivileged user rather than the blocks
// merely unallocated. The difference is the reserve the filesystem keeps for
// root, which this daemon cannot spend and must not count as headroom.
func AvailableBytes(dir string) (int64, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(dir, &fs); err != nil {
		return 0, err
	}
	return int64(fs.Bavail) * int64(fs.Bsize), nil
}
