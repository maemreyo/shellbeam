//go:build linux

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
	var cred *unix.Ucred
	var opErr error
	if err = raw.Control(func(fd uintptr) { cred, opErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return 0, err
	}
	if opErr != nil {
		return 0, opErr
	}
	return cred.Uid, nil
}
