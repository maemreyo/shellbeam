package contextexec

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

const (
	ClaimCapabilityBytes = 32
	ClaimChallengeBytes  = 32
)

type ClaimCapability struct{ secret [ClaimCapabilityBytes]byte }

func NewClaimCapability() (ClaimCapability, error) {
	var v ClaimCapability
	if _, err := rand.Read(v.secret[:]); err != nil {
		return v, fmt.Errorf("generate context helper capability")
	}
	return v, nil
}
func (c ClaimCapability) bytes() []byte { return append([]byte(nil), c.secret[:]...) }
func (c ClaimCapability) equal(other ClaimCapability) bool {
	return hmac.Equal(c.secret[:], other.secret[:])
}
func (ClaimCapability) String() string   { return "[redacted-context-helper-capability]" }
func (ClaimCapability) GoString() string { return "[redacted-context-helper-capability]" }
func (ClaimCapability) Format(s fmt.State, verb rune) {
	_, _ = io.WriteString(s, "[redacted-context-helper-capability]")
}
func writeCapability(w io.Writer, c ClaimCapability) error {
	written := 0
	for written < len(c.secret) {
		n, err := w.Write(c.secret[written:])
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		written += n
	}
	return nil
}
func readCapability(r io.Reader) (ClaimCapability, error) {
	var c ClaimCapability
	if _, err := io.ReadFull(r, c.secret[:]); err != nil {
		return c, fmt.Errorf("read context helper capability")
	}
	return c, nil
}
func ClaimVerifierDigest(c ClaimCapability) string {
	sum := sha256.Sum256(c.secret[:])
	return hex.EncodeToString(sum[:])
}

func NewClaimChallenge() (string, error) {
	raw := make([]byte, ClaimChallengeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate context helper challenge")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type ClaimIdentity struct {
	OpaqueLaunchID     string                   `json:"opaque_launch_id"`
	ContextExecID      string                   `json:"context_exec_id"`
	SessionID          string                   `json:"session_id"`
	AuthorityEpoch     delegated.AuthorityEpoch `json:"authority_epoch"`
	Generation         string                   `json:"generation"`
	RequestFingerprint string                   `json:"request_fingerprint"`
}

func (v ClaimIdentity) Validate() error {
	if !validOpaque(v.OpaqueLaunchID, core.MaxOpaqueRefBytes) || !validOpaque(v.ContextExecID, core.MaxContextExecIDBytes) || !validOpaque(v.SessionID, core.MaxSessionIDBytes) || !validOpaque(v.Generation, core.MaxOpaqueRefBytes) || !validSHA256(v.RequestFingerprint) {
		return fmt.Errorf("invalid context helper claim identity")
	}
	return v.AuthorityEpoch.Validate()
}
func ClaimProof(c ClaimCapability, id ClaimIdentity, challenge string) (string, error) {
	payload, err := claimPayload(id, challenge)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, c.secret[:])
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func VerifyClaimProof(c ClaimCapability, id ClaimIdentity, challenge, proof string) bool {
	expected, err := ClaimProof(c, id, challenge)
	return err == nil && hmac.Equal([]byte(expected), []byte(proof))
}
func claimPayload(id ClaimIdentity, challenge string) ([]byte, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if !validChallenge(challenge) {
		return nil, fmt.Errorf("invalid context helper challenge")
	}
	return []byte(fmt.Sprintf("shellbeam-context-helper-v%d\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s", ProtocolVersion, id.OpaqueLaunchID, id.ContextExecID, id.SessionID, id.AuthorityEpoch, id.Generation, id.RequestFingerprint, challenge)), nil
}
func validChallenge(v string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(raw) == ClaimChallengeBytes
}
func validProof(v string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(raw) == sha256.Size
}
func validSHA256(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range []byte(v) {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func validOpaque(v string, max int) bool {
	if v == "" || len(v) > max {
		return false
	}
	for _, c := range []byte(v) {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("_-.:", rune(c))) {
			return false
		}
	}
	return true
}
