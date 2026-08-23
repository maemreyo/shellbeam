//go:build linux

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
	var cred *unix.Ucred
	var opErr error
	if err = raw.Control(func(fd uintptr) { cred, opErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return 0, 0, err
	}
	if opErr != nil {
		return 0, 0, opErr
	}
	return int(cred.Pid), cred.Uid, nil
}

func platformForegroundVerifier(int, string) error {
	return fmt.Errorf("context helper foreground verification unavailable")
}
