//go:build linux || darwin

package ipc

import "github.com/maemreyo/shellbeam/internal/adapter/ownership"

func Listen(runtime string, actions Actions) (*Server, error) {
	return listenForTest(runtime, "", actions, nil, dialUnixSocket, true)
}

func ListenPending(runtime string, actions Actions) (*Server, error) {
	return listenForTest(runtime, "", actions, nil, dialUnixSocket, false)
}

func ListenPendingAs(runtime, incarnation string, actions Actions, stateLease *ownership.Lease) (*Server, error) {
	return listenForTest(runtime, incarnation, actions, stateLease, dialUnixSocket, false)
}

func listen(runtime string, actions Actions, dial socketDialer) (*Server, error) {
	return listenForTest(runtime, "", actions, nil, dial, true)
}

func listenForTest(runtime, incarnation string, actions Actions, existing *ownership.Lease, dial socketDialer, ready bool) (*Server, error) {
	if err := prepareRuntime(runtime); err != nil {
		return nil, err
	}
	lease, err := ownership.AcquireWith(existing, runtime, incarnation)
	if err != nil {
		return nil, err
	}
	return listenWithReadiness(runtime, actions, lease, dial, ready)
}
