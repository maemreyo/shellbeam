package delegatedsession

import (
	"context"
	"fmt"
	"io"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	shell "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type OutputSink interface {
	Append([]byte) error
}

type CreateRequest struct {
	ProviderRef core.ProviderRef
	SessionID   string
	OperationID string
	SessionName string
	Spec        operation.ExecutionSpec
	Output      OutputSink
}

type CreateResult struct {
	ProviderRef core.ProviderRef
	Observation Observation
}

type Observation struct {
	Provider           core.ProviderIdentity
	ProviderCurrent    bool
	ProviderGeneration string
	Owner              core.Owner
	Terminal           bool
	ExitCode           *int
	PanePID            int
	CurrentCommand     string
	PaneTTY            string
	CWD                string
	OutputBytes        int64
}

type Detacher interface {
	Detach(context.Context, core.ProviderRef) error
}

type Provider interface {
	Identity() core.ProviderIdentity
	ProviderRefForSession(string, time.Time) (core.ProviderRef, error)
	Probe(context.Context) error
	Create(context.Context, CreateRequest) (CreateResult, error)
	Reattach(context.Context, core.ProviderRef, OutputSink) (Observation, error)
	Write(context.Context, core.ProviderRef, []byte) error
	Signal(context.Context, core.ProviderRef, string) error
	Inspect(context.Context, core.ProviderRef) (Observation, error)
	Wait(context.Context, core.ProviderRef) (Observation, error)
	Close(context.Context, core.ProviderRef) error
}

type ProviderClientRef struct {
	Ref string `json:"ref"`
}

func (r ProviderClientRef) Validate() error {
	if !validProviderOpaque(r.Ref, 256) {
		return fmt.Errorf("invalid provider client ref")
	}
	return nil
}

type HumanAttachSpec struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Environment []string
}

type HumanAttachResult struct {
	ClientRef     ProviderClientRef
	ObservedOwner core.Owner
	Done          <-chan error
}

type HumanClientObservation struct {
	ClientRef          ProviderClientRef
	Present            bool
	ReadOnly           bool
	ObservedOwner      core.Owner
	ProviderGeneration string
}

type IngressFenceProof struct {
	ClientRef          ProviderClientRef
	AuthorityEpoch     core.AuthorityEpoch
	ProviderGeneration string
	Fenced             bool
}

type HumanControlSpec struct {
	HandoffID      string
	AuthorityEpoch core.AuthorityEpoch
}

func (s HumanControlSpec) Validate() error {
	if !validProviderOpaque(s.HandoffID, 128) {
		return fmt.Errorf("invalid handoff id")
	}
	return s.AuthorityEpoch.Validate()
}

type HumanProvider interface {
	AttachHuman(context.Context, core.ProviderRef, HumanAttachSpec) (HumanAttachResult, error)
	SetHumanWritable(context.Context, core.ProviderRef, ProviderClientRef, bool) error
	FenceHumanIngress(context.Context, core.ProviderRef, ProviderClientRef, core.AuthorityEpoch) (IngressFenceProof, error)
	InspectHumanClient(context.Context, core.ProviderRef, ProviderClientRef) (HumanClientObservation, error)
	ArmWritableHumanControl(context.Context, core.ProviderRef, ProviderClientRef, HumanControlSpec) error
	WaitWritableHumanControl(context.Context, core.ProviderRef, ProviderClientRef, HumanControlSpec) (handoff.HumanControlKind, error)
	PrepareReadOnlyLocalControl(context.Context, core.ProviderRef, ProviderClientRef) error
}

type PrivacySpec struct {
	HandoffID      string              `json:"handoff_id"`
	AuthorityEpoch core.AuthorityEpoch `json:"authority_epoch"`
}

func (s PrivacySpec) Validate() error {
	if !validProviderOpaque(s.HandoffID, 128) {
		return fmt.Errorf("invalid privacy handoff id")
	}
	return s.AuthorityEpoch.Validate()
}

type PrivacyHandle struct {
	OpaqueRef  string `json:"opaque_ref"`
	Generation string `json:"generation"`
}

func (h PrivacyHandle) Validate() error {
	if !validProviderOpaque(h.OpaqueRef, 128) || !validProviderOpaque(h.Generation, 128) {
		return fmt.Errorf("invalid privacy handle")
	}
	return nil
}

type PrivateObservationProof struct {
	Handle               PrivacyHandle `json:"handle"`
	ProviderGeneration   string        `json:"provider_generation"`
	PrivateFromFirstByte bool          `json:"private_from_first_byte"`
	ObservedAt           time.Time     `json:"observed_at"`
}

func (p PrivateObservationProof) Validate() error {
	if err := p.Handle.Validate(); err != nil {
		return err
	}
	if !validProviderOpaque(p.ProviderGeneration, 128) || !p.PrivateFromFirstByte || p.ObservedAt.IsZero() {
		return fmt.Errorf("invalid private observation proof")
	}
	return nil
}

type ForwardBoundary struct {
	Proof shell.PrivacyReleaseProof `json:"proof"`
}

func (b ForwardBoundary) ValidateFor(spec PrivacySpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := b.Proof.Validate(); err != nil {
		return err
	}
	if b.Proof.HandoffID != spec.HandoffID || b.Proof.AuthorityEpoch != spec.AuthorityEpoch {
		return fmt.Errorf("privacy release proof does not match active handoff")
	}
	return nil
}

type PrivacyProvider interface {
	ArmPrivateObservation(context.Context, core.ProviderRef, PrivacySpec) (PrivacyHandle, error)
	ProvePrivateObservation(context.Context, core.ProviderRef, PrivacyHandle) (PrivateObservationProof, error)
	ReleasePrivateObservation(context.Context, core.ProviderRef, PrivacyHandle, ForwardBoundary) error
}

type PrivacyObservation struct {
	ProviderGeneration string    `json:"provider_generation"`
	Active             bool      `json:"active"`
	ReleasePending     bool      `json:"release_pending"`
	ObservedAt         time.Time `json:"observed_at"`
}

func (v PrivacyObservation) Validate() error {
	if !validProviderOpaque(v.ProviderGeneration, 128) || v.ObservedAt.IsZero() || v.ReleasePending != v.Active {
		return fmt.Errorf("invalid privacy observation")
	}
	return nil
}

type PrivacyInspector interface {
	InspectPrivacy(context.Context, core.ProviderRef) (PrivacyObservation, error)
}

func validProviderOpaque(v string, max int) bool {
	if len(v) < 1 || len(v) > max {
		return false
	}
	for i := 0; i < len(v); i++ {
		b := v[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}
