//go:build linux

package ipc

import (
	"fmt"
	"golang.org/x/sys/unix"
	"net"
)

type authListener struct {
	net.Listener
	uid uint32
}

func (l *authListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		u, err := peerUID(c)
		if err == nil && u == l.uid {
			return &authenticatedConn{Conn: c, uid: u}, nil
		}
		_ = c.Close()
		if err != nil {
			return nil, err
		}
	}
}
func peerUID(c net.Conn) (uint32, error) {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("not unix")
	}
	raw, err := u.SyscallConn()
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
