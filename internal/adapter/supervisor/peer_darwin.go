//go:build darwin

package supervisor

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func peerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("not unix")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var opErr error
	if err = raw.Control(func(fd uintptr) {
		var cred *unix.Xucred
		cred, opErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if opErr == nil {
			uid = cred.Uid
		}
	}); err != nil {
		return 0, err
	}
	return uid, opErr
}
