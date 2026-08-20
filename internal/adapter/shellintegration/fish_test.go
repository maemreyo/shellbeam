package shellintegration

import (
	"strings"
	"testing"
)

func TestFishAdapterInstallsPromptEventWithoutReplacingUserFunctions(t *testing.T) {
	port := &recordingCommandPort{}
	adapter, err := NewFishAdapter(task6Deps(t, port))
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := adapter.Install(t.Context(), task6WatchRequest("fish"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watcher.Close() })
	scripts := port.snapshot()
	if len(scripts) != 1 {
		t.Fatalf("scripts=%#v", scripts)
	}
	install := scripts[0]
	assertMinimalNotifierInvocation(t, install)
	for _, want := range []string{"function __shellbeam_handoff_", "--on-event fish_prompt", "set --query --export CONTROL_PLANE_API_KEY", "functions --erase"} {
		if !strings.Contains(install, want) {
			t.Fatalf("fish install missing %q: %s", want, install)
		}
	}
	if strings.Contains(install, "functions --erase fish_prompt") {
		t.Fatalf("fish prompt overwritten: %s", install)
	}
}

func TestFishAdapterCloseRemovesOnlyShellBeamFunction(t *testing.T) {
	port := &recordingCommandPort{}
	adapter, _ := NewFishAdapter(task6Deps(t, port))
	watcher, err := adapter.Install(t.Context(), task6WatchRequest("fish"))
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	scripts := port.snapshot()
	if len(scripts) != 2 || !strings.Contains(scripts[1], "functions --erase __shellbeam_handoff_") {
		t.Fatalf("cleanup=%#v", scripts)
	}
}
