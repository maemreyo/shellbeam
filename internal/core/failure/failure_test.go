package failure

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestFailureStableCodeSerializationAndMatching(t *testing.T) {
	cause := errors.New("store failed at /Users/test/.ssh/id_rsa token=secret")
	failureErr := New(OperationConflict, map[string]string{
		"operation_id": "op-123",
		"path":         "/Users/test/private",
	}, cause)
	if !errors.Is(failureErr, OperationConflict) {
		t.Fatal("errors.Is did not match stable code")
	}
	var typed *Failure
	if !errors.As(failureErr, &typed) || typed.Cause != cause {
		t.Fatalf("errors.As did not preserve typed cause: %#v", typed)
	}
	public := Public(failureErr)
	if public.Code != OperationConflict || public.Message != "operation conflicts with an existing intent" || public.Retryable {
		t.Fatalf("public failure=%#v", public)
	}
	if len(public.Details) != 1 || public.Details["operation_id"] != "op-123" {
		t.Fatalf("unsafe details were not filtered: %#v", public.Details)
	}
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"id_rsa", "token=secret", "/Users/test/private"} {
		if strings.Contains(text, secret) {
			t.Fatalf("public serialization leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, `"code":"operation_conflict"`) {
		t.Fatalf("stable code not serialized: %s", text)
	}
}

func TestFailureUnknownMapsToInternalWithoutLeak(t *testing.T) {
	raw := errors.New("open /Users/test/private.pem: token=super-secret")
	public := Public(raw)
	if public.Code != Internal || public.Message != "internal error" || public.Retryable {
		t.Fatalf("unknown error projection=%#v", public)
	}
	if len(public.Details) != 0 || public.Cause != nil {
		t.Fatalf("unknown error leaked internal data: %#v", public)
	}
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private.pem") || strings.Contains(string(data), "super-secret") {
		t.Fatalf("unknown error leaked in JSON: %s", data)
	}
}

func TestFailureLegacyMappingAndRetryability(t *testing.T) {
	tests := []struct {
		raw       string
		code      Code
		retryable bool
	}{
		{"operation_conflict", OperationConflict, false},
		{"operation_metadata_conflict", OperationMetadataConflict, false},
		{"capacity_exceeded", CapacityExceeded, true},
		{"persistence_unavailable", PersistenceUnavailable, true},
		{"storage_reserve_exhausted", StorageReserveExhausted, true},
		{"invalid operation id", InvalidInput, false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := Public(errors.New(tt.raw))
			if got.Code != tt.code || got.Retryable != tt.retryable {
				t.Fatalf("Public(%q)=%#v", tt.raw, got)
			}
		})
	}
}

func TestFailurePublicCodeSet(t *testing.T) {
	for _, code := range []Code{
		InvalidInput,
		FeatureUnavailable,
		OperationConflict,
		OperationMetadataConflict,
		WorkspaceNotFound,
		WorkspaceAddressEscape,
		ManifestInvalid,
		ManifestReviewRequired,
		IdentityObservationFailed,
		EventCursorInvalid,
		EventCursorExpired,
		EventContinuityUnavailable,
		StructuredAdapterUnavailable,
		StructuredAdapterUnsupported,
		StructuredResultMalformed,
		StructuredResultPartial,
		StructuredResultBudgetExceeded,
		StructuredResultNotFound,
		Internal,
	} {
		if code == "" || code.Error() != string(code) {
			t.Fatalf("invalid public code %q", code)
		}
		got := Public(New(code, nil, nil))
		if got.Code != code || got.Message == "" {
			t.Fatalf("missing public spec for %q: %#v", code, got)
		}
	}
}

func TestA4FailureCodesAreStableAndDoNotLeakDetails(t *testing.T) {
	codes := []Code{
		TelemetryUnavailable, TelemetryPartial, TelemetryBudgetExceeded, TelemetryIncompatibleHistory,
		ResourceObservationUnavailable, ResourceObservationPartial, ResourceLimitUnsupported,
		ReproNotFound, ReproMaterializationUnavailable, ReproSourceUnavailable, ReproReferenceCompacted,
	}
	for _, code := range codes {
		public := Public(New(code, map[string]string{
			"operation_id": "op-1", "repro_id": "repro_01K00000000000000000000000", "ref_id": "result:1",
			"metric": "max_rss", "reason": "bounded", "path": "/Users/alice/.ssh/id_work token=secret",
		}, errors.New("private /Users/alice/.ssh/id_work token=secret")))
		if public.Code != code || public.Message == "" {
			t.Fatalf("missing A4 failure spec for %q: %#v", code, public)
		}
		data, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "id_work") || strings.Contains(string(data), "token=secret") {
			t.Fatalf("A4 public failure leaked private detail: %s", data)
		}
	}
}
