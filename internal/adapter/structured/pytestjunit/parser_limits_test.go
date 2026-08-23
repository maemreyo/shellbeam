package pytestjunit

import (
	"context"
	"strings"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestPytestJUnitRejectsRawInput(t *testing.T) {
	reader, _ := newArtifactReader(`<testsuite name="x" tests="0" failures="0" errors="0" skipped="0"/>`)
	raw := core.RawInputRef(core.RawOutputRef{SessionID: "s", StartByte: 0, EndByte: 0, SHA256: strings.Repeat("a", 64)})
	if _, err := (Adapter{}).Parse(context.Background(), raw, reader, pytestLimits()); err == nil {
		t.Fatal("raw input accepted by artifact-only adapter")
	}
}

func TestPytestJUnitMalformedAndStructuralRootsFailClosed(t *testing.T) {
	for _, xml := range []string{
		`<not-junit/>`,
		`<testsuites><property name="root-extension" value="x"/></testsuites>`,
		`<testsuites><testcase name="orphan"/></testsuites>`,
		`<testsuite name="x" tests="1" failures="0" errors="0" skipped="0"><testcase name="a"></testsuite>`,
	} {
		reader, ref := newArtifactReader(xml)
		result, err := (Adapter{}).Parse(context.Background(), ref, reader, pytestLimits())
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != core.ParseMalformed || (result.Completeness != core.CompletenessUnavailable && result.Completeness != core.CompletenessPartial) {
			t.Fatalf("xml=%q result=%#v", xml, result)
		}
	}
}

func TestPytestJUnitEnforcesDepthFieldByteAndRecordBudgets(t *testing.T) {
	cases := []struct {
		name   string
		xml    string
		limits app.Limits
	}{
		{"depth", `<testsuite name="x" tests="1" failures="0" errors="0" skipped="0"><testcase name="a">` + strings.Repeat(`<x>`, 40) + strings.Repeat(`</x>`, 40) + `</testcase></testsuite>`, pytestLimits()},
		{"attribute", `<testsuite name="` + strings.Repeat("x", 65537) + `" tests="0" failures="0" errors="0" skipped="0"/>`, pytestLimits()},
		{"bytes", `<testsuite name="x" tests="1" failures="0" errors="0" skipped="0"><testcase name="a"/></testsuite>`, app.Limits{MaxBytes: 32, MaxRecords: 2048, MaxStringBytes: 64 << 10, MaxDepth: 64, MaxDuration: pytestLimits().MaxDuration}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader, ref := newArtifactReader(tc.xml)
			result, err := (Adapter{}).Parse(context.Background(), ref, reader, tc.limits)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != core.ParseBudgetExceeded {
				t.Fatalf("result=%#v", result)
			}
		})
	}

	var b strings.Builder
	b.WriteString(`<testsuite name="x" tests="1025" failures="0" errors="0" skipped="0">`)
	for i := 0; i < 1025; i++ {
		b.WriteString(`<testcase name="a"/>`)
	}
	b.WriteString(`</testsuite>`)
	reader, ref := newArtifactReader(b.String())
	limits := pytestLimits()
	limits.MaxRecords = 1024
	result, err := (Adapter{}).Parse(context.Background(), ref, reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != core.ParseBudgetExceeded {
		t.Fatalf("record budget result=%#v", result)
	}
}

func TestPytestJUnitEnforcesElementBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<testsuite name="x" tests="0" failures="0" errors="0" skipped="0"><properties>`)
	for i := 0; i < maxXMLElements; i++ {
		b.WriteString(`<property name="a" value="b"/>`)
	}
	b.WriteString(`</properties></testsuite>`)
	reader, ref := newArtifactReader(b.String())
	limits := pytestLimits()
	limits.MaxBytes = int64(len(b.String())) + 1
	result, err := (Adapter{}).Parse(context.Background(), ref, reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != core.ParseBudgetExceeded {
		t.Fatalf("result=%#v", result)
	}
}

func TestPytestJUnitHonorsCancelledContext(t *testing.T) {
	reader, ref := newArtifactReader(`<testsuite name="x" tests="0" failures="0" errors="0" skipped="0"/>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Adapter{}).Parse(ctx, ref, reader, pytestLimits()); err == nil {
		t.Fatal("cancelled context accepted")
	}
}
