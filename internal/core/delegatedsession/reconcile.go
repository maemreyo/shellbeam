package delegatedsession

type ReconcileInput struct {
	Epoch            AuthorityEpoch
	DesiredOwner     Owner
	ObservedOwner    Owner
	ProviderIdentity ProviderIdentity
	ProviderCurrent  bool
}

type EffectiveAuthority struct {
	Epoch  AuthorityEpoch `json:"authority_epoch"`
	Owner  Owner          `json:"owner"`
	Fenced bool           `json:"fenced"`
}

func ReconcileAuthority(in ReconcileInput) EffectiveAuthority {
	fenced := EffectiveAuthority{Epoch: in.Epoch, Owner: OwnerNone, Fenced: true}
	if in.Epoch.Validate() != nil || in.ProviderIdentity.Validate() != nil || !in.ProviderCurrent {
		return fenced
	}
	if in.DesiredOwner.Validate() != nil || in.ObservedOwner.Validate() != nil {
		return fenced
	}
	if in.DesiredOwner != OwnerAgent || in.ObservedOwner != OwnerAgent {
		return fenced
	}
	return EffectiveAuthority{Epoch: in.Epoch, Owner: OwnerAgent, Fenced: false}
}
