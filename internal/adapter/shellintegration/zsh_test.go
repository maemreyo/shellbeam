package shellintegration

import (
	"strings"
	"testing"
)

func TestZshAdapterUsesComposablePrecmdHookAndExactRemoval(t *testing.T) {
	port := &recordingCommandPort{}
	adapter, err := NewZshAdapter(task6Deps(t, port))
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := adapter.Install(t.Context(), task6WatchRequest("zsh"))
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
	for _, want := range []string{"typeset -ga precmd_functions", "precmd_functions+=(__shellbeam_handoff_", "${parameters[CONTROL_PLANE_API_KEY]-}"} {
		if !strings.Contains(scripts[0], want) {
			t.Fatalf("zsh install missing %q: %s", want, scripts[0])
		}
	}
	if strings.Contains(scripts[0], "precmd_functions=(__shellbeam_handoff_") {
		t.Fatalf("zsh replaced hook array instead of composing: %s", scripts[0])
	}
	if !strings.Contains(scripts[1], "precmd_functions=(${precmd_functions:#__shellbeam_handoff_") || !strings.Contains(scripts[1], "unfunction __shellbeam_handoff_") {
		t.Fatalf("zsh cleanup not exact: %s", scripts[1])
	}
}
