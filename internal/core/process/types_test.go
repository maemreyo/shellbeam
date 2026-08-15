package process

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTargetValidateAcceptsSessionOrPIDOnly(t *testing.T) {
	valid := []Target{
		{Kind: TargetSession, SessionID: "01M02CURRENTSESSION"},
		{Kind: TargetPID, PID: 123},
	}
	for _, target := range valid {
		if err := target.Validate(); err != nil {
			t.Fatalf("valid target rejected: %#v: %v", target, err)
		}
	}

	invalid := []Target{
		{},
		{Kind: TargetSession},
		{Kind: TargetSession, SessionID: "sid", PID: 1},
		{Kind: TargetPID, PID: 0},
		{Kind: TargetPID, PID: -1},
		{Kind: TargetPID, PID: 1, SessionID: "sid"},
		{Kind: "name", SessionID: "shellbeam"},
	}
	for _, target := range invalid {
		if err := target.Validate(); err == nil {
			t.Fatalf("invalid target accepted: %#v", target)
		}
	}
}

func TestIdentityBindsStableStartEvidence(t *testing.T) {
	start := time.Unix(1234, 5678).UTC()
	first, err := NewIdentity(42, start, "go")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewIdentity(42, start, "go")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first.Value, "proc_") {
		t.Fatalf("identity unstable: %#v %#v", first, second)
	}
	changed, err := NewIdentity(42, start.Add(time.Second), "go")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Value == first.Value {
		t.Fatal("changed process start time did not change identity")
	}
	if _, err := NewIdentity(42, time.Time{}, "go"); err == nil {
		t.Fatal("identity accepted without stable start evidence")
	}
}

func TestArgvViewCannotRepresentArbitraryArgumentValues(t *testing.T) {
	typ := reflect.TypeOf(ArgvView{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.ToLower(field.Name)
		if field.Type.Kind() == reflect.Slice || name == "args" || name == "arguments" || name == "argv" {
			t.Fatalf("argv view exposes arbitrary argument values through %q", field.Name)
		}
	}
}

func TestObservationValidateEnforcesHardBoundsAndIdentityHonesty(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewIdentity(100, now.Add(-time.Second), "sleep")
	if err != nil {
		t.Fatal(err)
	}
	valid := Observation{
		SchemaVersion: SchemaVersion,
		ObservedAt:    now,
		Quality:       QualityComplete,
		Target:        Target{Kind: TargetPID, PID: 100},
		Root: &ProcessFact{
			PID: 100, ParentPID: 1, Identity: &identity,
			Relation: RelationExternal, State: StateRunning,
			ExecutableIdentity: "sleep",
			ArgvView:           &ArgvView{ExecutableIdentity: "sleep", ArgumentCount: 2},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid observation rejected: %v", err)
	}

	bad := valid
	bad.Descendants = make([]ProcessFact, MaxDescendants+1)
	for i := range bad.Descendants {
		bad.Descendants[i] = ProcessFact{PID: 1000 + i, ParentPID: 100, Relation: RelationExternal, State: StateRunning}
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("descendant limit not enforced")
	}

	bad = valid
	bad.Ports = make([]PortObservation, MaxPortRecords+1)
	for i := range bad.Ports {
		bad.Ports[i] = PortObservation{PID: 100, Protocol: "tcp", LocalEndpointClass: "loopback", Port: 1000 + i, Quality: PortComplete}
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("port limit not enforced")
	}

	bad = valid
	bad.Root = &ProcessFact{PID: 100, ParentPID: 1, Relation: RelationExternal, State: StateRunning}
	if err := bad.Validate(); err != nil {
		t.Fatalf("missing optional stable identity should lower certainty, not invalidate fact: %v", err)
	}

	bad = valid
	malformed := identity
	malformed.StartTime = time.Time{}
	bad.Root = &ProcessFact{PID: 100, ParentPID: 1, Identity: &malformed, Relation: RelationExternal, State: StateRunning}
	if err := bad.Validate(); err == nil {
		t.Fatal("identity without stable evidence accepted")
	}
}

func TestUnavailableObservationMayOmitRootButCannotClaimComplete(t *testing.T) {
	value := Observation{
		SchemaVersion:   SchemaVersion,
		ObservedAt:      time.Now().UTC(),
		Quality:         QualityUnavailable,
		Target:          Target{Kind: TargetSession, SessionID: "01M02OLD"},
		DiagnosticCodes: []string{DiagnosticObservationIncomplete},
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("unavailable observation rejected: %v", err)
	}
	value.Quality = QualityComplete
	if err := value.Validate(); err == nil {
		t.Fatal("complete observation accepted without root")
	}
}
