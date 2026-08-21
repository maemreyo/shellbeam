package daemon

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestStructuredCaptureMetadataDoesNotChangeRawTerminalReceiptProjection(t *testing.T) {
	service := &Service{options: Options{Incarnation: "daemon"}}
	baseReservation := operation.Reservation{
		SchemaVersion: 2, OperationID: "receipt-structured-op", SessionID: "receipt-structured-session",
		ExecutionMode: operation.ExecutionModeArgv, Executable: "/usr/bin/jest", CWD: "/repo",
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: strings.Repeat("c", 64),
	}
	plain := &liveSession{operationID: string(baseReservation.OperationID), sessionID: string(baseReservation.SessionID), reservation: baseReservation}
	structuredReservation := baseReservation
	structuredReservation.StructuredAdapter = "jest-json"
	structuredReservation.StructuredCaptureDigest = strings.Repeat("d", 64)
	structured := &liveSession{operationID: plain.operationID, sessionID: plain.sessionID, reservation: structuredReservation}

	plainReceipt := service.receiptFor(plain, session.Failed, session.Failure)
	structuredReceipt := service.receiptFor(structured, session.Failed, session.Failure)
	if !reflect.DeepEqual(plainReceipt, structuredReceipt) {
		t.Fatalf("structured metadata changed raw receipt: plain=%#v structured=%#v", plainReceipt, structuredReceipt)
	}
	encoded, err := json.Marshal(structuredReceipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"structured_adapter", "structured_capture_digest", "jest-json"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private structured metadata leaked into receipt: %s", encoded)
		}
	}
}
