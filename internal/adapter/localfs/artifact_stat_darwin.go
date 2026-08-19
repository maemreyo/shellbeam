//go:build darwin

package localfs

import "golang.org/x/sys/unix"

func artifactStatTimes(st unix.Stat_t) (mtimeNS, ctimeNS int64) {
	return st.Mtim.Sec*1_000_000_000 + st.Mtim.Nsec, st.Ctim.Sec*1_000_000_000 + st.Ctim.Nsec
}
