package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var nativeProbeRegistry = map[string]nativeProbeFunc{
	"P0":  probeP0PrivateServerIdentity,
	"P1":  probeP1ExactHumanClientIdentity,
	"P2":  probeP2ExactClientFlagIsolation,
	"P3":  probeP3SameClientIngressFence,
	"P4":  probeP4PrivacyScope,
	"P5":  probeP5PrivateFromFirstByte,
	"P6":  probeP6ReconnectNoReplay,
	"P7":  probeP7EnvironmentPreservation,
	"P8":  probeP8WritableHumanControl,
	"P9":  probeP9ReadOnlyLocalControl,
	"P10": probeP10ResizeIsolation,
	"P11": probeP11CrashReconnectIdentity,
	"P12": probeP12ACKOrderingAndBackpressure,
	"P13": probeP13ResourceLeakStress,
	"P14": probeP14MultiSessionPrivacyIsolation,
	"P15": probeP15ObserverOverlapPrivacyFault,
}

type identity struct {
	GitHead     string
	GOOS        string
	GOARCH      string
	GoVersion   string
	TmuxPath    string
	TmuxVersion string
	TmuxSHA256  string
}

func main() {
	if err := runCLI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: interactive-handoff-h0 run|render|verify-gate")
	}
	switch args[0] {
	case "render":
		return runRender(args[1:])
	case "verify-gate":
		return runVerifyGate(args[1:])
	case "run":
		return runNative(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runNative(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var tmuxPath, rawDir, jsonPath string
	fs.StringVar(&tmuxPath, "tmux", "", "absolute tmux executable path")
	fs.StringVar(&rawDir, "raw-dir", "", "H0 raw artifact directory")
	fs.StringVar(&jsonPath, "json", "", "native report JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || tmuxPath == "" || rawDir == "" || jsonPath == "" {
		return errors.New("run requires --tmux, --raw-dir, and --json")
	}
	id, err := collectIdentity(tmuxPath)
	if err != nil {
		return err
	}
	repo, err := repositoryRoot()
	if err != nil {
		return err
	}
	rawDir, jsonPath, err = validateRunPaths(repo, rawDir, jsonPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(rawDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(rawDir, 0o700); err != nil {
		return err
	}
	results := runNativeProbes(nativeProbeEnv{Tmux: id.TmuxPath, RawDir: rawDir}, nativeProbeRegistry)
	report := Report{SchemaVersion: reportSchemaVersion, GitHead: id.GitHead, GOOS: id.GOOS, GOARCH: id.GOARCH, GoVersion: id.GoVersion, TmuxPath: id.TmuxPath, TmuxVersion: id.TmuxVersion, TmuxSHA256: id.TmuxSHA256, Results: sortedResults(results), Verdict: verdict(results)}
	if err := validateReport(report); err != nil {
		return fmt.Errorf("native report invalid: %w", err)
	}
	encoded, err := marshalDeterministic(report)
	if err != nil {
		return err
	}
	return writeFileAtomic(jsonPath, encoded, 0o600)
}

func runNativeProbes(env nativeProbeEnv, registry map[string]nativeProbeFunc) []ProbeResult {
	results := make([]ProbeResult, 0, 16)
	for _, id := range requiredProbeIDs() {
		probe := registry[id]
		if probe == nil {
			results = append(results, ProbeResult{ID: id, Status: StatusNotRun, Summary: "probe is not registered"})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), nativeProbeTimeout(id))
		result := probe(ctx, env)
		cancel()
		if result.ID != id {
			result = ProbeResult{ID: id, Status: StatusFail, Summary: fmt.Sprintf("probe returned id %q", result.ID)}
		}
		results = append(results, result)
	}
	return results
}

func nativeProbeTimeout(id string) time.Duration {
	switch id {
	case "P13", "P14", "P15":
		return 3 * time.Minute
	case "P3", "P5", "P6", "P10", "P11", "P12":
		return 90 * time.Second
	default:
		return 45 * time.Second
	}
}

func repositoryRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w: %s", err, strings.TrimSpace(string(out)))
	}
	root := strings.TrimSpace(string(out))
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("invalid repository root %q", root)
	}
	return filepath.Clean(root), nil
}

func validateRunPaths(repoRoot, rawDir, jsonPath string) (string, string, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", "", err
	}
	allowed := filepath.Join(root, ".build", "interactive-handoff-h0")
	resolve := func(path string) (string, error) {
		if path == "" {
			return "", errors.New("empty output path")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		return filepath.Abs(filepath.Clean(path))
	}
	rawAbs, err := resolve(rawDir)
	if err != nil {
		return "", "", err
	}
	jsonAbs, err := resolve(jsonPath)
	if err != nil {
		return "", "", err
	}
	if !pathWithin(allowed, rawAbs) || rawAbs == allowed {
		return "", "", errors.New("raw-dir must be below .build/interactive-handoff-h0")
	}
	if !pathWithin(rawAbs, jsonAbs) || jsonAbs == rawAbs {
		return "", "", errors.New("json report must be a file below raw-dir")
	}
	if err := rejectExistingSymlinkComponents(root, rawAbs); err != nil {
		return "", "", err
	}
	if err := rejectExistingSymlinkComponents(root, jsonAbs); err != nil {
		return "", "", err
	}
	return rawAbs, jsonAbs, nil
}

func rejectExistingSymlinkComponents(root, target string) error {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return err
	}
	current := filepath.Clean(root)
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("H0 output path contains symlink component %q", current)
		}
	}
	return nil
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("empty --input")
	}
	*f = append(*f, v)
	return nil
}

func runRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var inputs stringListFlag
	var gatePath, markdownPath string
	fs.Var(&inputs, "input", "native platform report path; repeat for each platform")
	fs.StringVar(&gatePath, "gate-json", "", "tracked gate JSON output")
	fs.StringVar(&markdownPath, "markdown", "", "tracked Markdown output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || len(inputs) == 0 || gatePath == "" || markdownPath == "" {
		return errors.New("render requires one or more --input plus --gate-json and --markdown")
	}
	reports := make([]BoundReport, 0, len(inputs))
	for _, path := range inputs {
		bound, err := loadBoundReport(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		reports = append(reports, bound)
	}
	gate := gateFromReports(reports)
	if err := verifyGate(gate, reports); err != nil {
		return fmt.Errorf("derived gate invalid: %w", err)
	}
	gateJSON, err := marshalDeterministic(gate)
	if err != nil {
		return err
	}
	markdown, err := renderMarkdown(gate, reports)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(gatePath, gateJSON, 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(markdownPath, markdown, 0o644); err != nil {
		return err
	}
	return nil
}

func runVerifyGate(args []string) error {
	fs := flag.NewFlagSet("verify-gate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var gatePath, platform string
	var requireH1 bool
	fs.StringVar(&gatePath, "gate-json", "", "tracked gate JSON path")
	fs.BoolVar(&requireH1, "require-h1", false, "require H1 eligibility for one platform")
	fs.StringVar(&platform, "platform", "", "platform required by --require-h1")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || gatePath == "" {
		return errors.New("verify-gate requires --gate-json")
	}
	if requireH1 && platform == "" {
		return errors.New("--require-h1 requires --platform")
	}
	if !requireH1 && platform != "" {
		return errors.New("--platform requires --require-h1")
	}
	var gate QualificationGate
	if err := decodeStrictFile(gatePath, &gate); err != nil {
		return err
	}
	reports := make([]BoundReport, 0, len(gate.PlatformReports))
	for _, binding := range gate.PlatformReports {
		bound, err := loadBoundReport(binding.ReportPath)
		if err != nil {
			return fmt.Errorf("load bound report %s: %w", binding.ReportPath, err)
		}
		reports = append(reports, bound)
	}
	if requireH1 {
		return verifyH1ForPlatform(gate, reports, platform)
	}
	return verifyGate(gate, reports)
}

func loadBoundReport(path string) (BoundReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return BoundReport{}, err
	}
	var report Report
	if err := decodeStrictBytes(raw, &report); err != nil {
		return BoundReport{}, err
	}
	if err := validateReport(report); err != nil {
		return BoundReport{}, err
	}
	sum := sha256.Sum256(raw)
	return BoundReport{
		Report:       report,
		ReportSHA256: hex.EncodeToString(sum[:]),
		Path:         path,
	}, nil
}

func decodeStrictFile(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeStrictBytes(raw, dst)
}

func decodeStrictBytes(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func collectIdentity(tmuxPath string) (identity, error) {
	if !filepath.IsAbs(tmuxPath) {
		return identity{}, errors.New("tmux path must be absolute")
	}
	info, err := os.Stat(tmuxPath)
	if err != nil {
		return identity{}, err
	}
	if !info.Mode().IsRegular() {
		return identity{}, errors.New("tmux path must name a regular file")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return identity{}, errors.New("tmux path is not executable")
	}
	raw, err := os.ReadFile(tmuxPath)
	if err != nil {
		return identity{}, err
	}
	sum := sha256.Sum256(raw)
	versionOut, err := exec.Command(tmuxPath, "-V").CombinedOutput()
	if err != nil {
		return identity{}, fmt.Errorf("tmux -V: %w: %s", err, strings.TrimSpace(string(versionOut)))
	}
	version := strings.TrimSpace(string(versionOut))
	if version == "" {
		return identity{}, errors.New("tmux -V returned empty version")
	}
	gitOut, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return identity{}, fmt.Errorf("git rev-parse HEAD: %w: %s", err, strings.TrimSpace(string(gitOut)))
	}
	gitHead := strings.TrimSpace(string(gitOut))
	if len(gitHead) != 40 {
		return identity{}, fmt.Errorf("unexpected git HEAD %q", gitHead)
	}
	return identity{
		GitHead:     gitHead,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		GoVersion:   runtime.Version(),
		TmuxPath:    tmuxPath,
		TmuxVersion: version,
		TmuxSHA256:  hex.EncodeToString(sum[:]),
	}, nil
}
