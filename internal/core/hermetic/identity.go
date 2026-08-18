package hermetic

import (
	"fmt"
	"regexp"
)

const (
	ProviderBubblewrap    = "bubblewrap"
	BubblewrapVersionV1   = "0.11.2"
	maxIdentityIDBytes    = 128
	maxSecurityPolicyByte = 256
)

var identityIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$`)

type ProviderIdentity struct {
	Provider              string `json:"provider"`
	Version               string `json:"version"`
	BinarySHA256          string `json:"binary_sha256"`
	RuntimeManifestSHA256 string `json:"runtime_manifest_sha256"`
	SecurityPolicyID      string `json:"security_policy_id,omitempty"`
	SecurityPolicySHA256  string `json:"security_policy_sha256,omitempty"`
}

func (i ProviderIdentity) Validate() error {
	if i.Provider != ProviderBubblewrap || i.Version != BubblewrapVersionV1 {
		return fmt.Errorf("unsupported hermetic provider identity")
	}
	if !validSHA256(i.BinarySHA256) || !validSHA256(i.RuntimeManifestSHA256) {
		return fmt.Errorf("invalid hermetic provider digest")
	}
	if (i.SecurityPolicyID == "") != (i.SecurityPolicySHA256 == "") {
		return fmt.Errorf("incomplete hermetic security policy identity")
	}
	if i.SecurityPolicyID != "" {
		if len(i.SecurityPolicyID) > maxSecurityPolicyByte || !identityIDPattern.MatchString(i.SecurityPolicyID) || !validSHA256(i.SecurityPolicySHA256) {
			return fmt.Errorf("invalid hermetic security policy identity")
		}
	}
	return nil
}

type ToolchainIdentity struct {
	ID             string `json:"id"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

func (i ToolchainIdentity) Validate() error {
	if i.ID == "" || len(i.ID) > maxIdentityIDBytes || !identityIDPattern.MatchString(i.ID) || !validSHA256(i.ManifestSHA256) {
		return fmt.Errorf("invalid hermetic toolchain identity")
	}
	return nil
}
