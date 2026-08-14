package daemon

import workspace "github.com/maemreyo/shellbeam/internal/core/workspace"

type ManagedShellLease interface {
	End()
}

type WorkspaceCoherence interface {
	BeginManagedShell() ManagedShellLease
	CaptureBarrier() workspace.CoherenceBarrier
}
