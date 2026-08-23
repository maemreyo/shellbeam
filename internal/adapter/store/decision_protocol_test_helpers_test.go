package store

import (
	"encoding/json"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func EpisodeDiagnosisForStoreTest() decisionprotocol.EpisodeKind {
	return decisionprotocol.EpisodeDiagnosis
}
func RecordEpisodeForStoreTest() decisionprotocol.RecordKind { return decisionprotocol.RecordEpisode }
func canonicalEnvelopeForStoreTest(seq decisionprotocol.RecordSeq, kind decisionprotocol.RecordKind, body any) (decisionprotocol.CanonicalRecordEnvelope, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return decisionprotocol.CanonicalRecordEnvelope{}, err
	}
	return decisionprotocol.CanonicalRecordEnvelope{SchemaVersion: 1, CanonicalRecordSeq: seq, Kind: kind, Body: b}, nil
}
