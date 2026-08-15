// Package environment observes bounded, secret-safe host environment facts.
package environment

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
)

type Host struct {
	goos      func() string
	goarch    func() string
	lookupEnv func(string) (string, bool)
}

func NewHost() *Host {
	return &Host{
		goos:      func() string { return runtime.GOOS },
		goarch:    func() string { return runtime.GOARCH },
		lookupEnv: os.LookupEnv,
	}
}

func (h *Host) Observe(ctx context.Context, execution core.ExecutionContext, relevantEnvironment []string) (core.FingerprintInput, error) {
	if err := ctx.Err(); err != nil {
		return core.FingerprintInput{}, err
	}
	goos := h.goos
	if goos == nil {
		goos = func() string { return runtime.GOOS }
	}
	goarch := h.goarch
	if goarch == nil {
		goarch = func() string { return runtime.GOARCH }
	}
	lookup := h.lookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	path, _ := lookup("PATH")
	names, err := normalizeNames(relevantEnvironment)
	if err != nil {
		return core.FingerprintInput{}, err
	}
	presence := make([]core.VariablePresence, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return core.FingerprintInput{}, err
		}
		_, present := lookup(name)
		presence = append(presence, core.VariablePresence{Name: name, Present: present})
	}
	input := core.FingerprintInput{
		Platform:         core.Platform{OS: goos(), Architecture: goarch()},
		Execution:        execution,
		Path:             core.PathFingerprint(path),
		VariablePresence: presence,
	}
	if _, err := core.EnvironmentFingerprint(input); err != nil {
		return core.FingerprintInput{}, err
	}
	return input, nil
}

func normalizeNames(values []string) ([]string, error) {
	if len(values) > core.MaxRelevantVariables {
		return nil, fmt.Errorf("too many relevant environment variables")
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			return nil, fmt.Errorf("empty environment variable name")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}
