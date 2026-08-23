//go:build linux || darwin

package process

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"

	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestPrivateHermeticStartClearsOuterEnvClosesStdinAndExposesStatusFD(t *testing.T) {
	t.Setenv("SHELLBEAM_HERMETIC_SECRET", "must-not-cross")
	sink := &memorySink{}
	command := hermeticapp.ProviderCommand{
		Executable: "/bin/sh",
		Argv: []string{"/bin/sh", "-c", `
if [ -n "${SHELLBEAM_HERMETIC_SECRET+x}" ]; then exit 71; fi
if IFS= read -r line; then exit 72; fi
printf '{"child-pid":4242}\n' >&3
printf clean
`},
		Dir:       t.TempDir(),
		Env:       []string{},
		StdinMode: operation.StdinModeClosed,
		StatusFD:  3,
	}
	handle, spawn, status, err := (Owner{}).StartPrivateHermetic(context.Background(), command, sink)
	if err != nil || !spawn.Attempted || !spawn.Succeeded || handle == nil || status == nil {
		t.Fatalf("handle=%T spawn=%#v status=%T err=%v", handle, spawn, status, err)
	}
	defer status.Close()
	line, err := bufio.NewReader(status).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != `{"child-pid":4242}` {
		t.Fatalf("status line=%q err=%v", line, err)
	}
	exit := handle.Wait(context.Background())
	if !exit.Reaped || exit.Code == nil || *exit.Code != 0 || exit.Signal != "" {
		t.Fatalf("literal exit=%#v", exit)
	}
	sink.mu.Lock()
	got := string(sink.b)
	sink.mu.Unlock()
	if got != "clean" {
		t.Fatalf("captured output=%q", got)
	}
}

func TestPrivateHermeticStartRejectsNonEmptyOuterEnvOrWrongStatusFDBeforeSpawn(t *testing.T) {
	for name, mutate := range map[string]func(*hermeticapp.ProviderCommand){
		"ambient env": func(c *hermeticapp.ProviderCommand) { c.Env = []string{"SECRET=value"} },
		"wrong fd":    func(c *hermeticapp.ProviderCommand) { c.StatusFD = 4 },
	} {
		t.Run(name, func(t *testing.T) {
			marker := t.TempDir() + "/spawned"
			command := hermeticapp.ProviderCommand{
				Executable: "/bin/sh", Argv: []string{"/bin/sh", "-c", "touch " + marker}, Dir: t.TempDir(),
				Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3,
			}
			mutate(&command)
			handle, spawn, status, err := (Owner{}).StartPrivateHermetic(context.Background(), command, &memorySink{})
			if err == nil || handle != nil || status != nil || spawn.Succeeded {
				t.Fatalf("unsafe command accepted handle=%T spawn=%#v status=%T err=%v", handle, spawn, status, err)
			}
			if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
				t.Fatalf("unsafe command spawned child: %v", statErr)
			}
		})
	}
}
