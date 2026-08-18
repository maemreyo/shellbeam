package hermetic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	RequestVersionV1          = 1
	MaxRepoInputs             = 256
	MaxRepoInputSelectorBytes = 1024
	MaxRepoInputTotalBytes    = 64 << 10
)

type Mode string

type NetworkMode string

type EnvironmentMode string

type StdinMode string

type WritesMode string

const (
	ModeRequired Mode = "required"

	NetworkOff NetworkMode = "off"

	EnvironmentFixedAllowlist EnvironmentMode = "fixed_allowlist"

	StdinClosed StdinMode = "closed"

	WritesEphemeralDiscard WritesMode = "ephemeral_discard"
)

type Request struct {
	Version     int             `json:"version"`
	Mode        Mode            `json:"mode"`
	RepoInputs  []string        `json:"repo_inputs"`
	Network     NetworkMode     `json:"network"`
	Environment EnvironmentMode `json:"environment"`
	Stdin       StdinMode       `json:"stdin"`
	Writes      WritesMode      `json:"writes"`
}

func (r Request) Validate() error {
	if r.Version != RequestVersionV1 {
		return fmt.Errorf("unsupported hermetic request version")
	}
	if r.Mode != ModeRequired {
		return fmt.Errorf("unsupported hermetic mode")
	}
	if r.Network != NetworkOff || r.Environment != EnvironmentFixedAllowlist || r.Stdin != StdinClosed || r.Writes != WritesEphemeralDiscard {
		return fmt.Errorf("unsupported hermetic v1 boundary semantics")
	}
	_, err := normalizeRepoInputs(r.RepoInputs)
	return err
}

func (r *Request) Clone() *Request {
	if r == nil {
		return nil
	}
	copy := *r
	copy.RepoInputs = append([]string(nil), r.RepoInputs...)
	return &copy
}

func (r Request) Canonical() (Request, error) {
	if err := r.Validate(); err != nil {
		return Request{}, err
	}
	out := r
	inputs, err := normalizeRepoInputs(r.RepoInputs)
	if err != nil {
		return Request{}, err
	}
	out.RepoInputs = inputs
	return out, nil
}

func BindFingerprint(kind, base string, request *Request) (string, error) {
	if request == nil {
		return base, nil
	}
	if kind != "request" && kind != "execution" {
		return "", fmt.Errorf("invalid hermetic fingerprint kind")
	}
	if !validSHA256(base) {
		return "", fmt.Errorf("invalid base fingerprint")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Version  int     `json:"version"`
		Kind     string  `json:"kind"`
		Base     string  `json:"base_fingerprint"`
		Hermetic Request `json:"hermetic"`
	}{Version: 1, Kind: kind, Base: base, Hermetic: canonical})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeRepoInputs(inputs []string) ([]string, error) {
	if len(inputs) == 0 || len(inputs) > MaxRepoInputs {
		return nil, fmt.Errorf("hermetic repo input count out of bounds")
	}
	total := 0
	seen := make(map[string]struct{}, len(inputs))
	out := make([]string, 0, len(inputs))
	for _, selector := range inputs {
		if err := validateRepoInputSelector(selector); err != nil {
			return nil, err
		}
		total += len(selector)
		if total > MaxRepoInputTotalBytes {
			return nil, fmt.Errorf("hermetic repo input selector budget exceeded")
		}
		if _, exists := seen[selector]; exists {
			return nil, fmt.Errorf("duplicate hermetic repo input selector")
		}
		seen[selector] = struct{}{}
		out = append(out, selector)
	}
	sort.Strings(out)
	return out, nil
}

func validateRepoInputSelector(selector string) error {
	if selector == "" || len(selector) > MaxRepoInputSelectorBytes || !utf8.ValidString(selector) {
		return fmt.Errorf("invalid hermetic repo input selector")
	}
	if strings.HasPrefix(selector, "/") || strings.Contains(selector, "\\") || path.Clean(selector) != selector || selector == "." || selector == ".." || strings.HasPrefix(selector, "../") || strings.Contains(selector, "/../") {
		return fmt.Errorf("invalid hermetic repo input selector")
	}
	for _, r := range selector {
		if r == 0 || unicode.IsControl(r) {
			return fmt.Errorf("invalid hermetic repo input selector")
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
