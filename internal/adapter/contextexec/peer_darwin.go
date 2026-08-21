//go:build darwin

package contextexec

import (
	"fmt"
	"golang.org/x/sys/unix"
	"net"
)

func peerCredentials(conn net.Conn) (int, uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, fmt.Errorf("context helper peer is not unix")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var pid int
	var uid uint32
	var opErr error
	if err = raw.Control(func(fd uintptr) {
		var cred *unix.Xucred
		cred, opErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if opErr == nil {
			uid = cred.Uid
			pid, opErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		}
	}); err != nil {
		return 0, 0, err
	}
	return pid, uid, opErr
}
