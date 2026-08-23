package interactivehandoff

import (
	"errors"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestStateKeepsAuthorityIngressBoundaryPrivacyAndCaptureOrthogonal(t *testing.T) {
	state := agentOwnedState()
	if err := state.Validate(); err != nil {
		t.Fatalf("valid agent-owned state rejected: %v", err)
	}

	bothWritable := state
	bothWritable.HumanIngress = IngressWritable
	bothWritable.HumanClient = &HumanClientRef{Ref: "human-client-1"}
	if err := bothWritable.Validate(); err == nil {
		t.Fatal("dual writable ingress accepted")
	}

	human := state
	human.Phase = PhaseHumanOwned
	human.DesiredOwner = delegated.OwnerHuman
	human.ProviderOwner = delegated.OwnerHuman
	human.AgentIngress = IngressFenced
	human.HumanIngress = IngressWritable
	human.HumanClient = &HumanClientRef{Ref: "human-client-1"}
	human.TransferBoundary = TransferBoundary{Kind: BoundaryHumanAttested, Established: true}
	if err := human.Validate(); err != nil {
		t.Fatalf("valid human-owned state rejected: %v", err)
	}

	private := human
	private.PrivacyState = PrivacyPrivate
	private.PrivacyRelease = PrivacyReleasePending
	private.CaptureState = CapturePrivate
	if err := private.Validate(); err != nil {
		t.Fatalf("future-compatible private state rejected: %v", err)
	}
	if err := private.ValidateH2(); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("H2 private state err=%v want feature_unavailable", err)
	}
}

func TestTransferBoundaryUsesClosedQualifiedKinds(t *testing.T) {
	for _, boundary := range []TransferBoundary{
		{Kind: BoundaryNone},
		{Kind: BoundaryShell, Established: true},
		{Kind: BoundaryProcess, Established: true},
		{Kind: BoundaryProviderOrdered, Established: true},
		{Kind: BoundaryHumanAttested, Established: true},
	} {
		if err := boundary.Validate(); err != nil {
			t.Fatalf("boundary %#v rejected: %v", boundary, err)
		}
	}
	if err := (TransferBoundary{Kind: BoundaryHumanAttested}).Validate(); err == nil {
		t.Fatal("unestablished non-none boundary accepted")
	}
	if err := (TransferBoundary{Kind: BoundaryNone, Established: true}).Validate(); err == nil {
		t.Fatal("established none boundary accepted")
	}
}

func TestControlSignalReplayPrecedesEpochAndConflictsOnIDReuse(t *testing.T) {
	incoming := ControlSignal{HandoffID: "handoff-1", AuthorityEpoch: 2, ControlID: "control-ready-1", Kind: HumanControlReady}
	decision, err := DecideControl(&incoming, incoming, 9)
	if err != nil || decision.Action != ControlReplay {
		t.Fatalf("known old-epoch replay decision=%#v err=%v", decision, err)
	}

	changed := incoming
	changed.Kind = HumanControlAbort
	if _, err := DecideControl(&incoming, changed, 2); !errors.Is(err, failure.HandoffConflict) {
		t.Fatalf("same control id changed kind err=%v", err)
	}

	stale := incoming
	stale.ControlID = "control-new"
	if _, err := DecideControl(nil, stale, 3); !errors.Is(err, failure.StaleControlGeneration) {
		t.Fatalf("unseen stale err=%v", err)
	}
	if decision, err := DecideControl(nil, incoming, 2); err != nil || decision.Action != ControlReserve {
		t.Fatalf("current control decision=%#v err=%v", decision, err)
	}
}

func TestHumanControlKindClosedVocabulary(t *testing.T) {
	for _, kind := range []HumanControlKind{HumanControlReady, HumanControlAbort, HumanControlStatus, HumanControlResume, HumanControlTerminate, HumanControlRequestControl} {
		signal := ControlSignal{HandoffID: "handoff-1", AuthorityEpoch: 1, ControlID: "control-" + string(kind), Kind: kind}
		if err := signal.Validate(); err != nil {
			t.Fatalf("kind %q rejected: %v", kind, err)
		}
	}
	if err := (ControlSignal{HandoffID: "handoff-1", AuthorityEpoch: 1, ControlID: "control-x", Kind: "shell_eval"}).Validate(); err == nil {
		t.Fatal("unknown human control accepted")
	}
}

func agentOwnedState() State {
	return State{
		SchemaVersion: StateSchemaVersion,
		HandoffID:     "handoff-1", SessionID: "session-1", Phase: PhaseAgentOwned, AuthorityEpoch: 1,
		DesiredOwner: delegated.OwnerAgent, ProviderOwner: delegated.OwnerAgent,
		AgentIngress: IngressWritable, HumanIngress: IngressFenced,
		TransferBoundary: TransferBoundary{Kind: BoundaryNone},
		PrivacyState:     PrivacyStateStandard, PrivacyRelease: PrivacyReleaseNotRequired, CaptureState: CapturePublic,
		ProviderGeneration: "provider-generation-1",
	}
}
