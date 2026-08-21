package structuredresult

import "fmt"

const MaxObservedEntries = 65536

type CompletenessReason string

const (
	CompletenessReasonPassRecordsElided CompletenessReason = "pass_records_elided"
	CompletenessReasonZeroMatch         CompletenessReason = "zero_match"
)

type ObservedEntryCounts struct {
	Namespace         string `json:"namespace"`
	VocabularyVersion int    `json:"vocabulary_version"`
	Files             int    `json:"files"`
	Entries           int    `json:"entries"`
	Pass              int    `json:"pass"`
	Fail              int    `json:"fail"`
	Skip              int    `json:"skip"`
	Error             int    `json:"error"`
}

func (c ObservedEntryCounts) Validate() error {
	if !safeStructuredText(c.Namespace, 128) || c.VocabularyVersion != 1 {
		return fmt.Errorf("invalid observed entry vocabulary")
	}
	counts := [...]int{c.Files, c.Entries, c.Pass, c.Fail, c.Skip, c.Error}
	for _, count := range counts {
		if count < 0 || count > MaxObservedEntries {
			return fmt.Errorf("invalid observed entry count")
		}
	}
	if c.Pass+c.Fail+c.Skip+c.Error != c.Entries {
		return fmt.Errorf("observed entry status counts do not partition entries")
	}
	return nil
}

func (r CompletenessReason) Validate() error {
	switch r {
	case "", CompletenessReasonPassRecordsElided, CompletenessReasonZeroMatch:
		return nil
	default:
		return fmt.Errorf("invalid completeness reason")
	}
}

func validCompletenessReason(v CompletenessReason) bool { return v.Validate() == nil }
