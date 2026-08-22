package capability

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestContextExecCapabilityRequiresExactH4CompositionAndKeepsResourceClaimsSeparate(t *testing.T) {
	const feature = Feature("context_exec")
	baseline := Baseline(Limits{})
	if baseline.Features[feature] != Unavailable {
		t.Fatalf("baseline context_exec=%q want unavailable", baseline.Features[feature])
	}
	field, ok := reflect.TypeOf(Catalog{}).FieldByName("ContextExec")
	if !ok || field.Type.Kind() != reflect.Pointer {
		t.Fatal("Catalog lacks ContextExec support field")
	}
	method := reflect.ValueOf(baseline).MethodByName("WithContextExec")
	if !method.IsValid() {
		t.Fatal("Catalog lacks WithContextExec")
	}

	support := reflect.New(field.Type.Elem()).Elem()
	setContextExecSupportField(t, support, "ProviderID", "tmux_control_mode")
	setContextExecSupportField(t, support, "ProviderVersion", 1)
	setContextExecSupportField(t, support, "Platform", "darwin")
	setContextExecStringSlice(t, support, "ShellAdapters", []string{"fish", "zsh", "bash"})
	setContextExecSupportField(t, support, "HelperProtocolVersion", 3)
	setContextExecSupportField(t, support, "EvidenceAuthority", "context_exec_child_owned_v1")
	setContextExecStringSlice(t, support, "EvidenceQualities", []string{"unproven", "incomplete", "complete", "ambiguous"})
	setContextExecSupportField(t, support, "OutputAttribution", "helper_owned_child_pipes")
	setContextExecSupportField(t, support, "ResourceEnforcement", "unavailable")
	setContextExecSupportField(t, support, "Hermetic", "unavailable")

	withoutH4 := method.Call([]reflect.Value{support})[0].Interface().(Catalog)
	if withoutH4.Features[feature] != Unavailable || reflect.ValueOf(withoutH4).FieldByName("ContextExec").IsNil() == false {
		t.Fatalf("context exec advertised without H4 composition: %#v", withoutH4.Features)
	}

	qualified := baseline.
		WithDelegatedInteractive(DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).
		WithInteractiveHandoff(InteractiveHandoffSupport{
			ManualStandard: true, Secret: true,
			Privacy:          &HandoffPrivacySupport{SecretPrivateInterval: true, PrivacyReleaseSeparate: true, ObserverTopologyQualified: true, HumanInputPersisted: false},
			CaptureQualities: []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete},
		})
	method = reflect.ValueOf(qualified).MethodByName("WithContextExec")
	got := method.Call([]reflect.Value{support})[0].Interface().(Catalog)
	if got.Features[feature] != Available {
		t.Fatalf("qualified context_exec=%q want available", got.Features[feature])
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire, &body); err != nil {
		t.Fatal(err)
	}
	ctx, ok := body["context_exec"].(map[string]any)
	if !ok {
		t.Fatalf("context_exec support missing: %s", wire)
	}
	for key, want := range map[string]any{
		"provider_id": "tmux_control_mode", "provider_version": float64(1), "platform": "darwin",
		"helper_protocol_version": float64(3), "evidence_authority": "context_exec_child_owned_v1",
		"output_attribution": "helper_owned_child_pipes", "resource_enforcement": "unavailable", "hermetic": "unavailable",
	} {
		if ctx[key] != want {
			t.Fatalf("context_exec.%s=%#v want %#v; body=%s", key, ctx[key], want, wire)
		}
	}
	for _, version := range got.ReceiptSchemaVersions {
		if version == 6 {
			return
		}
	}
	t.Fatalf("context exec capability did not advertise receipt v6: %v", got.ReceiptSchemaVersions)
}

func setContextExecSupportField(t *testing.T, value reflect.Value, name string, raw any) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanSet() {
		t.Fatalf("context exec support lacks field %s", name)
	}
	input := reflect.ValueOf(raw)
	if input.Type().AssignableTo(field.Type()) {
		field.Set(input)
		return
	}
	if input.Type().ConvertibleTo(field.Type()) {
		field.Set(input.Convert(field.Type()))
		return
	}
	t.Fatalf("cannot set %s (%v) from %T", name, field.Type(), raw)
}

func setContextExecStringSlice(t *testing.T, value reflect.Value, name string, raw []string) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.Slice {
		t.Fatalf("context exec support lacks slice %s", name)
	}
	out := reflect.MakeSlice(field.Type(), len(raw), len(raw))
	for i, item := range raw {
		v := reflect.ValueOf(item)
		if !v.Type().ConvertibleTo(field.Type().Elem()) {
			t.Fatalf("cannot set %s[%d] (%v) from string", name, i, field.Type().Elem())
		}
		out.Index(i).Set(v.Convert(field.Type().Elem()))
	}
	field.Set(out)
}
