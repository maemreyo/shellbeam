package jestjson

import (
	"errors"

	"github.com/go-json-experiment/json/jsontext"
	"github.com/maemreyo/shellbeam/internal/core/jsonstrict"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type observedProfile struct {
	family string
	v29    *documentV29
	v30    *documentV30
}

func decodeProfile(data []byte) (observedProfile, core.ParseOutcome) {
	var v30 documentV30
	if err := jsonstrict.Decode(data, &v30); err == nil {
		if v30ProfileComplete(v30) {
			return observedProfile{family: "v30", v30: &v30}, core.ParseComplete
		}
	} else if syntacticJSONError(err) {
		return observedProfile{}, core.ParseMalformed
	}

	var v29 documentV29
	if err := jsonstrict.Decode(data, &v29); err == nil {
		return observedProfile{family: "v29", v29: &v29}, core.ParseComplete
	} else if syntacticJSONError(err) {
		return observedProfile{}, core.ParseMalformed
	}
	return observedProfile{}, core.ParseUnavailable
}

func v30ProfileComplete(doc documentV30) bool {
	seenAssertion := false
	for _, result := range doc.TestResults {
		for _, assertion := range result.AssertionResults {
			seenAssertion = true
			if assertion.Failing == nil || assertion.StartAt == nil {
				return false
			}
		}
	}
	return seenAssertion
}

func syntacticJSONError(err error) bool {
	var target *jsontext.SyntacticError
	return errors.As(err, &target)
}

func semanticsCoverage(family string) *core.ProducerSemanticsCoverage {
	observable := []string{
		"core:failure_set_completeness",
		"core:observed_entry_counts",
		"core:test_status_fail",
		"core:test_status_pass",
		"core:test_status_skip",
		"jest:invocations",
		"jest:pending",
		"jest:suite_focused",
		"jest:todo",
		"jest:zero_match_emits_artifact",
	}
	unavailable := []string{
		"jest:error_status",
		"jest:hook_phase",
		"jest:retry_reasons",
		"jest:suite_aggregate_counters",
		"jest:suite_execution_error_distinction",
	}
	if family == "v30" {
		observable = append(observable, "jest:failing_expected", "jest:failing_unexpected")
	} else {
		unavailable = append(unavailable, "jest:failing_expected", "jest:failing_unexpected")
	}
	// ProducerSemanticsCoverage requires both claim sets sorted.
	sortStrings(observable)
	sortStrings(unavailable)
	return &core.ProducerSemanticsCoverage{
		Namespace: "jest", VocabularyVersion: jestVocabularyV1, Format: "jest_json", Family: family,
		MechanicallyObservable: observable, Unavailable: unavailable,
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
