//go:build linux || darwin

package supervisor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

const (
	TerminalRecordSchemaVersion = 1
	maxTerminalRecordBytes      = 32 << 10
)

type TerminalRecord struct {
	SchemaVersion       int                    `json:"schema_version"`
	ProtocolVersion     int                    `json:"protocol_version"`
	SessionID           string                 `json:"session_id"`
	GenerationID        string                 `json:"generation_id"`
	State               session.State          `json:"state"`
	Outcome             session.Outcome        `json:"outcome"`
	Spawn               receipt.SpawnEvidence  `json:"spawn_evidence"`
	Exit                receipt.ExitEvidence   `json:"exit_evidence"`
	Signal              receipt.SignalEvidence `json:"signal_evidence"`
	TimedOut            bool                   `json:"timed_out"`
	OutputBytes         int64                  `json:"output_bytes"`
	OutputComplete      bool                   `json:"output_complete"`
	InputAcceptedBytes  int64                  `json:"input_accepted_bytes"`
	InputDeliveredBytes int64                  `json:"input_delivered_bytes"`
	StdinClosed         bool                   `json:"stdin_closed"`
	FailureReason       string                 `json:"failure_reason,omitempty"`
	Integrity           string                 `json:"integrity"`
}

func SealTerminalRecord(capability Capability, record TerminalRecord) (TerminalRecord, error) {
	record.Integrity = ""
	if err := validateTerminalRecordBody(record); err != nil {
		return TerminalRecord{}, terminalFailure("invalid_record")
	}
	body, err := terminalRecordBody(record)
	if err != nil {
		return TerminalRecord{}, terminalFailure("encode")
	}
	mac := hmac.New(sha256.New, capability.secret[:])
	_, _ = mac.Write(body)
	record.Integrity = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return record, nil
}

func VerifyTerminalRecord(capability Capability, record TerminalRecord, sessionID, generationID string) error {
	if record.SessionID != sessionID || record.GenerationID != generationID {
		return terminalFailure("identity")
	}
	if _, err := operation.ParseSessionID(sessionID); err != nil || !validOpaque(generationID) {
		return terminalFailure("identity")
	}
	integrity, err := base64.RawURLEncoding.DecodeString(record.Integrity)
	if err != nil || len(integrity) != sha256.Size {
		return terminalFailure("integrity")
	}
	bodyRecord := record
	bodyRecord.Integrity = ""
	if err := validateTerminalRecordBody(bodyRecord); err != nil {
		return terminalFailure("invalid_record")
	}
	body, err := terminalRecordBody(bodyRecord)
	if err != nil {
		return terminalFailure("encode")
	}
	mac := hmac.New(sha256.New, capability.secret[:])
	_, _ = mac.Write(body)
	if !hmac.Equal(integrity, mac.Sum(nil)) {
		return failure.New(failure.SupervisorAuthFailed, map[string]string{"session_id": sessionID, "reason": "terminal_integrity"}, fmt.Errorf("supervisor terminal integrity failed"))
	}
	return nil
}

func WriteTerminalRecord(layout Layout, record TerminalRecord) error {
	if err := validateLayout(layout); err != nil {
		return err
	}
	metadata, err := LoadMetadata(layout)
	if err != nil {
		return err
	}
	if record.SessionID != filepath.Base(layout.SessionDir) || record.SessionID != metadata.SessionID || record.GenerationID != metadata.GenerationID || record.Integrity == "" {
		return terminalFailure("identity")
	}
	encoded, err := json.Marshal(record)
	if err != nil || len(encoded)+1 > maxTerminalRecordBytes {
		return terminalFailure("encode")
	}
	data := append(encoded, '\n')
	if err := createOrMatchPrivateFile(layout.TerminalPath, data); err != nil {
		return terminalFailure("write")
	}
	return nil
}

func LoadTerminalRecord(layout Layout, capability Capability, sessionID, generationID string) (TerminalRecord, error) {
	if err := validateLayout(layout); err != nil {
		return TerminalRecord{}, err
	}
	raw, err := readPrivateFile(layout.TerminalPath, 2, maxTerminalRecordBytes)
	if err != nil {
		return TerminalRecord{}, terminalFailure("read")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record TerminalRecord
	if err := decoder.Decode(&record); err != nil {
		return TerminalRecord{}, terminalFailure("decode")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return TerminalRecord{}, terminalFailure("decode")
	}
	if err := VerifyTerminalRecord(capability, record, sessionID, generationID); err != nil {
		return TerminalRecord{}, err
	}
	return record, nil
}

func validateTerminalRecordBody(record TerminalRecord) error {
	if record.SchemaVersion != TerminalRecordSchemaVersion || record.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("invalid terminal record schema")
	}
	if _, err := operation.ParseSessionID(record.SessionID); err != nil || !validOpaque(record.GenerationID) {
		return fmt.Errorf("invalid terminal record identity")
	}
	if record.OutputBytes < 0 || record.InputAcceptedBytes < 0 || record.InputDeliveredBytes < 0 || record.InputDeliveredBytes > record.InputAcceptedBytes {
		return fmt.Errorf("invalid terminal record outcome")
	}
	switch record.State {
	case session.Completed:
		if record.Outcome != session.Success || record.TimedOut {
			return fmt.Errorf("invalid completed outcome")
		}
	case session.Failed:
		if record.Outcome != session.Failure || record.TimedOut {
			return fmt.Errorf("invalid failed outcome")
		}
	case session.TimedOut:
		if record.Outcome != session.Timeout || !record.TimedOut {
			return fmt.Errorf("invalid timeout outcome")
		}
	case session.Killed:
		if record.Outcome != session.KilledOutcome || record.TimedOut {
			return fmt.Errorf("invalid killed outcome")
		}
	default:
		return fmt.Errorf("invalid supervisor terminal state")
	}
	if record.State == session.Completed {
		if !record.Spawn.Attempted || !record.Spawn.Succeeded || !record.Exit.Reaped || record.Exit.Code == nil || *record.Exit.Code != 0 || !record.OutputComplete || record.InputAcceptedBytes != record.InputDeliveredBytes {
			return fmt.Errorf("successful terminal record lacks evidence")
		}
	}
	return nil
}

func terminalRecordBody(record TerminalRecord) ([]byte, error) {
	if record.Integrity != "" {
		return nil, fmt.Errorf("integrity must be excluded from terminal body")
	}
	return json.Marshal(record)
}

func terminalFailure(reason string) error {
	return failure.New(failure.SupervisorStateConflict, map[string]string{"reason": reason}, fmt.Errorf("invalid supervisor terminal record"))
}
