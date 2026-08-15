package process

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

type childRef struct {
	parent int
	pid    int
}

func appendBoundedDescendants(ctx context.Context, host HostObserver, observation *core.Observation) error {
	if observation.Root == nil {
		return nil
	}
	seen := map[int]struct{}{observation.Root.PID: {}}
	current := []int{observation.Root.PID}
	for depth := 1; depth <= core.MaxTraversalDepth && len(current) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		childrenByParent, adapterTruncated, err := host.Children(ctx, current)
		if err != nil {
			return err
		}
		if adapterTruncated {
			markPartial(observation, core.DiagnosticLimitExceeded, true)
		}
		refs := sortedChildRefs(current, childrenByParent)
		next := make([]int, 0, len(refs))
		for _, ref := range refs {
			if _, exists := seen[ref.pid]; exists {
				continue
			}
			seen[ref.pid] = struct{}{}
			if len(observation.Descendants) >= core.MaxDescendants {
				markPartial(observation, core.DiagnosticLimitExceeded, true)
				return nil
			}
			fact, err := host.Observe(ctx, ref.pid)
			if err != nil {
				if errors.Is(err, failure.ProcessAccessDenied) || errors.Is(err, failure.ProcessNotFound) || errors.Is(err, failure.ProcessIdentityChanged) {
					markPartial(observation, childDiagnostic(err), false)
					continue
				}
				return err
			}
			fact.Relation = core.RelationShellBeamDescendant
			candidate := *observation
			candidate.Descendants = append(append([]core.ProcessFact(nil), observation.Descendants...), fact)
			encoded, err := json.Marshal(candidate)
			if err != nil {
				return err
			}
			if len(encoded) > core.MaxObservationBytes {
				markPartial(observation, core.DiagnosticLimitExceeded, true)
				return nil
			}
			observation.Descendants = append(observation.Descendants, fact)
			next = append(next, fact.PID)
		}
		if depth == core.MaxTraversalDepth && len(next) > 0 {
			markPartial(observation, core.DiagnosticLimitExceeded, true)
			return nil
		}
		current = next
	}
	return nil
}

func sortedChildRefs(parents []int, children map[int][]int) []childRef {
	refs := make([]childRef, 0)
	for _, parent := range parents {
		values := append([]int(nil), children[parent]...)
		sort.Ints(values)
		for _, pid := range values {
			if pid > 0 {
				refs = append(refs, childRef{parent: parent, pid: pid})
			}
		}
	}
	return refs
}

func childDiagnostic(err error) string {
	if errors.Is(err, failure.ProcessIdentityChanged) {
		return core.DiagnosticIdentityChanged
	}
	return core.DiagnosticObservationIncomplete
}

func markPartial(observation *core.Observation, diagnostic string, truncated bool) {
	if observation.Quality != core.QualityUnavailable {
		observation.Quality = core.QualityPartial
	}
	observation.Truncated = observation.Truncated || truncated
	for _, existing := range observation.DiagnosticCodes {
		if existing == diagnostic {
			return
		}
	}
	if len(observation.DiagnosticCodes) < core.MaxDiagnosticCodes {
		observation.DiagnosticCodes = append(observation.DiagnosticCodes, diagnostic)
	}
}

func itoa(value int) string { return strconv.Itoa(value) }
