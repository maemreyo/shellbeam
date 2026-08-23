package jestjson

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func validateProfileBounds(ctx context.Context, profile observedProfile, fieldLimit int) (bool, error) {
	files, entries := 0, 0
	if profile.v30 != nil {
		files = len(profile.v30.TestResults)
		for i, file := range profile.v30.TestResults {
			if err := checkJestContext(ctx, i); err != nil {
				return false, err
			}
			if entries > core.MaxObservedEntries-len(file.AssertionResults) || !testResultV30StringsWithin(file, fieldLimit) {
				return false, nil
			}
			entries += len(file.AssertionResults)
		}
	} else if profile.v29 != nil {
		files = len(profile.v29.TestResults)
		for i, file := range profile.v29.TestResults {
			if err := checkJestContext(ctx, i); err != nil {
				return false, err
			}
			if entries > core.MaxObservedEntries-len(file.AssertionResults) || !testResultV29StringsWithin(file, fieldLimit) {
				return false, nil
			}
			entries += len(file.AssertionResults)
		}
	}
	return files <= core.MaxObservedEntries && entries <= core.MaxObservedEntries, nil
}

func testResultV30StringsWithin(file testResultV30, limit int) bool {
	if !jestStringsWithin(limit, file.Message, file.Name, file.Status, file.Summary) {
		return false
	}
	for _, assertion := range file.AssertionResults {
		if !assertionV30StringsWithin(assertion, limit) {
			return false
		}
	}
	return true
}

func testResultV29StringsWithin(file testResultV29, limit int) bool {
	if !jestStringsWithin(limit, file.Message, file.Name, file.Status, file.Summary) {
		return false
	}
	for _, assertion := range file.AssertionResults {
		if !assertionV29StringsWithin(assertion, limit) {
			return false
		}
	}
	return true
}

func assertionV30StringsWithin(a assertionV30, limit int) bool {
	if !jestStringsWithin(limit, a.FullName, a.Status, a.Title) || !jestStringSliceWithin(a.AncestorTitles, limit) || !jestStringSliceWithin(a.FailureMessages, limit) {
		return false
	}
	return true
}

func assertionV29StringsWithin(a assertionV29, limit int) bool {
	if !jestStringsWithin(limit, a.FullName, a.Status, a.Title) || !jestStringSliceWithin(a.AncestorTitles, limit) || !jestStringSliceWithin(a.FailureMessages, limit) {
		return false
	}
	return true
}

func jestStringsWithin(limit int, values ...string) bool {
	for _, value := range values {
		if len(value) > limit {
			return false
		}
	}
	return true
}

func jestStringSliceWithin(values []string, limit int) bool {
	for _, value := range values {
		if len(value) > limit {
			return false
		}
	}
	return true
}

func checkJestContext(ctx context.Context, i int) error {
	if i&255 == 0 {
		return ctx.Err()
	}
	return nil
}
