package outputview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	MaxReturnBytes       = 64 << 10
	MaxWorkBytes         = 1 << 20
	MaxLines             = 512
	MaxMatches           = 128
	MaxPatternBytes      = 4096
	MaxContinuationBytes = 2048
)

type SelectorKind string

type SearchMode string

type RetentionState string

const (
	SelectorRawRange SelectorKind = "raw_range"
	SelectorTail     SelectorKind = "tail"
	SelectorLines    SelectorKind = "lines"
	SelectorPreview  SelectorKind = "preview"
	SelectorSearch   SelectorKind = "search"

	SearchLiteral SearchMode = "literal"
	SearchRegex   SearchMode = "regex"

	RetentionRetained    RetentionState = "retained"
	RetentionCompacted   RetentionState = "compacted"
	RetentionUnavailable RetentionState = "unavailable"
)

type Request struct {
	SessionID    string   `json:"session_id"`
	Selector     Selector `json:"selector"`
	Continuation string   `json:"continuation,omitempty"`
}

type Selector struct {
	Kind          SelectorKind `json:"kind"`
	StartByte     int64        `json:"start_byte,omitempty"`
	MaxBytes      int          `json:"max_bytes,omitempty"`
	TailBytes     int          `json:"bytes,omitempty"`
	TailLines     int          `json:"lines,omitempty"`
	StartLine     int          `json:"start_line,omitempty"`
	MaxLines      int          `json:"max_lines,omitempty"`
	HeadBytes     int          `json:"head_bytes,omitempty"`
	SearchMode    SearchMode   `json:"mode,omitempty"`
	Pattern       string       `json:"pattern,omitempty"`
	CaseSensitive bool         `json:"case_sensitive,omitempty"`
	MaxMatches    int          `json:"max_matches,omitempty"`
}

type Extent struct {
	SessionID string         `json:"session_id"`
	State     RetentionState `json:"retention_state"`
	Bytes     int64          `json:"retained_bytes"`
	Terminal  bool           `json:"terminal"`
}

type RawRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type Match struct {
	Line      int      `json:"line,omitempty"`
	RawRange  RawRange `json:"raw_range"`
	Excerpt   string   `json:"excerpt"`
	Truncated bool     `json:"truncated,omitempty"`
}

type Result struct {
	SchemaVersion  int            `json:"schema_version"`
	SessionID      string         `json:"session_id"`
	SelectorKind   SelectorKind   `json:"selector_kind"`
	RetentionState RetentionState `json:"retention_state"`
	FrozenCutBytes int64          `json:"frozen_cut_bytes"`
	Ranges         []RawRange     `json:"raw_ranges,omitempty"`
	Text           string         `json:"text,omitempty"`
	Matches        []Match        `json:"matches,omitempty"`
	Partial        bool           `json:"partial,omitempty"`
	Truncated      bool           `json:"truncated,omitempty"`
	Continuation   string         `json:"continuation,omitempty"`
}

func (r Request) Validate() error {
	if r.SessionID == "" {
		return fmt.Errorf("session id missing")
	}
	if len(r.Continuation) > MaxContinuationBytes {
		return fmt.Errorf("continuation exceeds limit")
	}
	if err := r.Selector.Validate(); err != nil {
		return err
	}
	if r.Continuation != "" && r.Selector.Kind != SelectorLines && r.Selector.Kind != SelectorTail && r.Selector.Kind != SelectorSearch {
		return fmt.Errorf("selector does not accept continuation")
	}
	return nil
}

func (s Selector) Validate() error {
	if s.StartByte < 0 || s.MaxBytes < 0 || s.TailBytes < 0 || s.TailLines < 0 || s.StartLine < 0 || s.MaxLines < 0 || s.HeadBytes < 0 || s.MaxMatches < 0 {
		return fmt.Errorf("selector contains negative value")
	}
	switch s.Kind {
	case SelectorRawRange:
		if s.MaxBytes < 1 || s.MaxBytes > MaxReturnBytes || s.hasTail() || s.StartLine != 0 || s.MaxLines != 0 || s.HeadBytes != 0 || s.hasSearch() {
			return fmt.Errorf("invalid raw range selector")
		}
	case SelectorTail:
		if (s.TailBytes == 0) == (s.TailLines == 0) || s.TailBytes > MaxReturnBytes || s.TailLines > MaxLines || s.StartByte != 0 || s.MaxBytes != 0 || s.StartLine != 0 || s.MaxLines != 0 || s.HeadBytes != 0 || s.hasSearch() {
			return fmt.Errorf("invalid tail selector")
		}
	case SelectorLines:
		if s.StartLine < 1 || s.MaxLines < 1 || s.MaxLines > MaxLines || s.StartByte != 0 || s.MaxBytes != 0 || s.hasTail() || s.HeadBytes != 0 || s.hasSearch() {
			return fmt.Errorf("invalid lines selector")
		}
	case SelectorPreview:
		if s.HeadBytes+s.TailBytes < 1 || s.HeadBytes+s.TailBytes > MaxReturnBytes || s.StartByte != 0 || s.MaxBytes != 0 || s.TailLines != 0 || s.StartLine != 0 || s.MaxLines != 0 || s.hasSearch() {
			return fmt.Errorf("invalid preview selector")
		}
	case SelectorSearch:
		if (s.SearchMode != SearchLiteral && s.SearchMode != SearchRegex) || s.Pattern == "" || len(s.Pattern) > MaxPatternBytes || s.MaxMatches < 1 || s.MaxMatches > MaxMatches || s.StartByte != 0 || s.MaxBytes != 0 || s.hasTail() || s.StartLine != 0 || s.MaxLines != 0 || s.HeadBytes != 0 {
			return fmt.Errorf("invalid search selector")
		}
	default:
		return fmt.Errorf("unknown selector kind")
	}
	return nil
}

func (s Selector) Fingerprint() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s Selector) hasTail() bool { return s.TailBytes != 0 || s.TailLines != 0 }
func (s Selector) hasSearch() bool {
	return s.SearchMode != "" || s.Pattern != "" || s.CaseSensitive || s.MaxMatches != 0
}
