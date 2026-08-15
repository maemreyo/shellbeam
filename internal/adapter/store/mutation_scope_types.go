package store

import (
	"fmt"
	"reflect"
	"sort"

	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
)

const mutationScopeStoreSchema = 1
const maxMutationScopePrivateBytes = 256 << 10

type mutationScopeIndex struct {
	SchemaVersion int          `json:"schema_version"`
	Scopes        []core.Scope `json:"scopes"`
}

type mutationScopePending struct {
	SchemaVersion int                  `json:"schema_version"`
	Kind          string               `json:"kind"`
	ScopeID       string               `json:"scope_id"`
	Scope         *core.Scope          `json:"scope,omitempty"`
	Identity      *core.ScopeIdentity  `json:"identity,omitempty"`
	Receipt       core.MutationReceipt `json:"receipt"`
}

type mutationScopeClaim struct {
	SchemaVersion      int                   `json:"schema_version"`
	MutationID         string                `json:"mutation_id"`
	ScopeID            string                `json:"scope_id"`
	RequestFingerprint string                `json:"request_fingerprint"`
	Status             string                `json:"status"`
	Pending            *mutationScopePending `json:"pending,omitempty"`
	Receipt            *core.MutationReceipt `json:"receipt,omitempty"`
}

func (p mutationScopePending) validate() error {
	if p.SchemaVersion != mutationScopeStoreSchema || p.ScopeID == "" || p.Receipt.ScopeID != p.ScopeID || p.Receipt.Validate() != nil {
		return fmt.Errorf("invalid mutation scope pending")
	}
	switch p.Kind {
	case "set":
		if p.Scope == nil || p.Identity == nil || p.Scope.ScopeID != p.ScopeID || p.Identity.ScopeID != p.ScopeID || p.Receipt.Result != core.ResultSet {
			return fmt.Errorf("invalid mutation scope set pending")
		}
		if err := p.Scope.Validate(); err != nil {
			return err
		}
		if err := p.Identity.Validate(); err != nil {
			return err
		}
	case "release":
		if p.Scope != nil || p.Identity != nil || (p.Receipt.Result != core.ResultReleased && p.Receipt.Result != core.ResultAlreadyAbsent) {
			return fmt.Errorf("invalid mutation scope release pending")
		}
	default:
		return fmt.Errorf("invalid mutation scope pending kind")
	}
	return nil
}

func (c mutationScopeClaim) validate() error {
	if c.SchemaVersion != mutationScopeStoreSchema || c.MutationID == "" || c.ScopeID == "" || len(c.RequestFingerprint) != 64 {
		return fmt.Errorf("invalid mutation scope claim")
	}
	switch c.Status {
	case "prepared":
		if c.Pending == nil || c.Receipt != nil || c.Pending.Receipt.MutationID != c.MutationID || c.Pending.Receipt.RequestFingerprint != c.RequestFingerprint || c.Pending.ScopeID != c.ScopeID {
			return fmt.Errorf("invalid prepared mutation scope claim")
		}
		return c.Pending.validate()
	case "committed":
		if c.Pending != nil || c.Receipt == nil || c.Receipt.MutationID != c.MutationID || c.Receipt.RequestFingerprint != c.RequestFingerprint || c.Receipt.ScopeID != c.ScopeID {
			return fmt.Errorf("invalid committed mutation scope claim")
		}
		return c.Receipt.Validate()
	default:
		return fmt.Errorf("invalid mutation scope claim status")
	}
}

func canonicalScopeIndex(scopes []core.Scope, max int) (mutationScopeIndex, error) {
	if len(scopes) > max {
		return mutationScopeIndex{}, fmt.Errorf("mutation scope index capacity exceeded")
	}
	out := append([]core.Scope(nil), scopes...)
	sort.Slice(out, func(i, j int) bool { return out[i].ScopeID < out[j].ScopeID })
	for i := range out {
		if err := out[i].Validate(); err != nil {
			return mutationScopeIndex{}, err
		}
		if i > 0 && out[i-1].ScopeID == out[i].ScopeID {
			return mutationScopeIndex{}, fmt.Errorf("duplicate mutation scope index entry")
		}
	}
	return mutationScopeIndex{SchemaVersion: mutationScopeStoreSchema, Scopes: out}, nil
}

func samePending(a, b mutationScopePending) bool { return reflect.DeepEqual(a, b) }
