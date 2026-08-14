package workspace

import (
	"fmt"
	"strings"
)

// CoherenceBarrier describes ShellBeam-owned shell lifecycle invalidation state.
// It is cache-coherence metadata only; it is not filesystem containment proof.
type CoherenceBarrier struct {
	DaemonIncarnation            string `json:"daemon_incarnation"`
	Epoch                        uint64 `json:"state_root_shell_freshness_epoch"`
	ActiveManagedShellOperations int    `json:"active_managed_shell_operations"`
}

func (b CoherenceBarrier) Validate() error {
	if strings.TrimSpace(b.DaemonIncarnation) == "" {
		return fmt.Errorf("daemon incarnation missing")
	}
	if b.ActiveManagedShellOperations < 0 {
		return fmt.Errorf("active managed shell operations negative")
	}
	return nil
}
