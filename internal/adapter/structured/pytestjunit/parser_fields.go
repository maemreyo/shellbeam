package pytestjunit

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func parseSuite(el xml.StartElement, ordinal, depth, fieldLimit int) (suiteState, error) {
	name, ok := attrValue(el, "name")
	if !ok || !safeField(name, fieldLimit, false) {
		return suiteState{}, errMalformed
	}
	tests, err := nonnegativeIntAttr(el, "tests")
	if err != nil {
		return suiteState{}, err
	}
	failures, err := nonnegativeIntAttr(el, "failures")
	if err != nil {
		return suiteState{}, err
	}
	errorsCount, err := nonnegativeIntAttr(el, "errors")
	if err != nil {
		return suiteState{}, err
	}
	skipped, err := nonnegativeIntAttr(el, "skipped")
	if err != nil {
		return suiteState{}, err
	}
	if failures > tests || errorsCount > tests || skipped > tests || failures+errorsCount+skipped > tests {
		return suiteState{}, errMalformed
	}
	duration, err := durationAttr(el, "time")
	if err != nil {
		return suiteState{}, err
	}
	return suiteState{name: name, ordinal: ordinal, tagDepth: depth, tests: tests, failures: failures, errors: errorsCount, skipped: skipped, durationMS: duration}, nil
}

func parseTestcase(el xml.StartElement, depth, fieldLimit int) (testcaseState, error) {
	name, ok := attrValue(el, "name")
	if !ok || !safeField(name, fieldLimit, false) {
		return testcaseState{}, errMalformed
	}
	classname, _ := attrValue(el, "classname")
	if classname != "" && !safeField(classname, fieldLimit, false) {
		return testcaseState{}, errMalformed
	}
	duration, err := durationAttr(el, "time")
	if err != nil {
		return testcaseState{}, err
	}
	return testcaseState{name: name, classname: classname, durationMS: duration, tagDepth: depth, status: core.TestPassed}, nil
}

func attrValue(el xml.StartElement, name string) (string, bool) {
	for _, attr := range el.Attr {
		if attr.Name.Local == name {
			return attr.Value, true
		}
	}
	return "", false
}
func nonnegativeIntAttr(el xml.StartElement, name string) (int, error) {
	value, ok := attrValue(el, name)
	if !ok {
		return 0, errMalformed
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 1<<20 {
		return 0, errMalformed
	}
	return n, nil
}
func durationAttr(el xml.StartElement, name string) (int64, error) {
	value, ok := attrValue(el, name)
	if !ok || value == "" {
		return 0, nil
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > float64(math.MaxInt64)/1000 {
		return 0, errMalformed
	}
	return int64(math.Round(seconds * 1000)), nil
}
func safeField(value string, limit int, allowEmpty bool) bool {
	if len(value) > limit || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.TrimSpace(value) != value {
		return false
	}
	return allowEmpty || value != ""
}
func minInt64(values ...int64) int64 {
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func isSemanticElement(name string) bool {
	switch name {
	case "testsuites", "testsuite", "testcase", "failure", "error", "skipped":
		return true
	default:
		return false
	}
}
