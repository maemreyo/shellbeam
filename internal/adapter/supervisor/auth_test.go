package supervisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCapabilityChallengeProofBindsSessionGenerationAndChallenge(t *testing.T) {
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(capability.bytes(), other.bytes()) || len(capability.bytes()) != CapabilityBytes {
		t.Fatal("capabilities are not independent fixed-width secrets")
	}
	challenge, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := Proof(capability, "persistent-session-a", "generation-a", challenge)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyProof(capability, "persistent-session-a", "generation-a", challenge, proof) {
		t.Fatal("valid proof rejected")
	}
	proofAlias := nonCanonicalBase64Alias(t, proof)
	for name, changed := range map[string]struct{ session, generation, challenge, proof string }{
		"session":    {"persistent-session-b", "generation-a", challenge, proof},
		"generation": {"persistent-session-a", "generation-b", challenge, proof},
		"challenge":  {"persistent-session-a", "generation-a", challenge + "x", proof},
		"proof":      {"persistent-session-a", "generation-a", challenge, proofAlias},
	} {
		t.Run(name, func(t *testing.T) {
			if VerifyProof(capability, changed.session, changed.generation, changed.challenge, changed.proof) {
				t.Fatal("changed proof input accepted")
			}
		})
	}
}

func nonCanonicalBase64Alias(t *testing.T, canonical string) string {
	t.Helper()
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, canonical[len(canonical)-1])
	if last < 0 || last%4 != 0 || last+1 >= len(alphabet) {
		t.Fatalf("unexpected canonical base64 proof tail: %q", canonical)
	}
	return canonical[:len(canonical)-1] + string(alphabet[last+1])
}

func TestCapabilityFormattingAndJSONNeverExposeSecret(t *testing.T) {
	secret := bytes.Repeat([]byte{0x41}, CapabilityBytes)
	capability, err := capabilityFromBytes(secret)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := strings.Repeat("41", CapabilityBytes)
	formatted := fmt.Sprintf("%v %+v %#v %s", capability, capability, capability, capability)
	if strings.Contains(formatted, sentinel) || strings.Contains(formatted, strings.Repeat("A", CapabilityBytes)) {
		t.Fatalf("capability leaked through formatting: %q", formatted)
	}
	encoded, err := json.Marshal(capability)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), sentinel) || strings.Contains(string(encoded), strings.Repeat("A", CapabilityBytes)) {
		t.Fatalf("capability leaked through json: %s", encoded)
	}
}

func TestCapabilityAndChallengeRejectMalformedMaterial(t *testing.T) {
	if _, err := capabilityFromBytes(make([]byte, CapabilityBytes-1)); err == nil {
		t.Fatal("short capability accepted")
	}
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Proof(capability, "persistent-session-a", "generation-a", "challenge"); err == nil {
		t.Fatal("non-random challenge encoding accepted")
	}
	for _, tc := range []struct{ session, generation, challenge string }{
		{"../bad", "generation-a", "challenge"},
		{"persistent-session-a", "../bad", "challenge"},
		{"persistent-session-a", "generation-a", ""},
	} {
		if _, err := Proof(capability, tc.session, tc.generation, tc.challenge); err == nil {
			t.Fatalf("malformed proof input accepted: %#v", tc)
		}
	}
}
