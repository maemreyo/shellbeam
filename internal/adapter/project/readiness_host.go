package project

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/project"
)

const (
	hostReadinessProviderVersion = 1
	maxGoVersionBytes            = 256
	maxGoModBytes                = 256 << 10
	hostGoVersionTimeout         = 2 * time.Second
)

type HostReadiness struct {
	lookPath  func(string) (string, error)
	lookupEnv func(string) (string, bool)
	goVersion func(context.Context) (string, error)
	readFile  func(string) ([]byte, error)
}

func NewHostReadiness() *HostReadiness {
	return &HostReadiness{
		lookPath: exec.LookPath, lookupEnv: os.LookupEnv,
		goVersion: runHostGoVersion, readFile: readBoundedGoMod,
	}
}

func (h *HostReadiness) ObserveExecutable(ctx context.Context, id string) core.ReadinessCheck {
	check := core.ReadinessCheck{ID: id, Kind: core.RequirementExecutable}
	if ctx.Err() != nil {
		check.Status = core.CheckUnavailable
		return check
	}
	lookPath := h.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	_, err := lookPath(id)
	switch {
	case err == nil:
		check.Status = core.CheckAvailable
	case errors.Is(err, exec.ErrNotFound):
		check.Status = core.CheckMissing
	default:
		check.Status = core.CheckUnavailable
	}
	return check
}

func (h *HostReadiness) ObserveEnvironmentPresence(ctx context.Context, id string, required bool) core.ReadinessCheck {
	check := core.ReadinessCheck{ID: id, Kind: core.RequirementEnvironmentPresence, Required: required}
	if ctx.Err() != nil {
		check.Status = core.CheckUnavailable
		return check
	}
	lookup := h.lookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(id)
	if ok && value != "" {
		check.Status = core.CheckPresentNonEmpty
	} else {
		check.Status = core.CheckAbsent
	}
	return check
}

func (h *HostReadiness) ObserveToolchain(ctx context.Context, root, id string, declaration core.Toolchain) core.ReadinessCheck {
	check := core.ReadinessCheck{ID: id, Kind: core.RequirementToolchain}
	if id != "go" || ctx.Err() != nil {
		check.Status = core.CheckUnavailable
		return check
	}
	check.ProviderID, check.ProviderVersion = "go-host", hostReadinessProviderVersion
	lookPath := h.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath("go"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			check.Status = core.CheckMissing
		} else {
			check.Status = core.CheckUnavailable
		}
		return check
	}
	expected, err := h.expectedGoVersion(root, declaration)
	if err != nil {
		check.Status = core.CheckUnknown
		return check
	}
	goVersion := h.goVersion
	if goVersion == nil {
		goVersion = runHostGoVersion
	}
	actualText, err := goVersion(ctx)
	if err != nil || len(actualText) > maxGoVersionBytes {
		check.Status = core.CheckUnknown
		return check
	}
	actual, err := parseGoVersion(actualText)
	if err != nil {
		check.Status = core.CheckUnknown
		return check
	}
	if goVersionCompatible(actual, expected) {
		check.Status = core.CheckCompatible
	} else {
		check.Status = core.CheckIncompatible
	}
	return check
}

type goVersionTuple struct {
	major int
	minor int
	patch int
}

func (h *HostReadiness) expectedGoVersion(root string, declaration core.Toolchain) (goVersionTuple, error) {
	if declaration.Version != "" {
		return parseGoVersion(declaration.Version)
	}
	if declaration.VersionSource == "" || declaration.Manager != "" {
		return goVersionTuple{}, fmt.Errorf("unsupported go version source")
	}
	path, err := safeRepositoryFile(root, declaration.VersionSource)
	if err != nil {
		return goVersionTuple{}, err
	}
	readFile := h.readFile
	if readFile == nil {
		readFile = readBoundedGoMod
	}
	data, err := readFile(path)
	if err != nil || len(data) > maxGoModBytes {
		return goVersionTuple{}, fmt.Errorf("go version source unavailable")
	}
	return parseGoModVersion(data)
}

func safeRepositoryFile(root, relative string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Base(relative) != "go.mod" {
		return "", fmt.Errorf("unsupported go version source")
	}
	joined := filepath.Join(root, filepath.FromSlash(relative))
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("go version source escapes repository")
	}
	return resolved, nil
}

func parseGoModVersion(data []byte) (goVersionTuple, error) {
	var goDirective, toolchainDirective string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "go":
			goDirective = fields[1]
		case "toolchain":
			toolchainDirective = fields[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return goVersionTuple{}, err
	}
	if toolchainDirective != "" && toolchainDirective != "default" {
		return parseGoVersion(toolchainDirective)
	}
	if goDirective != "" {
		return parseGoVersion(goDirective)
	}
	return goVersionTuple{}, fmt.Errorf("go version directive missing")
}

func parseGoVersion(value string) (goVersionTuple, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "go")
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return goVersionTuple{}, fmt.Errorf("invalid go version")
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" {
			return goVersionTuple{}, fmt.Errorf("invalid go version")
		}
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return goVersionTuple{}, fmt.Errorf("invalid go version")
		}
		values[i] = parsed
	}
	return goVersionTuple{major: values[0], minor: values[1], patch: values[2]}, nil
}

func goVersionCompatible(actual, required goVersionTuple) bool {
	if actual.major != required.major {
		return actual.major > required.major
	}
	if actual.minor != required.minor {
		return actual.minor > required.minor
	}
	return actual.patch >= required.patch
}

func readBoundedGoMod(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGoModBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGoModBytes {
		return nil, fmt.Errorf("go.mod exceeds readiness limit")
	}
	return data, nil
}

type boundedOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		copyN := min(remaining, len(p))
		b.data = append(b.data, p[:copyN]...)
	}
	if len(p) > remaining {
		b.overflow = true
	}
	return n, nil
}

func runHostGoVersion(ctx context.Context) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, hostGoVersionTimeout)
	defer cancel()
	command := exec.CommandContext(probeCtx, "go", "env", "GOVERSION")
	output := &boundedOutput{limit: maxGoVersionBytes}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", err
	}
	if output.overflow {
		return "", fmt.Errorf("go version output exceeds limit")
	}
	return strings.TrimSpace(string(output.data)), nil
}
