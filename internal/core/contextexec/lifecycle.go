package contextexec

import "fmt"

type Lifecycle string

const (
	LifecycleReserved            Lifecycle = "reserved"
	LifecycleHelperRequested     Lifecycle = "helper_requested"
	LifecycleHelperAuthenticated Lifecycle = "helper_authenticated"
	LifecycleChildReserved       Lifecycle = "child_reserved"
	LifecycleChildSpawned        Lifecycle = "child_spawned"
	LifecycleChildTerminal       Lifecycle = "child_terminal"
	LifecycleCanonicalized       Lifecycle = "canonicalized"
	LifecycleHelperLost          Lifecycle = "helper_lost"
	LifecycleAmbiguous           Lifecycle = "ambiguous"
)

func (v Lifecycle) Validate() error {
	switch v {
	case LifecycleReserved, LifecycleHelperRequested, LifecycleHelperAuthenticated, LifecycleChildReserved, LifecycleChildSpawned, LifecycleChildTerminal, LifecycleCanonicalized, LifecycleHelperLost, LifecycleAmbiguous:
		return nil
	default:
		return fmt.Errorf("invalid context exec lifecycle")
	}
}

func (v Lifecycle) Terminal() bool {
	return v == LifecycleCanonicalized || v == LifecycleHelperLost || v == LifecycleAmbiguous
}

func (v Lifecycle) CanAdvanceTo(next Lifecycle) bool {
	if v.Validate() != nil || next.Validate() != nil || v.Terminal() {
		return false
	}
	if next == LifecycleHelperLost || next == LifecycleAmbiguous {
		return v != LifecycleReserved
	}
	switch v {
	case LifecycleReserved:
		return next == LifecycleHelperRequested
	case LifecycleHelperRequested:
		return next == LifecycleHelperAuthenticated
	case LifecycleHelperAuthenticated:
		return next == LifecycleChildReserved
	case LifecycleChildReserved:
		return next == LifecycleChildSpawned
	case LifecycleChildSpawned:
		return next == LifecycleChildTerminal
	case LifecycleChildTerminal:
		return next == LifecycleCanonicalized
	default:
		return false
	}
}
