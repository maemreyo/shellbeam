package daemon

import delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"

// DelegatedRuntime is the qualified provider boundary used only when a request
// explicitly selects delegated_interactive. Ordinary and persistent starts do
// not consult it.
type DelegatedRuntime interface {
	delegatedapp.Provider
}
