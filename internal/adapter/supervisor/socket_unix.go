//go:build linux || darwin

package supervisor

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type controlListener struct {
	net.Listener
	path string
	info os.FileInfo
	uid  uint32
}

func ListenControl(layout Layout) (net.Listener, error) {
	if err := validateLayout(layout); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(layout.SocketPath); err == nil {
		return nil, socketFailure("collision")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, socketFailure("collision")
	}
	staged, err := stagedControlSocketPath(layout.SessionDir)
	if err != nil {
		return nil, socketFailure("staging")
	}
	listener, err := net.Listen("unix", staged)
	if err != nil {
		return nil, socketFailure("listen")
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	cleanup := func() {
		_ = listener.Close()
		_ = os.Remove(staged)
	}
	if err := os.Chmod(staged, 0600); err != nil {
		cleanup()
		return nil, socketFailure("permissions")
	}
	stagedInfo, err := os.Lstat(staged)
	if err != nil || stagedInfo.Mode()&os.ModeSocket == 0 || stagedInfo.Mode().Perm() != 0600 || !ownedByCurrent(stagedInfo) {
		cleanup()
		return nil, socketFailure("staging")
	}
	if err := os.Link(staged, layout.SocketPath); err != nil {
		cleanup()
		return nil, socketFailure("publish")
	}
	published, err := os.Lstat(layout.SocketPath)
	if err != nil || !os.SameFile(stagedInfo, published) || published.Mode().Perm() != 0600 || !ownedByCurrent(published) {
		_ = os.Remove(layout.SocketPath)
		cleanup()
		return nil, socketFailure("publish")
	}
	if err := os.Remove(staged); err != nil {
		_ = os.Remove(layout.SocketPath)
		cleanup()
		return nil, socketFailure("publish")
	}
	return &controlListener{Listener: listener, path: layout.SocketPath, info: published, uid: uint32(os.Getuid())}, nil
}

func (l *controlListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uid, peerErr := peerUID(conn)
		if peerErr == nil && uid == l.uid {
			return conn, nil
		}
		_ = conn.Close()
		if peerErr != nil {
			return nil, socketFailure("peer")
		}
	}
}

func (l *controlListener) Close() error {
	err := l.Listener.Close()
	current, statErr := os.Lstat(l.path)
	if statErr == nil && os.SameFile(l.info, current) {
		_ = os.Remove(l.path)
	}
	return err
}

func stagedControlSocketPath(dir string) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return filepath.Join(dir, ".control.sock.pending-"+hex.EncodeToString(nonce[:])), nil
}

func socketFailure(reason string) error {
	return failure.New(failure.SupervisorUnavailable, map[string]string{"reason": reason}, fmt.Errorf("supervisor control socket unavailable"))
}
