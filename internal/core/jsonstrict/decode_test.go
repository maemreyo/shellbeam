package jsonstrict

import "testing"

type testPayload struct {
	Action string `json:"action"`
}

func TestDecodeRejectsAmbiguousJSON(t *testing.T) {
	cases := map[string][]byte{
		"duplicate":    []byte(`{"action":"x","action":"y"}`),
		"wrong-case":   []byte(`{"Action":"x"}`),
		"unknown":      []byte(`{"action":"x","unknown":1}`),
		"invalid-utf8": append([]byte(`{"action":"`), append([]byte{0xff}, []byte(`"}`)...)...),
		"trailing":     []byte(`{"action":"x"} {}`),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var got testPayload
			if err := Decode(in, &got); err == nil {
				t.Fatalf("accepted %q", in)
			}
		})
	}
}

func TestDecodeAcceptsExactKnownMembers(t *testing.T) {
	var got testPayload
	if err := Decode([]byte(`{"action":"start"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "start" {
		t.Fatalf("action=%q", got.Action)
	}
}
