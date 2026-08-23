package structuredresult

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	MaxProducerSemanticsClaims = 32
	maxProducerNamespaceBytes  = 64
	maxProducerCodeBytes       = 128
	maxProducerAddressBytes    = 1024
	maxArtifactEntryOrdinal    = 1 << 20
)

type ProducerTestDisposition struct {
	Namespace         string `json:"namespace"`
	VocabularyVersion int    `json:"vocabulary_version"`
	Code              string `json:"code"`
}

type ProducerSemanticsCoverage struct {
	Namespace              string   `json:"namespace"`
	VocabularyVersion      int      `json:"vocabulary_version"`
	Format                 string   `json:"format"`
	Family                 string   `json:"family"`
	MechanicallyObservable []string `json:"mechanically_observable,omitempty"`
	Unavailable            []string `json:"unavailable,omitempty"`
}

type ArtifactTestEntryRef struct {
	ArtifactBlobID  string `json:"artifact_blob_id"`
	SuiteOrdinal    int    `json:"suite_ordinal"`
	TestcaseOrdinal int    `json:"testcase_ordinal"`
}

type ProducerTestAddress struct {
	Namespace         string `json:"namespace"`
	VocabularyVersion int    `json:"vocabulary_version"`
	SuiteName         string `json:"suite_name,omitempty"`
	Classname         string `json:"classname,omitempty"`
	Name              string `json:"name"`
}

type TestSuiteAggregate struct {
	Tests    int `json:"tests"`
	Failures int `json:"failures"`
	Errors   int `json:"errors"`
	Skipped  int `json:"skipped"`
}

func (d ProducerTestDisposition) Validate() error {
	if !validProducerEnvelope(d.Namespace, d.VocabularyVersion) || !safeStructuredText(d.Code, maxProducerCodeBytes) {
		return fmt.Errorf("invalid producer test disposition")
	}
	return nil
}

func (c ProducerSemanticsCoverage) Validate() error {
	if !validProducerEnvelope(c.Namespace, c.VocabularyVersion) || !safeStructuredText(c.Format, maxProducerCodeBytes) || !safeStructuredText(c.Family, maxProducerCodeBytes) {
		return fmt.Errorf("invalid producer semantics coverage")
	}
	if !validClaimSet(c.MechanicallyObservable) || !validClaimSet(c.Unavailable) {
		return fmt.Errorf("invalid producer semantics claims")
	}
	seen := make(map[string]struct{}, len(c.MechanicallyObservable))
	for _, claim := range c.MechanicallyObservable {
		seen[claim] = struct{}{}
	}
	for _, claim := range c.Unavailable {
		if _, ok := seen[claim]; ok {
			return fmt.Errorf("producer semantic claim has contradictory coverage")
		}
	}
	return nil
}

func (r ArtifactTestEntryRef) Validate() error {
	if !validArtifactBlobID(r.ArtifactBlobID) || r.SuiteOrdinal < 0 || r.TestcaseOrdinal < 0 || r.SuiteOrdinal > maxArtifactEntryOrdinal || r.TestcaseOrdinal > maxArtifactEntryOrdinal {
		return fmt.Errorf("invalid artifact test entry")
	}
	return nil
}

func ArtifactTestRecordID(derivationKey string, ref ArtifactTestEntryRef) (string, error) {
	if !validDigest(derivationKey) || ref.Validate() != nil {
		return "", fmt.Errorf("invalid artifact record identity")
	}
	encoded, err := json.Marshal(struct {
		Version       int                  `json:"version"`
		DerivationKey string               `json:"derivation_key"`
		Kind          string               `json:"kind"`
		Entry         ArtifactTestEntryRef `json:"entry"`
	}{1, derivationKey, "testcase", ref})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (a ProducerTestAddress) Validate() error {
	if !validProducerEnvelope(a.Namespace, a.VocabularyVersion) || !safeStructuredText(a.Name, maxProducerAddressBytes) {
		return fmt.Errorf("invalid producer test address")
	}
	for _, optional := range []string{a.SuiteName, a.Classname} {
		if optional != "" && !safeStructuredText(optional, maxProducerAddressBytes) {
			return fmt.Errorf("invalid producer test address")
		}
	}
	return nil
}

func (a TestSuiteAggregate) Validate() error {
	for _, value := range []int{a.Tests, a.Failures, a.Errors, a.Skipped} {
		if value < 0 || value > maxArtifactEntryOrdinal {
			return fmt.Errorf("invalid test suite aggregate")
		}
	}
	return nil
}

func validProducerEnvelope(namespace string, version int) bool {
	return safeStructuredText(namespace, maxProducerNamespaceBytes) && version >= 1
}

func validClaimSet(values []string) bool {
	if len(values) > MaxProducerSemanticsClaims {
		return false
	}
	previous := ""
	for _, value := range values {
		if !safeStructuredText(value, maxProducerCodeBytes) || previous != "" && value <= previous {
			return false
		}
		previous = value
	}
	return true
}
