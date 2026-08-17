package ipc

import daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"

// RuntimeLease is the consumer-owned daemon lifetime port. Server owns the
// supplied reference after successful construction and releases it last on
// Close.
type RuntimeLease = daemonapp.RuntimeLease

// ListenPendingAs acquires the runtime ownership reference through the
// caller's already-held daemon lease, then transfers that reference to Server.
func ListenPendingAs(runtime, incarnation string, actions Actions, source daemonapp.RuntimeLeaseSource) (*Server, error) {
	if source == nil {
		return nil, daemonapp.ErrRuntimeLeaseSourceUnavailable
	}
	lease, err := source.AcquireRuntimeLease(runtime, incarnation)
	if err != nil {
		return nil, err
	}
	return ListenPendingWithLease(runtime, actions, lease)
}
