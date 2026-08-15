package evidence

import (
	"errors"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestEvidenceCursorRoundTripBindsFilterAndFrozenIndexCut(t *testing.T) {
	codec, err := NewCursorCodec(observation.CursorKeyMaterial{StateRootEpoch: "epoch_11111111111111111111111111111111", Generation: "key_22222222222222222222222222222222", Secret: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	filter := InspectFilter{WorkspaceID: "ws_01K00000000000000000000000", VerificationKind: "test"}
	token, err := codec.Encode(filter, CursorState{IndexGeneration: 42, AfterSequence: 17})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, EvidenceCursorPrefix) || len(token) > MaxCursorBytes {
		t.Fatalf("token=%q", token)
	}
	state, err := codec.Decode(token, filter)
	if err != nil || state.IndexGeneration != 42 || state.AfterSequence != 17 {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if _, err := codec.Decode(token, InspectFilter{WorkspaceID: filter.WorkspaceID, VerificationKind: "build"}); !errors.Is(err, failure.EvidenceCursorInvalid) {
		t.Fatalf("binding err=%v", err)
	}
}

func TestEvidenceCursorExpiresAcrossKeyGenerationAndRejectsTamper(t *testing.T) {
	first, err := NewCursorCodec(observation.CursorKeyMaterial{StateRootEpoch: "epoch_11111111111111111111111111111111", Generation: "key_22222222222222222222222222222222", Secret: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	filter := InspectFilter{OperationID: "op-safe"}
	token, err := first.Encode(filter, CursorState{IndexGeneration: 9, AfterSequence: 3})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCursorCodec(observation.CursorKeyMaterial{StateRootEpoch: "epoch_11111111111111111111111111111111", Generation: "key_33333333333333333333333333333333", Secret: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Decode(token, filter); !errors.Is(err, failure.EvidenceCursorExpired) {
		t.Fatalf("expired err=%v", err)
	}
	tampered := token[:len(token)-1] + "A"
	if _, err := first.Decode(tampered, filter); !errors.Is(err, failure.EvidenceCursorInvalid) {
		t.Fatalf("tamper err=%v", err)
	}
}
