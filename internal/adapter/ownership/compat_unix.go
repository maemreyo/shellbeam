//go:build linux || darwin

// Package ownership is a compatibility facade for the shared runtime ownership
// primitive. New production code should import internal/ownership directly.
package ownership

import shared "github.com/maemreyo/shellbeam/internal/ownership"

var ErrOwnerAlive = shared.ErrOwnerAlive

const OwnerFileName = shared.OwnerFileName

type Owner = shared.Owner
type Lease = shared.Lease

func Acquire(dir, incarnation string) (*Lease, error) {
	return shared.Acquire(dir, incarnation)
}

func AcquireWith(existing *Lease, dir, incarnation string) (*Lease, error) {
	return shared.AcquireWith(existing, dir, incarnation)
}

func Held(dir string) (bool, error) {
	return shared.Held(dir)
}

func ReadOwner(dir string) (Owner, bool, error) {
	return shared.ReadOwner(dir)
}
