package shellintegration

import (
	"strings"
	"testing"
)

func TestBashAdapterAppendsPromptCommandCompatiblyAndRemovesSuffixOnly(t *testing.T) {
	port := &recordingCommandPort{}
	adapter, err := NewBashAdapter(task6Deps(t, port))
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := adapter.Install(t.Context(), task6WatchRequest("bash"))
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	scripts := port.snapshot()
	if len(scripts) != 2 {
		t.Fatalf("scripts=%#v", scripts)
	}
	assertMinimalNotifierInvocation(t, scripts[0])
	if !strings.Contains(scripts[0], "compgen -A export -- CONTROL_PLANE_API_KEY") {
		t.Fatalf("bash export metadata check missing: %s", scripts[0])
	}
	if !strings.Contains(scripts[0], `PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND;}`) {
		t.Fatalf("bash did not append PROMPT_COMMAND: %s", scripts[0])
	}
	if strings.Contains(scripts[0], `PROMPT_COMMAND="__shellbeam_handoff_`) {
		t.Fatalf("bash replaced existing PROMPT_COMMAND: %s", scripts[0])
	}
	if !strings.Contains(scripts[1], `PROMPT_COMMAND="${PROMPT_COMMAND%;__shellbeam_handoff_`) {
		t.Fatalf("bash cleanup does not remove exact suffix: %s", scripts[1])
	}
}
