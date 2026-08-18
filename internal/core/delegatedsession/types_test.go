package delegatedsession

import (
	"strings"
	"testing"
)

func TestClosedDelegatedSessionVocabularyAndValidation(t *testing.T) {
	if ModeDelegatedInteractive != "delegated_interactive" {
		t.Fatalf("mode=%q", ModeDelegatedInteractive)
	}
	for _, epoch := range []AuthorityEpoch{1, 2, 99} {
		if err := epoch.Validate(); err != nil {
			t.Fatalf("valid epoch %d rejected: %v", epoch, err)
		}
	}
	if err := AuthorityEpoch(0).Validate(); err == nil {
		t.Fatal("zero authority epoch accepted")
	}

	for _, owner := range []Owner{OwnerNone, OwnerAgent, OwnerHuman} {
		if err := owner.Validate(); err != nil {
			t.Fatalf("valid owner %q rejected: %v", owner, err)
		}
	}
	if err := Owner("daemon").Validate(); err == nil {
		t.Fatal("unknown owner accepted")
	}

	for _, kind := range []MutationKind{
		MutationWrite, MutationSignal, MutationKill, MutationResize,
		MutationTransfer, MutationHumanControl, MutationProviderAuthority,
	} {
		if err := kind.Validate(); err != nil {
			t.Fatalf("valid mutation kind %q rejected: %v", kind, err)
		}
	}
	if err := MutationKind("shell_eval").Validate(); err == nil {
		t.Fatal("unknown mutation kind accepted")
	}
}

func TestProviderIdentityAndBindingValidation(t *testing.T) {
	provider := ProviderIdentity{ID: "tmux_control_mode", Version: 1}
	if err := provider.Validate(); err != nil {
		t.Fatalf("valid provider rejected: %v", err)
	}
	for name, candidate := range map[string]ProviderIdentity{
		"empty_id":     {Version: 1},
		"zero_version": {ID: "tmux_control_mode"},
		"slash":        {ID: "tmux/control", Version: 1},
		"control":      {ID: "tmux\ncontrol", Version: 1},
		"oversized":    {ID: strings.Repeat("x", MaxProviderIDBytes+1), Version: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := candidate.Validate(); err == nil {
				t.Fatalf("invalid provider accepted: %#v", candidate)
			}
		})
	}

	binding := Binding{
		SessionID:    "01M0H1DELEGATEDSESSION000001",
		OperationID:  "h1-op-1",
		Provider:     provider,
		Epoch:        1,
		DesiredOwner: OwnerAgent,
	}
	if err := binding.Validate(); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	cases := map[string]func(*Binding){
		"session":   func(v *Binding) { v.SessionID = "bad/session" },
		"operation": func(v *Binding) { v.OperationID = "" },
		"provider":  func(v *Binding) { v.Provider.Version = 0 },
		"epoch":     func(v *Binding) { v.Epoch = 0 },
		"owner":     func(v *Binding) { v.DesiredOwner = "unknown" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := binding
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid binding accepted: %#v", got)
			}
		})
	}
}
