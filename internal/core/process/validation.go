package process

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func (t Target) Validate() error {
	switch t.Kind {
	case TargetSession:
		if !safeSessionID.MatchString(t.SessionID) || t.PID != 0 {
			return fmt.Errorf("invalid session process target")
		}
	case TargetPID:
		if t.PID <= 0 || t.SessionID != "" {
			return fmt.Errorf("invalid pid process target")
		}
	default:
		return fmt.Errorf("invalid process target kind")
	}
	return nil
}

func NewIdentity(pid int, startTime time.Time, executableIdentity string) (Identity, error) {
	if pid <= 0 || startTime.IsZero() {
		return Identity{}, fmt.Errorf("stable process identity requires pid and start time")
	}
	encoded, err := json.Marshal(struct {
		Version            int    `json:"version"`
		PID                int    `json:"pid"`
		StartTime          string `json:"start_time"`
		ExecutableIdentity string `json:"executable_identity,omitempty"`
	}{
		Version:            SchemaVersion,
		PID:                pid,
		StartTime:          startTime.UTC().Format(time.RFC3339Nano),
		ExecutableIdentity: executableIdentity,
	})
	if err != nil {
		return Identity{}, err
	}
	sum := sha256.Sum256(encoded)
	return Identity{Value: "proc_" + hex.EncodeToString(sum[:]), StartTime: startTime.UTC()}, nil
}

func (o Observation) Validate() error {
	if o.SchemaVersion != SchemaVersion || o.ObservedAt.IsZero() {
		return fmt.Errorf("invalid process observation identity")
	}
	if err := o.Target.Validate(); err != nil {
		return err
	}
	if !validQuality(o.Quality) {
		return fmt.Errorf("invalid process observation quality")
	}
	if len(o.Descendants) > MaxDescendants {
		return fmt.Errorf("too many process descendants")
	}
	if len(o.Ports) > MaxPortRecords {
		return fmt.Errorf("too many process ports")
	}
	if len(o.DiagnosticCodes) > MaxDiagnosticCodes {
		return fmt.Errorf("too many process diagnostics")
	}
	if o.Root == nil {
		if o.Quality != QualityUnavailable {
			return fmt.Errorf("available process observation missing root")
		}
	} else if err := validateFact(*o.Root); err != nil {
		return err
	}
	seen := make(map[int]struct{}, len(o.Descendants)+1)
	if o.Root != nil {
		seen[o.Root.PID] = struct{}{}
	}
	for _, fact := range o.Descendants {
		if err := validateFact(fact); err != nil {
			return err
		}
		if _, ok := seen[fact.PID]; ok {
			return fmt.Errorf("duplicate process pid")
		}
		seen[fact.PID] = struct{}{}
	}
	for _, port := range o.Ports {
		if err := validatePort(port); err != nil {
			return err
		}
	}
	for _, code := range o.DiagnosticCodes {
		if !validDiagnostic(code) {
			return fmt.Errorf("invalid process diagnostic")
		}
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		return err
	}
	if len(encoded) > MaxObservationBytes {
		return fmt.Errorf("process observation exceeds byte limit")
	}
	return nil
}

func validateFact(fact ProcessFact) error {
	if fact.PID <= 0 || fact.ParentPID < 0 {
		return fmt.Errorf("invalid process fact pid")
	}
	if !validRelation(fact.Relation) || !validState(fact.State) {
		return fmt.Errorf("invalid process fact state/relation")
	}
	if fact.Identity != nil {
		if !validPrefixedDigest(fact.Identity.Value, "proc_") || fact.Identity.StartTime.IsZero() {
			return fmt.Errorf("invalid process identity")
		}
		if !fact.StartTime.IsZero() && !fact.StartTime.Equal(fact.Identity.StartTime) {
			return fmt.Errorf("process identity start time mismatch")
		}
	}
	if fact.ArgvView != nil {
		if fact.ArgvView.ArgumentCount < 0 || len(fact.ArgvView.ExecutableIdentity) > 1024 {
			return fmt.Errorf("invalid argv view")
		}
	}
	if len(fact.ExecutableIdentity) > 1024 {
		return fmt.Errorf("executable identity too large")
	}
	return nil
}

func validatePort(port PortObservation) error {
	if port.PID <= 0 || port.Port < 1 || port.Port > 65535 {
		return fmt.Errorf("invalid port observation")
	}
	if port.Protocol != "tcp" && port.Protocol != "udp" {
		return fmt.Errorf("invalid port protocol")
	}
	switch port.LocalEndpointClass {
	case "loopback", "wildcard", "local":
	default:
		return fmt.Errorf("invalid local endpoint class")
	}
	if port.Quality != PortComplete && port.Quality != PortUnavailable {
		return fmt.Errorf("invalid port quality")
	}
	return nil
}

func validQuality(value Quality) bool {
	return value == QualityComplete || value == QualityPartial || value == QualityUnavailable
}

func validRelation(value Relation) bool {
	return value == RelationShellBeamRoot || value == RelationShellBeamDescendant || value == RelationExternal
}

func validState(value State) bool {
	switch value {
	case StateRunning, StateSleeping, StateStopped, StateZombie, StateExited, StateUnknown:
		return true
	default:
		return false
	}
}

func validDiagnostic(code string) bool {
	switch code {
	case DiagnosticObservationIncomplete, DiagnosticLimitExceeded, DiagnosticPortUnavailable, DiagnosticIdentityChanged:
		return true
	default:
		return false
	}
}

func validPrefixedDigest(value, prefix string) bool {
	if len(value) != len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	_, err := hex.DecodeString(value[len(prefix):])
	return err == nil
}
