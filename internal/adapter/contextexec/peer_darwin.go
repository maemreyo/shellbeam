//go:build darwin

package contextexec

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
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

func foregroundDarwin(peerPID int, paneTTY string) error {
	cleanTTY := filepath.Clean(paneTTY)
	if peerPID <= 1 || !filepath.IsAbs(cleanTTY) || !strings.HasPrefix(cleanTTY, "/dev/") || strings.ContainsAny(paneTTY, "\x00\r\n") {
		return fmt.Errorf("invalid context helper foreground expectation")
	}
	var ttyStat unix.Stat_t
	if err := unix.Stat(cleanTTY, &ttyStat); err != nil || ttyStat.Mode&unix.S_IFMT != unix.S_IFCHR {
		return fmt.Errorf("context helper pane tty unavailable")
	}
	peerPGID, err := unix.Getpgid(peerPID)
	if err != nil || peerPGID <= 1 {
		return fmt.Errorf("context helper process group unproven")
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", peerPID)
	if err != nil || info == nil || int(info.Proc.P_pid) != peerPID {
		return fmt.Errorf("context helper foreground process identity unproven")
	}
	if int(info.Eproc.Pgid) != peerPGID || info.Eproc.Tpgid != info.Eproc.Pgid || info.Eproc.Tdev != int32(ttyStat.Rdev) {
		return fmt.Errorf("context helper is not pane foreground process group")
	}
	return nil
}

func platformForegroundVerifier(peerPID int, paneTTY string) error {
	return foregroundDarwin(peerPID, paneTTY)
}
