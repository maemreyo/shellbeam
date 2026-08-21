package jestjson

import (
	"context"
	"os"
	"testing"
)

func FuzzJestJSON(f *testing.F) {
	for _, seed := range [][]byte{
		realFixtureForFuzz("29.7.0"),
		realFixtureForFuzz("30.4.2"),
		[]byte(`{"numFailedTestSuites":0,"numFailedTests":0,"numPassedTestSuites":0,"numPassedTests":0,"numPendingTestSuites":0,"numPendingTests":0,"numRuntimeErrorTestSuites":0,"numTodoTests":0,"numTotalTestSuites":0,"numTotalTests":0,"openHandles":[],"snapshot":{},"startTime":0,"success":true,"testResults":[],"wasInterrupted":false}`),
	} {
		if len(seed) > 0 {
			f.Add(seed)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		reader, ref := newArtifactReader(data)
		reader.input.RepositoryRoot = "/private/jest-fixture"
		result, err := (Adapter{}).Parse(context.Background(), ref, reader, jestLimits())
		if err != nil {
			return
		}
		if result.SemanticsCoverage != nil && result.SemanticsCoverage.Validate() != nil {
			t.Fatalf("invalid coverage: %#v", result.SemanticsCoverage)
		}
		if result.ObservedEntries != nil && result.ObservedEntries.Validate() != nil {
			t.Fatalf("invalid observed entries: %#v", result.ObservedEntries)
		}
		for i, record := range result.Records {
			if err := record.Validate(); err != nil {
				t.Fatalf("record[%d]: %v", i, err)
			}
		}
	})
}

func realFixtureForFuzz(version string) []byte {
	path := "../../../../tests/fixtures/jest-json/real-doc-fixtures/jest-" + version + "/pass.json"
	data, _ := os.ReadFile(path)
	return data
}
