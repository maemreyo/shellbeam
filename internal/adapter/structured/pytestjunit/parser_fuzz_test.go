package pytestjunit

import (
	"context"
	"testing"
)

func FuzzPytestJUnit(f *testing.F) {
	for _, seed := range []string{
		`<testsuite name="pytest" tests="0" failures="0" errors="0" skipped="0"/>`,
		`<testsuites><testsuite name="pytest" tests="1" failures="0" errors="0" skipped="0"><testcase classname="pkg" name="a"/></testsuite></testsuites>`,
		`<testsuite name="pytest" tests="1" failures="0" errors="0" skipped="1"><testcase name="a"><skipped type="pytest.xfail"/></testcase></testsuite>`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		reader, ref := newArtifactReader(text)
		result, err := (Adapter{}).Parse(context.Background(), ref, reader, pytestLimits())
		if err != nil {
			return
		}
		if result.SemanticsCoverage != nil && result.SemanticsCoverage.Validate() != nil {
			t.Fatalf("invalid coverage: %#v", result.SemanticsCoverage)
		}
		for i, record := range result.Records {
			if err := record.Validate(); err != nil {
				t.Fatalf("record[%d]: %v", i, err)
			}
		}
	})
}
