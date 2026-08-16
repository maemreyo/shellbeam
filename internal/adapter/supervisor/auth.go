package supervisor

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const (
	CapabilityBytes = 32
	ChallengeBytes  = 32
)

type Capability struct {
	secret [CapabilityBytes]byte
}

func NewCapability() (Capability, error) {
	var capability Capability
	if _, err := rand.Read(capability.secret[:]); err != nil {
		return Capability{}, fmt.Errorf("generate supervisor capability")
	}
	return capability, nil
}

func capabilityFromBytes(raw []byte) (Capability, error) {
	if len(raw) != CapabilityBytes {
		return Capability{}, fmt.Errorf("invalid supervisor capability")
	}
	var capability Capability
	copy(capability.secret[:], raw)
	return capability, nil
}

func (c Capability) bytes() []byte {
	return append([]byte(nil), c.secret[:]...)
}

func EncodeCapability(writer io.Writer, capability Capability) error {
	raw := capability.bytes()
	written := 0
	for written < len(raw) {
		n, err := writer.Write(raw[written:])
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		written += n
	}
	return nil
}

func DecodeCapability(reader io.Reader) (Capability, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, CapabilityBytes+1))
	if err != nil || len(raw) != CapabilityBytes {
		return Capability{}, fmt.Errorf("invalid supervisor capability")
	}
	return capabilityFromBytes(raw)
}

func (Capability) String() string { return "[redacted-supervisor-capability]" }

func (Capability) MarshalJSON() ([]byte, error) {
	return []byte(`"[redacted-supervisor-capability]"`), nil
}

func (Capability) Format(state fmt.State, verb rune) {
	_, _ = fmt.Fprint(state, "[redacted-supervisor-capability]")
}

func NewChallenge() (string, error) {
	raw := make([]byte, ChallengeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate supervisor challenge")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func Proof(capability Capability, sessionID, generationID, challenge string) (string, error) {
	payload, err := proofPayload(sessionID, generationID, challenge)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, capability.secret[:])
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyProof(capability Capability, sessionID, generationID, challenge, proof string) bool {
	expected, err := Proof(capability, sessionID, generationID, challenge)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(proof), []byte(expected))
}

func proofPayload(sessionID, generationID, challenge string) ([]byte, error) {
	if _, err := operation.ParseSessionID(sessionID); err != nil {
		return nil, fmt.Errorf("invalid supervisor proof identity")
	}
	if !validOpaque(generationID) {
		return nil, fmt.Errorf("invalid supervisor proof identity")
	}
	if !validChallenge(challenge) {
		return nil, fmt.Errorf("invalid supervisor challenge")
	}
	return []byte(fmt.Sprintf("shellbeam-supervisor-v%d\x00%s\x00%s\x00%s", ProtocolVersion, sessionID, generationID, challenge)), nil
}

func validChallenge(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == ChallengeBytes
}

func validOpaque(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}
