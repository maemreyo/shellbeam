//go:build goexperiment.jsonv2

package main

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
)

type fixtureManifest struct {
	SchemaVersion int `json:"schema_version"`
	ValidV2       []struct {
		ID   string `json:"id"`
		JSON string `json:"json"`
	} `json:"valid_v2"`
	LegacyV1Files []string `json:"legacy_v1_files"`
}

type strictProbe struct {
	Action string `json:"action"`
}

type checkResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type report struct {
	SchemaVersion  int           `json:"schema_version"`
	GoVersion      string        `json:"go_version"`
	GOOS           string        `json:"goos"`
	GOARCH         string        `json:"goarch"`
	GoExperiment   string        `json:"goexperiment"`
	InvalidChecks  []checkResult `json:"invalid_checks"`
	ValidV2Checks  []checkResult `json:"valid_v2_checks"`
	LegacyV1Checks []checkResult `json:"legacy_v1_checks"`
	Verdict        string        `json:"verdict"`
}

func strictDecode(data []byte, dst any) error {
	return jsonv2.Unmarshal(data, dst, jsonv2.RejectUnknownMembers(true))
}

func invalidCorpus() map[string][]byte {
	invalidUTF8 := append([]byte(`{"action":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	return map[string][]byte{
		"invalid-utf8":    invalidUTF8,
		"duplicate-names": []byte(`{"action":"x","action":"y"}`),
		"unknown-names":   []byte(`{"action":"x","unknown":1}`),
		"wrong-case":      []byte(`{"Action":"x"}`),
		"trailing-json":   []byte(`{"action":"x"} {}`),
	}
}

func run(fixturesPath, outPath string) error {
	raw, err := os.ReadFile(fixturesPath)
	if err != nil {
		return err
	}
	var fixtures fixtureManifest
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		return err
	}
	if fixtures.SchemaVersion != 1 {
		return errors.New("unsupported fixture schema")
	}
	rep := report{SchemaVersion: 1, GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoExperiment: os.Getenv("GOEXPERIMENT"), Verdict: "PASS"}
	for id, raw := range invalidCorpus() {
		var dst strictProbe
		if err := strictDecode(raw, &dst); err == nil {
			rep.InvalidChecks = append(rep.InvalidChecks, checkResult{ID: id, Status: "FAIL", Detail: "ambiguous input accepted"})
			rep.Verdict = "FAIL"
		} else {
			rep.InvalidChecks = append(rep.InvalidChecks, checkResult{ID: id, Status: "PASS"})
		}
	}
	for _, f := range fixtures.ValidV2 {
		var baseline, candidate any
		e1 := json.Unmarshal([]byte(f.JSON), &baseline)
		e2 := jsonv2.Unmarshal([]byte(f.JSON), &candidate)
		status := "PASS"
		detail := ""
		if e1 != nil || e2 != nil || !reflect.DeepEqual(baseline, candidate) {
			status = "FAIL"
			detail = fmt.Sprintf("baseline=%v candidate=%v equivalent=%t", e1, e2, reflect.DeepEqual(baseline, candidate))
			rep.Verdict = "FAIL"
		}
		rep.ValidV2Checks = append(rep.ValidV2Checks, checkResult{ID: f.ID, Status: status, Detail: detail})
	}
	repoRoot, err := filepath.Abs(filepath.Join(filepath.Dir(fixturesPath), "..", "..", ".."))
	if err != nil {
		return err
	}
	for _, rel := range fixtures.LegacyV1Files {
		b, e := os.ReadFile(filepath.Join(repoRoot, rel))
		status := "PASS"
		detail := ""
		if e != nil {
			status = "FAIL"
			detail = e.Error()
		} else {
			var v any
			if e = json.Unmarshal(b, &v); e != nil {
				status = "FAIL"
				detail = e.Error()
			}
		}
		if status == "FAIL" {
			rep.Verdict = "FAIL"
		}
		rep.LegacyV1Checks = append(rep.LegacyV1Checks, checkResult{ID: rel, Status: status, Detail: detail})
	}
	enc, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	enc = append(enc, '\n')
	if err = os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	if err = os.WriteFile(outPath, enc, 0644); err != nil {
		return err
	}
	if rep.Verdict != "PASS" {
		return errors.New("parser tracer verdict FAIL")
	}
	return nil
}

func main() {
	fixtures := flag.String("fixtures", "tools/rich-media-parser-gate/testdata/fixtures.json", "fixture manifest")
	out := flag.String("out", ".build/rich-local-media-parser-gate/report.json", "report path")
	flag.Parse()
	if err := run(*fixtures, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
