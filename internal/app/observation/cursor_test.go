package observation

import (
	"errors"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestEventCursorRoundTripTamperTargetAndEpochBinding(t *testing.T) {
	key := testCursorKey("0")
	codec, err := NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	target := core.Target{Kind: core.TargetOperation, OperationID: "op-1"}
	token, err := codec.Encode(target, 42)
	if err != nil || !strings.HasPrefix(token, EventCursorPrefix) {
		t.Fatalf("token=%q err=%v", token, err)
	}
	seq, err := codec.Decode(token, target)
	if err != nil || seq != 42 {
		t.Fatalf("seq=%d err=%v", seq, err)
	}
	tampered := token[:len(token)-1] + "A"
	if _, err := codec.Decode(tampered, target); !errors.Is(err, failure.EventCursorInvalid) {
		t.Fatalf("tamper error=%v", err)
	}
	other := core.Target{Kind: core.TargetOperation, OperationID: "op-2"}
	if _, err := codec.Decode(token, other); !errors.Is(err, failure.EventCursorInvalid) {
		t.Fatalf("target mismatch error=%v", err)
	}
	oldCodec, err := NewCursorCodec(testCursorKey("1"))
	if err != nil {
		t.Fatal(err)
	}
	oldToken, err := oldCodec.Encode(target, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(oldToken, target); !errors.Is(err, failure.EventCursorExpired) {
		t.Fatalf("old epoch error=%v", err)
	}
	if _, err := codec.Decode("evtcur_v1_"+strings.Repeat("x", MaxEventCursorBytes), target); !errors.Is(err, failure.EventCursorInvalid) {
		t.Fatalf("oversized cursor error=%v", err)
	}
}

func testCursorKey(digit string) core.CursorKeyMaterial {
	return core.CursorKeyMaterial{
		StateRootEpoch: "epoch_" + strings.Repeat(digit, 32),
		Generation:     "key_" + strings.Repeat(digit, 32),
		Secret:         []byte(strings.Repeat(digit, 32)),
	}
}
