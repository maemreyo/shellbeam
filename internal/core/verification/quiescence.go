package verification

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	QuiescenceSchemaVersion             = 1
	ResourceKindProcess                 = "process"
	ResourceKindPort                    = "port"
	ResourceKindPersistentSession       = "persistent_session"
	QuiescenceQualityQualifiedLifecycle = "qualified_lifecycle"
	QuiescenceQualityUnavailable        = "unavailable"
	QuiescenceQualityReceiptNegative    = "receipt_negative"
)

type QuiescenceStatus string

const (
	QuiescenceComplete    QuiescenceStatus = "complete"
	QuiescenceIncomplete  QuiescenceStatus = "incomplete"
	QuiescenceUnknown     QuiescenceStatus = "unknown"
	QuiescenceUnavailable QuiescenceStatus = "unavailable"
)

type ResourceRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type QuiescenceObservation struct {
	SchemaVersion int              `json:"schema_version"`
	OperationID   string           `json:"operation_id"`
	SessionID     string           `json:"session_id"`
	Status        QuiescenceStatus `json:"status"`
	LiveResources []ResourceRef    `json:"live_resources,omitempty"`
	Transferred   []ResourceRef    `json:"transferred_resources,omitempty"`
	Unexpected    []ResourceRef    `json:"unexpected_resources,omitempty"`
	ObservedAt    time.Time        `json:"observed_at"`
	Quality       string           `json:"quality"`
}

type QuiescenceInput struct {
	Lifecycle         *QuiescenceObservation
	AllowedTransfers  []ResourceRef
	CleanupIncomplete bool
}

func (s QuiescenceStatus) Validate() error {
	switch s {
	case QuiescenceComplete, QuiescenceIncomplete, QuiescenceUnknown, QuiescenceUnavailable:
		return nil
	default:
		return fmt.Errorf("invalid quiescence status")
	}
}

func (r ResourceRef) Validate() error {
	switch r.Kind {
	case ResourceKindProcess, ResourceKindPort, ResourceKindPersistentSession:
	default:
		return fmt.Errorf("invalid resource kind")
	}
	if len(r.Ref) < 1 || len(r.Ref) > 256 || strings.TrimSpace(r.Ref) != r.Ref || strings.ContainsAny(r.Ref, "\r\n\x00") {
		return fmt.Errorf("invalid resource ref")
	}
	return nil
}

func (o QuiescenceObservation) Validate() error {
	if o.SchemaVersion != QuiescenceSchemaVersion || o.Status.Validate() != nil || !boundedOptionalToken(o.OperationID, 128) || !boundedOptionalToken(o.SessionID, 128) || o.ObservedAt.IsZero() {
		return fmt.Errorf("invalid quiescence observation")
	}
	switch o.Quality {
	case QuiescenceQualityQualifiedLifecycle, QuiescenceQualityUnavailable, QuiescenceQualityReceiptNegative:
	default:
		return fmt.Errorf("invalid quiescence quality")
	}
	for _, refs := range [][]ResourceRef{o.LiveResources, o.Transferred, o.Unexpected} {
		if len(refs) > 256 {
			return fmt.Errorf("too many quiescence resources")
		}
		for _, ref := range refs {
			if err := ref.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

func ReconcileQuiescence(input QuiescenceInput) QuiescenceObservation {
	if input.CleanupIncomplete {
		return QuiescenceObservation{SchemaVersion: 1, Status: QuiescenceIncomplete, ObservedAt: time.Now().UTC(), Quality: QuiescenceQualityReceiptNegative}
	}
	if input.Lifecycle == nil {
		return QuiescenceObservation{SchemaVersion: 1, Status: QuiescenceUnknown, ObservedAt: time.Now().UTC(), Quality: QuiescenceQualityUnavailable}
	}
	proof := *input.Lifecycle
	proof.LiveResources = normalizeResourceRefs(proof.LiveResources)
	proof.Transferred = normalizeResourceRefs(proof.Transferred)
	proof.Unexpected = normalizeResourceRefs(proof.Unexpected)
	if proof.Validate() != nil || proof.Quality != QuiescenceQualityQualifiedLifecycle {
		if proof.Status == QuiescenceUnavailable || proof.Quality == QuiescenceQualityUnavailable {
			proof.Status, proof.Quality = QuiescenceUnavailable, QuiescenceQualityUnavailable
		} else {
			proof.Status, proof.Quality = QuiescenceUnknown, QuiescenceQualityUnavailable
		}
		return proof
	}
	if proof.Status != QuiescenceComplete {
		return proof
	}
	allowed := resourceRefSet(input.AllowedTransfers)
	claimed := resourceRefSet(proof.Transferred)
	transferred := make([]ResourceRef, 0, len(proof.Transferred))
	unexpected := append([]ResourceRef(nil), proof.Unexpected...)
	for _, live := range proof.LiveResources {
		key := resourceRefKey(live)
		if live.Kind == ResourceKindPersistentSession && claimed[key] && allowed[key] {
			transferred = append(transferred, live)
			continue
		}
		unexpected = append(unexpected, live)
	}
	proof.Transferred = normalizeResourceRefs(transferred)
	proof.Unexpected = normalizeResourceRefs(unexpected)
	if len(proof.Unexpected) != 0 {
		proof.Status = QuiescenceIncomplete
	}
	return proof
}

func normalizeResourceRefs(refs []ResourceRef) []ResourceRef {
	out := append([]ResourceRef(nil), refs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Ref < out[j].Ref
	})
	n := 0
	for _, ref := range out {
		if n == 0 || out[n-1] != ref {
			out[n] = ref
			n++
		}
	}
	return out[:n]
}
func resourceRefKey(r ResourceRef) string { return r.Kind + "\x00" + r.Ref }
func resourceRefSet(refs []ResourceRef) map[string]bool {
	out := map[string]bool{}
	for _, r := range refs {
		out[resourceRefKey(r)] = true
	}
	return out
}
