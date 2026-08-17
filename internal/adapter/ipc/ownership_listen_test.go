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
	return listenWithReadiness(runtime, actions, lease, dialUnixSocket, false)
}

func listen(runtime string, actions Actions, dial socketDialer) (*Server, error) {
	lease, err := ownership.Acquire(runtime, "")
	if err != nil {
		return nil, err
	}
	return listenWithReadiness(runtime, actions, lease, dial, true)
}
