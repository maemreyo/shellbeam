package delegatedsession

import "fmt"

type AuthorityEpoch uint64

func (e AuthorityEpoch) Validate() error {
	if e < 1 {
		return fmt.Errorf("invalid delegated authority epoch")
	}
	return nil
}

type Owner string

const (
	OwnerNone  Owner = "none"
	OwnerAgent Owner = "agent"
	OwnerHuman Owner = "human"
)

func (o Owner) Validate() error {
	switch o {
	case OwnerNone, OwnerAgent, OwnerHuman:
		return nil
	default:
		return fmt.Errorf("invalid delegated owner")
	}
}
