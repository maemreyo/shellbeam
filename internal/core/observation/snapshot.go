package observation

import (
	"fmt"
	"unicode"
)

const (
	MaxSnapshotFacts     = 64
	MaxSnapshotFactCode  = 128
	MaxSnapshotFactValue = 1024
)

type Snapshot struct {
	SchemaVersion      int            `json:"schema_version"`
	Target             Target         `json:"target"`
	CapturedThroughSeq ChangeSeq      `json:"captured_through_seq"`
	Facts              []SnapshotFact `json:"facts,omitempty"`
	Truncated          bool           `json:"truncated"`
}

type SnapshotFact struct {
	Code  string `json:"code"`
	Value string `json:"value"`
}

func (s Snapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("invalid observation snapshot schema")
	}
	if err := s.Target.Validate(); err != nil {
		return err
	}
	if len(s.Facts) > MaxSnapshotFacts {
		return fmt.Errorf("observation snapshot facts exceed limit")
	}
	for _, fact := range s.Facts {
		if err := fact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (f SnapshotFact) Validate() error {
	if !safeSnapshotText(f.Code, MaxSnapshotFactCode) || !safeSnapshotText(f.Value, MaxSnapshotFactValue) {
		return fmt.Errorf("invalid observation snapshot fact")
	}
	return nil
}

func safeSnapshotText(v string, max int) bool {
	if v == "" || len(v) > max {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
