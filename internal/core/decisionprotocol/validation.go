package decisionprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func boundedToken(v string, max int) bool {
	if v == "" || len(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validTime(v time.Time) bool { return !v.IsZero() }

func validHex64(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func validDerived(v, prefix string) bool {
	return strings.HasPrefix(v, prefix) && validHex64(strings.TrimPrefix(v, prefix))
}
func validGeneration(v string) bool  { return validDerived(v, "gen_") }
func validFingerprint(v string) bool { return validHex64(v) }

func uniqueStrings(values []string, maxCount, maxLen int, allowEmpty bool) error {
	if len(values) > maxCount {
		return fmt.Errorf("too many values")
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" && allowEmpty {
			continue
		}
		if !boundedToken(value, maxLen) {
			return fmt.Errorf("invalid value")
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate value")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func canonicalHash(prefix string, value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return prefix + hex.EncodeToString(sum[:]), nil
}

func sortedCandidateIDs(values []CandidateID) []CandidateID {
	out := append([]CandidateID(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func validID[T ~string](v T) bool { return boundedToken(string(v), 192) }

func ParseEpisodeID(v string) (EpisodeID, error) {
	if !boundedToken(v, 192) {
		return "", fmt.Errorf("invalid episode id")
	}
	return EpisodeID(v), nil
}
func ParseCandidateID(v string) (CandidateID, error) {
	if !boundedToken(v, 192) {
		return "", fmt.Errorf("invalid candidate id")
	}
	return CandidateID(v), nil
}
func ParseExperimentID(v string) (ExperimentID, error) {
	if !boundedToken(v, 192) {
		return "", fmt.Errorf("invalid experiment id")
	}
	return ExperimentID(v), nil
}
func ParsePredictionID(v string) (PredictionID, error) {
	if !boundedToken(v, 192) {
		return "", fmt.Errorf("invalid prediction id")
	}
	return PredictionID(v), nil
}
