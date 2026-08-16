//go:build linux || darwin

package ipc

import (
	"github.com/maemreyo/shellbeam/internal/adapter/ownership"
)

func Listen(runtime string, actions Actions) (*Server, error) {
	return listen(runtime, actions, dialUnixSocket)
}

func ListenPending(runtime string, actions Actions) (*Server, error) {
	lease, err := ownership.Acquire(runtime, "")
	if err != nil {
		return nil, err
	}
	server, err := listenWithReadiness(runtime, actions, lease, dialUnixSocket, false)
	if err != nil {
		_ = lease.Release()
		return nil, err
	}
	return server, nil
}

func listen(runtime string, actions Actions, dial socketDialer) (*Server, error) {
	lease, err := ownership.Acquire(runtime, "")
	if err != nil {
		return nil, err
	}
	server, err := listenWithReadiness(runtime, actions, lease, dial, true)
	if err != nil {
		_ = lease.Release()
		return nil, err
	}
	return server, nil
}
