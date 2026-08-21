package structuredresult

import (
	"testing"
	"time"
)

func TestStructuredLimitsPermitJSPersistedRecordBudget(t *testing.T) {
	limits := Limits{MaxBytes: 16 << 20, MaxRecords: 8192, MaxStringBytes: 64 << 10, MaxDepth: 16, MaxDuration: 5 * time.Second}
	if err := limits.Validate(); err != nil {
		t.Fatalf("8192 record parser budget rejected: %v", err)
	}
	limits.MaxRecords = 8193
	if err := limits.Validate(); err == nil {
		t.Fatal("record budget above 8192 accepted")
	}
}
