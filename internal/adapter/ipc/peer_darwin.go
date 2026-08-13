//go:build darwin

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
			return c, nil
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
