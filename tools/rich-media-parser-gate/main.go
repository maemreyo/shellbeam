//go:build goexperiment.jsonv2

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type fixtureManifest struct {
	SchemaVersion int `json:"schema_version"`
	ValidV2       []struct {
		ID             string `json:"id"`
		Kind           string `json:"kind"`
		JSON           string `json:"json"`
		SemanticSHA256 string `json:"semantic_sha256"`
	} `json:"valid_v2"`
	LegacyV1 []struct {
		ID             string `json:"id"`
		Path           string `json:"path"`
		SemanticSHA256 string `json:"semantic_sha256"`
	} `json:"legacy_v1"`
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
	SchemaVersion         int           `json:"schema_version"`
	CandidateMode         string        `json:"candidate_mode"`
	FixtureManifestSHA256 string        `json:"fixture_manifest_sha256"`
	GoVersion             string        `json:"go_version"`
	GoVersionCommand      string        `json:"go_version_command"`
	GOOS                  string        `json:"goos"`
	GOARCH                string        `json:"goarch"`
	GoExperiment          string        `json:"goexperiment"`
	CGOEnabled            string        `json:"cgo_enabled"`
	Command               string        `json:"command"`
	ExitStatus            int           `json:"exit_status"`
	InvalidChecks         []checkResult `json:"invalid_checks"`
	ValidV2Checks         []checkResult `json:"valid_v2_checks"`
	LegacyV1Checks        []checkResult `json:"legacy_v1_checks"`
	Verdict               string        `json:"verdict"`
}

func strictDecode(data []byte, dst any) error {
	return jsonv2.Unmarshal(data, dst, jsonv2.RejectUnknownMembers(true))
}

type invalidCase struct {
	id  string
	raw []byte
}

func invalidCorpus() []invalidCase {
	invalidUTF8 := append([]byte(`{"action":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	return []invalidCase{
		{"invalid-utf8", invalidUTF8},
		{"duplicate-names", []byte(`{"action":"x","action":"y"}`)},
		{"unknown-names", []byte(`{"action":"x","unknown":1}`)},
		{"wrong-case", []byte(`{"Action":"x"}`)},
		{"trailing-json", []byte(`{"action":"x"} {}`)},
	}
}

func semanticDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalSemanticDigest(raw []byte) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	return semanticDigest(v)
}

func fileSHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func output(name string, args ...string) (string, error) {
	b, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func repoRoot() (string, error) { return output("git", "rev-parse", "--show-toplevel") }

func writeReport(outPath string, rep report) error {
	enc, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	enc = append(enc, '\n')
	if err = os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, enc, 0644)
}

func loadFixtureManifest(path string) ([]byte, fixtureManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fixtureManifest{}, err
	}
	var fixtures fixtureManifest
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		return nil, fixtureManifest{}, err
	}
	if fixtures.SchemaVersion != 2 {
		return nil, fixtureManifest{}, errors.New("unsupported fixture schema")
	}
	return raw, fixtures, nil
}

func logicalCommand(fixturesPath, outPath string) string {
	prefix := ""
	if toolchain := os.Getenv("GOTOOLCHAIN"); toolchain != "" && toolchain != "auto" {
		prefix += "GOTOOLCHAIN=" + toolchain + " "
	}
	if experiment := os.Getenv("GOEXPERIMENT"); experiment != "" {
		prefix += "GOEXPERIMENT=" + experiment + " "
	}
	return prefix + "go run ./tools/rich-media-parser-gate -fixtures " + fixturesPath + " -out " + outPath
}

func goEnvDetails() (string, string, string, string, error) {
	b, err := exec.Command("go", "env", "GOEXPERIMENT", "GOOS", "GOARCH", "CGO_ENABLED").CombinedOutput()
	if err != nil {
		return "", "", "", "", err
	}
	text := strings.TrimSuffix(string(b), "\n")
	lines := strings.Split(text, "\n")
	if len(lines) != 4 {
		return "", "", "", "", fmt.Errorf("unexpected go env field count %d", len(lines))
	}
	return lines[0], lines[1], lines[2], lines[3], nil
}

var (
	stableJSONv2Version  = regexp.MustCompile(`^go1\.([0-9]+)(?:\.[0-9]+)?$`)
	previewJSONv2Version = regexp.MustCompile(`^go1\.([0-9]+)(?:beta|rc)[0-9]+$`)
)

func jsonv2Minor(version string, pattern *regexp.Regexp) (int, bool) {
	match := pattern.FindStringSubmatch(version)
	if len(match) != 2 {
		return 0, false
	}
	minor, err := strconv.Atoi(match[1])
	return minor, err == nil
}

func candidateMode(version, experiment string) (string, bool) {
	if strings.HasPrefix(version, "go1.26") {
		if experiment == "jsonv2" {
			return "go1.26-jsonv2-experiment", true
		}
		return "", false
	}
	if experiment != "" {
		return "", false
	}
	if minor, ok := jsonv2Minor(version, previewJSONv2Version); ok && minor >= 27 {
		return "go1.27-jsonv2-preview", true
	}
	if minor, ok := jsonv2Minor(version, stableJSONv2Version); ok && minor >= 27 {
		return "go1.27-stable-jsonv2", true
	}
	return "", false
}

func newCandidateReport(raw []byte, fixturesPath, outPath string) report {
	goVersionCommand, goVersionErr := output("go", "version")
	goExperiment, goos, goarch, cgoEnabled, goEnvErr := goEnvDetails()
	rep := report{
		SchemaVersion:         2,
		CandidateMode:         "",
		FixtureManifestSHA256: fileSHA256(raw),
		GoVersion:             runtime.Version(),
		GoVersionCommand:      goVersionCommand,
		GOOS:                  runtime.GOOS,
		GOARCH:                runtime.GOARCH,
		Command:               logicalCommand(fixturesPath, outPath),
		Verdict:               "PASS",
	}
	rep.GoExperiment, rep.GOOS, rep.GOARCH, rep.CGOEnabled = goExperiment, goos, goarch, cgoEnabled
	mode, modeOK := candidateMode(rep.GoVersion, rep.GoExperiment)
	rep.CandidateMode = mode
	if goVersionErr != nil || goEnvErr != nil || !modeOK {
		rep.Verdict = "FAIL"
	}
	return rep
}

func addInvalidChecks(rep *report) {
	for _, c := range invalidCorpus() {
		var dst strictProbe
		if err := strictDecode(c.raw, &dst); err == nil {
			rep.InvalidChecks = append(rep.InvalidChecks, checkResult{ID: c.id, Status: "FAIL", Detail: "ambiguous input accepted"})
			rep.Verdict = "FAIL"
		} else {
			rep.InvalidChecks = append(rep.InvalidChecks, checkResult{ID: c.id, Status: "PASS"})
		}
	}
}

func addValidV2Checks(rep *report, fixtures fixtureManifest) {
	for _, f := range fixtures.ValidV2 {
		var baseline, candidate any
		e1 := json.Unmarshal([]byte(f.JSON), &baseline)
		e2 := jsonv2.Unmarshal([]byte(f.JSON), &candidate)
		status, detail := "PASS", ""
		bd, bde := semanticDigest(baseline)
		cd, cde := semanticDigest(candidate)
		if e1 != nil || e2 != nil || bde != nil || cde != nil || !reflect.DeepEqual(baseline, candidate) || bd != f.SemanticSHA256 || cd != f.SemanticSHA256 {
			status = "FAIL"
			detail = fmt.Sprintf("baseline_err=%v candidate_err=%v equivalent=%t baseline_digest=%s candidate_digest=%s expected=%s", e1, e2, reflect.DeepEqual(baseline, candidate), bd, cd, f.SemanticSHA256)
			rep.Verdict = "FAIL"
		}
		rep.ValidV2Checks = append(rep.ValidV2Checks, checkResult{ID: f.ID, Status: status, Detail: detail})
	}
}

func addLegacyV1Checks(rep *report, fixtures fixtureManifest) {
	root, rootErr := repoRoot()
	if rootErr != nil {
		rep.Verdict = "FAIL"
		return
	}
	for _, f := range fixtures.LegacyV1 {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		digest := ""
		if err == nil {
			digest, err = canonicalSemanticDigest(b)
		}
		status, detail := "PASS", ""
		if err != nil || digest != f.SemanticSHA256 {
			status = "FAIL"
			detail = fmt.Sprintf("err=%v digest=%s expected=%s", err, digest, f.SemanticSHA256)
			rep.Verdict = "FAIL"
		}
		rep.LegacyV1Checks = append(rep.LegacyV1Checks, checkResult{ID: f.ID, Status: status, Detail: detail})
	}
}

func run(fixturesPath, outPath string) error {
	raw, fixtures, err := loadFixtureManifest(fixturesPath)
	if err != nil {
		return err
	}
	rep := newCandidateReport(raw, fixturesPath, outPath)
	addInvalidChecks(&rep)
	addValidV2Checks(&rep, fixtures)
	addLegacyV1Checks(&rep, fixtures)
	if rep.Verdict != "PASS" {
		rep.ExitStatus = 1
	}
	if err := writeReport(outPath, rep); err != nil {
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
