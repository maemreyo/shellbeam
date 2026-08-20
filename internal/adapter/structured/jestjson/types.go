package jestjson

import stdjson "encoding/json"

const (
	adapterVersion     = 1
	capabilityVersion  = 1
	jestVocabularyV1   = 1
	maxJestJSONRecords = 8192
	maxJestStringBytes = 64 << 10
)

type documentV29 struct {
	NumFailedTestSuites       int                `json:"numFailedTestSuites"`
	NumFailedTests            int                `json:"numFailedTests"`
	NumPassedTestSuites       int                `json:"numPassedTestSuites"`
	NumPassedTests            int                `json:"numPassedTests"`
	NumPendingTestSuites      int                `json:"numPendingTestSuites"`
	NumPendingTests           int                `json:"numPendingTests"`
	NumRuntimeErrorTestSuites int                `json:"numRuntimeErrorTestSuites"`
	NumTodoTests              int                `json:"numTodoTests"`
	NumTotalTestSuites        int                `json:"numTotalTestSuites"`
	NumTotalTests             int                `json:"numTotalTests"`
	OpenHandles               stdjson.RawMessage `json:"openHandles"`
	Snapshot                  stdjson.RawMessage `json:"snapshot"`
	CoverageMap               stdjson.RawMessage `json:"coverageMap,omitempty"`
	StartTime                 int64              `json:"startTime"`
	Success                   bool               `json:"success"`
	TestResults               []testResultV29    `json:"testResults"`
	WasInterrupted            bool               `json:"wasInterrupted"`
}

type documentV30 struct {
	NumFailedTestSuites       int                `json:"numFailedTestSuites"`
	NumFailedTests            int                `json:"numFailedTests"`
	NumPassedTestSuites       int                `json:"numPassedTestSuites"`
	NumPassedTests            int                `json:"numPassedTests"`
	NumPendingTestSuites      int                `json:"numPendingTestSuites"`
	NumPendingTests           int                `json:"numPendingTests"`
	NumRuntimeErrorTestSuites int                `json:"numRuntimeErrorTestSuites"`
	NumTodoTests              int                `json:"numTodoTests"`
	NumTotalTestSuites        int                `json:"numTotalTestSuites"`
	NumTotalTests             int                `json:"numTotalTests"`
	OpenHandles               stdjson.RawMessage `json:"openHandles"`
	Snapshot                  stdjson.RawMessage `json:"snapshot"`
	CoverageMap               stdjson.RawMessage `json:"coverageMap,omitempty"`
	StartTime                 int64              `json:"startTime"`
	Success                   bool               `json:"success"`
	TestResults               []testResultV30    `json:"testResults"`
	WasInterrupted            bool               `json:"wasInterrupted"`
}

type testResultV29 struct {
	AssertionResults []assertionV29     `json:"assertionResults"`
	Coverage         stdjson.RawMessage `json:"coverage,omitempty"`
	EndTime          int64              `json:"endTime"`
	Message          string             `json:"message"`
	Name             string             `json:"name"`
	StartTime        int64              `json:"startTime"`
	Status           string             `json:"status"`
	Summary          string             `json:"summary"`
}

type testResultV30 struct {
	AssertionResults []assertionV30     `json:"assertionResults"`
	Coverage         stdjson.RawMessage `json:"coverage,omitempty"`
	EndTime          int64              `json:"endTime"`
	Message          string             `json:"message"`
	Name             string             `json:"name"`
	StartTime        int64              `json:"startTime"`
	Status           string             `json:"status"`
	Summary          string             `json:"summary"`
}

type assertionV29 struct {
	AncestorTitles    []string           `json:"ancestorTitles"`
	Duration          *float64           `json:"duration,omitempty"`
	FailureDetails    stdjson.RawMessage `json:"failureDetails"`
	FailureMessages   []string           `json:"failureMessages"`
	FullName          string             `json:"fullName"`
	Invocations       int                `json:"invocations"`
	Location          stdjson.RawMessage `json:"location"`
	NumPassingAsserts int                `json:"numPassingAsserts"`
	RetryReasons      stdjson.RawMessage `json:"retryReasons"`
	Status            string             `json:"status"`
	Title             string             `json:"title"`
}

type assertionV30 struct {
	AncestorTitles    []string           `json:"ancestorTitles"`
	Duration          *float64           `json:"duration,omitempty"`
	Failing           *bool              `json:"failing"`
	FailureDetails    stdjson.RawMessage `json:"failureDetails"`
	FailureMessages   []string           `json:"failureMessages"`
	FullName          string             `json:"fullName"`
	Invocations       int                `json:"invocations"`
	Location          stdjson.RawMessage `json:"location"`
	NumPassingAsserts int                `json:"numPassingAsserts"`
	RetryReasons      stdjson.RawMessage `json:"retryReasons"`
	StartAt           *int64             `json:"startAt"`
	Status            string             `json:"status"`
	Title             string             `json:"title"`
}
