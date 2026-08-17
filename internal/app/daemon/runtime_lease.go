package daemon

import "errors"

var ErrRuntimeLeaseSourceUnavailable = errors.New("runtime lease source unavailable")

// RuntimeLease is an exclusive ownership reference for a daemon runtime
// directory. The infrastructure adapter that acquires it owns the locking
// mechanics; consumers only need lifetime release semantics.
type RuntimeLease interface {
	Release() error
}

// RuntimeLeaseSource can acquire another ownership reference related to an
// already-held daemon lease. This preserves same-directory state/runtime lease
// sharing without making one infrastructure adapter depend on another.
type RuntimeLeaseSource interface {
	AcquireRuntimeLease(dir, incarnation string) (RuntimeLease, error)
}
