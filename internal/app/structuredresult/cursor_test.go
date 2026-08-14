package structuredresult

import (
	"errors"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestStructuredCursorRoundTripBindsOperationDerivationAndFilter(t *testing.T) {
	codec, err := NewResultCursorCodec(structuredCursorKey("0"))
	if err != nil {
		t.Fatal(err)
	}
	filter := RecordFilter{RecordKind: "diagnostic", Severity: "error", Path: "internal/a.go"}
	token, err := codec.Encode("op-1", strings.Repeat("a", 64), filter, 17)
	if err != nil || !strings.HasPrefix(token, ResultCursorPrefix) {
		t.Fatalf("token=%q err=%v", token, err)
	}
	offset, err := codec.Decode(token, "op-1", strings.Repeat("a", 64), filter)
	if err != nil || offset != 17 {
		t.Fatalf("offset=%d err=%v", offset, err)
	}
	cases := []struct {
		name, op, key string
		filter        RecordFilter
	}{
		{"operation", "op-2", strings.Repeat("a", 64), filter},
		{"derivation", "op-1", strings.Repeat("b", 64), filter},
		{"filter", "op-1", strings.Repeat("a", 64), RecordFilter{RecordKind: "diagnostic", Severity: "warning"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := codec.Decode(token, tc.op, tc.key, tc.filter); !errors.Is(err, failure.InvalidInput) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	tampered := token[:len(token)-1] + "A"
	if _, err := codec.Decode(tampered, "op-1", strings.Repeat("a", 64), filter); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("tamper=%v", err)
	}
	old, _ := NewResultCursorCodec(structuredCursorKey("1"))
	oldToken, _ := old.Encode("op-1", strings.Repeat("a", 64), filter, 1)
	if _, err := codec.Decode(oldToken, "op-1", strings.Repeat("a", 64), filter); !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("epoch=%v", err)
	}
}

func structuredCursorKey(digit string) observation.CursorKeyMaterial {
	return observation.CursorKeyMaterial{StateRootEpoch: "epoch_" + strings.Repeat(digit, 32), Generation: "key_" + strings.Repeat(digit, 32), Secret: []byte(strings.Repeat(digit, 32))}
}
