package daemon

import delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"

// DelegatedRuntime is the qualified provider boundary used only when a request
// explicitly selects delegated_interactive. Ordinary and persistent starts do
// not consult it.
type DelegatedRuntime interface {
	delegatedapp.Provider
}

// DelegatedDetachRuntime is the graceful daemon-shutdown boundary. Detach must
// release only this daemon's control observer while leaving provider-owned
// session state intact for restart reconciliation.
type DelegatedDetachRuntime interface {
	DelegatedRuntime
	delegatedapp.Detacher
}
