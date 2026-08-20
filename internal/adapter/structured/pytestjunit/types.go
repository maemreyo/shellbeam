package pytestjunit

import (
	"errors"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const (
	adapterVersion        = 1
	capabilityVersion     = 1
	pytestVocabularyV1    = 1
	maxXMLDepth           = 32
	maxXMLElements        = 8192
	maxXMLFieldBytes      = 64 << 10
	maxXMLAttributes      = 256
	maxPytestJUnitRecords = 1024
)

var (
	errMalformed = errors.New("pytest junit malformed")
	errBudget    = errors.New("pytest junit budget exceeded")
)

type Adapter struct{}

func (Adapter) ID() string   { return "pytest-junit-xml" }
func (Adapter) Version() int { return adapterVersion }

func semanticsCoverage() *core.ProducerSemanticsCoverage {
	return &core.ProducerSemanticsCoverage{
		Namespace: "pytest", VocabularyVersion: pytestVocabularyV1, Format: "junit-xml", Family: "xunit2",
		MechanicallyObservable: []string{"coarse:error", "coarse:fail", "coarse:pass", "coarse:skip", "pytest:skip", "pytest:xfail"},
		Unavailable:            []string{"pytest:error_phase", "pytest:xfail_execution_state", "pytest:xpass_exact"},
	}
}

type suiteState struct {
	name        string
	ordinal     int
	tagDepth    int
	tests       int
	failures    int
	errors      int
	skipped     int
	durationMS  int64
	caseOrdinal int
}

type testcaseState struct {
	name        string
	classname   string
	durationMS  int64
	tagDepth    int
	status      core.TestStatus
	outcomeSeen bool
	invalid     bool
	partial     bool
	diagnostic  bool
	disposition *core.ProducerTestDisposition
}
