package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestE27TraceEventCorrelationContainsOnlySafeIdentityAndCounts(t *testing.T) {
	r := openInputTraceRepository(t)
	rec := inputTraceRecord("event-safe", strings.Repeat("f", 64), time.Now().UTC(), false)
	if err := r.PutInputTraceRecord(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	obs, _ := r.ListObservationObligations(context.Background(), 0, 10)
	if len(obs) != 1 {
		t.Fatalf("obs=%#v", obs)
	}
	o := obs[0]
	if o.Kind != observation.EventInputTraceRecorded || o.Correlation.OperationID != rec.OperationID || o.Correlation.SessionID != rec.SessionID || !strings.HasPrefix(o.SubjectRef, "input-trace:") {
		t.Fatalf("obligation=%#v", o)
	}
	for _, forbidden := range []string{"internal/dep.go", "/Users/", "DYLD_INSERT_LIBRARIES"} {
		if strings.Contains(o.Summary, forbidden) || strings.Contains(o.SubjectRef, forbidden) {
			t.Fatalf("event leaked %q: %#v", forbidden, o)
		}
	}
}
