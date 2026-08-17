package jsonstrict

import "testing"

func FuzzDecodeNeverPanics(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"action":"start"}`),
		[]byte(`{"Action":"start"}`),
		[]byte(`{"action":"x","action":"y"}`),
		[]byte(`{"action":"start"} {}`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var got testPayload
		_ = Decode(data, &got)
	})
}
