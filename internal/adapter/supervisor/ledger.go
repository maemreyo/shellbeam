//go:build linux || darwin

package supervisor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const ledgerSchemaVersion = 1

type InputLimits struct {
	MaxRecords       int
	MaxMetadataBytes int
	MaxQueuedBytes   int
}

type InputRecord struct {
	Kind      string `json:"kind"`
	Offset    int64  `json:"offset"`
	Length    int    `json:"length"`
	SHA256    string `json:"sha256,omitempty"`
	Delivered bool   `json:"delivered"`
}

type InputAdmission struct {
	Record        InputRecord
	AcceptedBytes int
	NextOffset    int64
	Duplicate     bool
	NeedsDelivery bool
}

type InputSnapshot struct {
	NextOffset     int64
	AcceptedBytes  int64
	DeliveredBytes int64
	QueuedBytes    int
	EOFAccepted    bool
	EOFDelivered   bool
	Records        int
}

type inputLedgerState struct {
	SchemaVersion  int           `json:"schema_version"`
	NextOffset     int64         `json:"next_offset"`
	AcceptedBytes  int64         `json:"accepted_bytes"`
	DeliveredBytes int64         `json:"delivered_bytes"`
	QueuedBytes    int           `json:"queued_bytes"`
	EOFAccepted    bool          `json:"eof_accepted"`
	EOFDelivered   bool          `json:"eof_delivered"`
	Records        []InputRecord `json:"records"`
}

type InputLedger struct {
	mu     sync.Mutex
	path   string
	limits InputLimits
	state  inputLedgerState
}

func OpenInputLedger(layout Layout, limits InputLimits) (*InputLedger, error) {
	if err := validateLayout(layout); err != nil {
		return nil, err
	}
	if limits.MaxRecords < 1 || limits.MaxMetadataBytes < 256 || limits.MaxQueuedBytes < 1 {
		return nil, fmt.Errorf("invalid persistent input limits")
	}
	path := filepath.Join(layout.SessionDir, "input-ledger.json")
	state := inputLedgerState{SchemaVersion: ledgerSchemaVersion, Records: []InputRecord{}}
	err := loadPrivateJSON(path, limits.MaxMetadataBytes, &state)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, io.EOF) {
		if err := createPrivateJSON(path, state, limits.MaxMetadataBytes); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := validateInputLedgerState(state, limits); err != nil {
		return nil, err
	}
	return &InputLedger{path: path, limits: limits, state: state}, nil
}

func (l *InputLedger) AcceptChars(offset int64, data []byte) (InputAdmission, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(data) == 0 {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("empty input")
	}
	hash := sha256.Sum256(data)
	want := InputRecord{Kind: "chars", Offset: offset, Length: len(data), SHA256: hex.EncodeToString(hash[:])}
	if existing, ok := inputRecordAt(l.state.Records, offset); ok {
		if sameInputIdentity(existing, want) {
			return InputAdmission{Record: existing, NextOffset: l.state.NextOffset, Duplicate: true, NeedsDelivery: !existing.Delivered}, nil
		}
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_conflict")
	}
	if l.state.EOFAccepted {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_closed")
	}
	if offset < l.state.NextOffset {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_conflict")
	}
	if offset > l.state.NextOffset {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_gap")
	}
	if l.state.QueuedBytes+len(data) > l.limits.MaxQueuedBytes {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_backpressure")
	}
	if len(l.state.Records) >= l.limits.MaxRecords {
		return InputAdmission{NextOffset: l.state.NextOffset}, inputHistoryFailure()
	}
	next := cloneInputState(l.state)
	next.Records = append(next.Records, want)
	next.NextOffset += int64(len(data))
	next.AcceptedBytes += int64(len(data))
	next.QueuedBytes += len(data)
	if err := l.commit(next); err != nil {
		return InputAdmission{NextOffset: l.state.NextOffset}, err
	}
	return InputAdmission{Record: want, AcceptedBytes: len(data), NextOffset: next.NextOffset, NeedsDelivery: true}, nil
}

func (l *InputLedger) AcceptEOF(offset int64) (InputAdmission, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	want := InputRecord{Kind: "eof", Offset: offset}
	if existing, ok := inputRecordAt(l.state.Records, offset); ok {
		if sameInputIdentity(existing, want) {
			return InputAdmission{Record: existing, NextOffset: l.state.NextOffset, Duplicate: true, NeedsDelivery: !existing.Delivered}, nil
		}
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_conflict")
	}
	if l.state.EOFAccepted {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_closed")
	}
	if offset < l.state.NextOffset {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_conflict")
	}
	if offset > l.state.NextOffset {
		return InputAdmission{NextOffset: l.state.NextOffset}, fmt.Errorf("input_gap")
	}
	if len(l.state.Records) >= l.limits.MaxRecords {
		return InputAdmission{NextOffset: l.state.NextOffset}, inputHistoryFailure()
	}
	next := cloneInputState(l.state)
	next.Records = append(next.Records, want)
	next.EOFAccepted = true
	if err := l.commit(next); err != nil {
		return InputAdmission{NextOffset: l.state.NextOffset}, err
	}
	return InputAdmission{Record: want, NextOffset: next.NextOffset, NeedsDelivery: true}, nil
}

func (l *InputLedger) MarkDelivered(record InputRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	index := -1
	for i := range l.state.Records {
		if l.state.Records[i].Offset == record.Offset {
			index = i
			break
		}
	}
	if index < 0 || !sameInputIdentity(l.state.Records[index], record) {
		return fmt.Errorf("input_conflict")
	}
	if l.state.Records[index].Delivered {
		return nil
	}
	next := cloneInputState(l.state)
	next.Records[index].Delivered = true
	if next.Records[index].Kind == "chars" {
		next.DeliveredBytes += int64(next.Records[index].Length)
		next.QueuedBytes -= next.Records[index].Length
	} else {
		next.EOFDelivered = true
	}
	return l.commit(next)
}

func (l *InputLedger) Snapshot() InputSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return InputSnapshot{
		NextOffset: l.state.NextOffset, AcceptedBytes: l.state.AcceptedBytes, DeliveredBytes: l.state.DeliveredBytes,
		QueuedBytes: l.state.QueuedBytes, EOFAccepted: l.state.EOFAccepted, EOFDelivered: l.state.EOFDelivered, Records: len(l.state.Records),
	}
}

func (l *InputLedger) commit(next inputLedgerState) error {
	if err := validateInputLedgerState(next, l.limits); err != nil {
		return err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if len(encoded)+1 > l.limits.MaxMetadataBytes {
		return inputHistoryFailure()
	}
	if err := replacePrivateJSON(l.path, next, l.limits.MaxMetadataBytes); err != nil {
		return err
	}
	l.state = next
	return nil
}

func validateInputLedgerState(state inputLedgerState, limits InputLimits) error {
	if state.SchemaVersion != ledgerSchemaVersion || state.NextOffset < 0 || state.AcceptedBytes < 0 || state.DeliveredBytes < 0 || state.DeliveredBytes > state.AcceptedBytes || state.QueuedBytes < 0 || state.QueuedBytes > limits.MaxQueuedBytes || len(state.Records) > limits.MaxRecords {
		return fmt.Errorf("invalid persistent input ledger")
	}
	var cursor, accepted, delivered int64
	queued := 0
	eofAccepted, eofDelivered := false, false
	for i, record := range state.Records {
		if record.Offset != cursor || record.Delivered && record.Kind == "eof" && !state.EOFAccepted {
			return fmt.Errorf("invalid persistent input ledger")
		}
		switch record.Kind {
		case "chars":
			if record.Length < 1 || !validDigest(record.SHA256) || eofAccepted {
				return fmt.Errorf("invalid persistent input ledger")
			}
			cursor += int64(record.Length)
			accepted += int64(record.Length)
			if record.Delivered {
				delivered += int64(record.Length)
			} else {
				queued += record.Length
			}
		case "eof":
			if record.Length != 0 || record.SHA256 != "" || eofAccepted || i != len(state.Records)-1 {
				return fmt.Errorf("invalid persistent input ledger")
			}
			eofAccepted = true
			eofDelivered = record.Delivered
		default:
			return fmt.Errorf("invalid persistent input ledger")
		}
	}
	if cursor != state.NextOffset || accepted != state.AcceptedBytes || delivered != state.DeliveredBytes || queued != state.QueuedBytes || eofAccepted != state.EOFAccepted || eofDelivered != state.EOFDelivered {
		return fmt.Errorf("invalid persistent input ledger")
	}
	return nil
}

func inputRecordAt(records []InputRecord, offset int64) (InputRecord, bool) {
	for _, record := range records {
		if record.Offset == offset {
			return record, true
		}
	}
	return InputRecord{}, false
}

func sameInputIdentity(a, b InputRecord) bool {
	return a.Kind == b.Kind && a.Offset == b.Offset && a.Length == b.Length && a.SHA256 == b.SHA256
}

func cloneInputState(state inputLedgerState) inputLedgerState {
	state.Records = append([]InputRecord(nil), state.Records...)
	return state
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func inputHistoryFailure() error {
	return failure.New(failure.PersistentInputHistoryExhausted, map[string]string{"reason": "record_limit"}, fmt.Errorf("persistent input history exhausted"))
}

type KillRecord struct {
	KillID    string `json:"kill_id"`
	Signal    string `json:"signal"`
	Attempted bool   `json:"attempted"`
	Succeeded bool   `json:"succeeded"`
	Needed    bool   `json:"needed"`
}

type killLedgerState struct {
	SchemaVersion int          `json:"schema_version"`
	Records       []KillRecord `json:"records"`
}

type KillLedger struct {
	mu    sync.Mutex
	path  string
	max   int
	state killLedgerState
}

func OpenKillLedger(layout Layout, maxRecords int) (*KillLedger, error) {
	if err := validateLayout(layout); err != nil {
		return nil, err
	}
	if maxRecords < 1 {
		return nil, fmt.Errorf("invalid persistent kill limit")
	}
	path := filepath.Join(layout.SessionDir, "kill-ledger.json")
	state := killLedgerState{SchemaVersion: ledgerSchemaVersion, Records: []KillRecord{}}
	err := loadPrivateJSON(path, 64<<10, &state)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, io.EOF) {
		if err := createPrivateJSON(path, state, 64<<10); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := validateKillLedgerState(state, maxRecords); err != nil {
		return nil, err
	}
	return &KillLedger{path: path, max: maxRecords, state: state}, nil
}

func (l *KillLedger) Admit(killID, signal string, terminal bool) (KillRecord, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := operation.ParseID(killID); err != nil {
		return KillRecord{}, false, fmt.Errorf("invalid kill_id")
	}
	if signal != "INT" && signal != "TERM" && signal != "KILL" {
		return KillRecord{}, false, fmt.Errorf("invalid signal")
	}
	for _, record := range l.state.Records {
		if record.KillID != killID {
			continue
		}
		if record.Signal != signal {
			return KillRecord{}, false, fmt.Errorf("kill_conflict")
		}
		return record, record.Needed && !record.Attempted, nil
	}
	if len(l.state.Records) >= l.max {
		return KillRecord{}, false, failure.New(failure.PersistentKillHistoryExhausted, map[string]string{"reason": "record_limit"}, fmt.Errorf("persistent kill history exhausted"))
	}
	record := KillRecord{KillID: killID, Signal: signal, Needed: !terminal}
	next := cloneKillState(l.state)
	next.Records = append(next.Records, record)
	if err := l.commit(next); err != nil {
		return KillRecord{}, false, err
	}
	return record, record.Needed, nil
}

func (l *KillLedger) Record(record KillRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	index := -1
	for i := range l.state.Records {
		if l.state.Records[i].KillID == record.KillID {
			index = i
			break
		}
	}
	if index < 0 || l.state.Records[index].Signal != record.Signal {
		return fmt.Errorf("kill_conflict")
	}
	next := cloneKillState(l.state)
	next.Records[index] = record
	return l.commit(next)
}

func (l *KillLedger) commit(next killLedgerState) error {
	if err := validateKillLedgerState(next, l.max); err != nil {
		return err
	}
	if err := replacePrivateJSON(l.path, next, 64<<10); err != nil {
		return err
	}
	l.state = next
	return nil
}

func validateKillLedgerState(state killLedgerState, max int) error {
	if state.SchemaVersion != ledgerSchemaVersion || len(state.Records) > max {
		return fmt.Errorf("invalid persistent kill ledger")
	}
	seen := map[string]struct{}{}
	for _, record := range state.Records {
		if _, err := operation.ParseID(record.KillID); err != nil || (record.Signal != "INT" && record.Signal != "TERM" && record.Signal != "KILL") || record.Succeeded && !record.Attempted {
			return fmt.Errorf("invalid persistent kill ledger")
		}
		if _, ok := seen[record.KillID]; ok {
			return fmt.Errorf("invalid persistent kill ledger")
		}
		seen[record.KillID] = struct{}{}
	}
	return nil
}

func cloneKillState(state killLedgerState) killLedgerState {
	state.Records = append([]KillRecord(nil), state.Records...)
	return state
}

func loadPrivateJSON(path string, maxBytes int, out any) error {
	raw, err := readPrivateFile(path, 2, maxBytes)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("invalid private json")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("invalid private json")
	}
	return nil
}

func createPrivateJSON(path string, value any, maxBytes int) error {
	data, err := encodePrivateJSON(value, maxBytes)
	if err != nil {
		return err
	}
	return createOrMatchPrivateFile(path, data)
}

func replacePrivateJSON(path string, value any, maxBytes int) error {
	data, err := encodePrivateJSON(value, maxBytes)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".supervisor-ledger-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if err := writeAndSyncPrivate(tmp, data); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	keep = true
	return syncDirectory(filepath.Dir(path))
}

func encodePrivateJSON(value any, maxBytes int) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxBytes {
		return nil, fmt.Errorf("private metadata limit exceeded")
	}
	return encoded, nil
}
