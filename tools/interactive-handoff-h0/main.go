package main

import (
	"bytes"
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
)

var nativeProbeRegistry = map[string]nativeProbeFunc{
	"P0": probeP0PrivateServerIdentity,
	"P1": probeP1ExactHumanClientIdentity,
	"P2": probeP2ExactClientFlagIsolation,
	"P3": probeP3SameClientIngressFence,
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
		return errors.New("usage: interactive-handoff-h0 render|verify-gate")
	}
	switch args[0] {
	case "render":
		return runRender(args[1:])
	case "verify-gate":
		return runVerifyGate(args[1:])
	case "run":
		return errors.New("run subcommand is unavailable until P0-P15 probes are implemented")
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
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
	var gatePath string
	fs.StringVar(&gatePath, "gate-json", "", "tracked gate JSON path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || gatePath == "" {
		return errors.New("verify-gate requires --gate-json")
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
