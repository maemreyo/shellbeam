package shellintegration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeContextHelperInvocationPreservesOnlyExportedProcessContext(t *testing.T) {
	for _, tc := range task5NativeShellCases() {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := os.Stat(tc.path); err != nil {
				t.Skipf("native shell unavailable: %s", tc.path)
			}
			root := t.TempDir()
			cwd := filepath.Join(root, "delegated cwd")
			if err := os.Mkdir(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			helperDir := filepath.Join(root, "Shell Beam's; helper")
			if err := os.Mkdir(helperDir, 0o700); err != nil {
				t.Fatal(err)
			}
			helper := filepath.Join(helperDir, "shellbeam helper")
			argvCapture := filepath.Join(root, "argv")
			exportedCapture := filepath.Join(root, "exported")
			localCapture := filepath.Join(root, "local")
			cwdCapture := filepath.Join(root, "cwd")
			body := "#!/bin/sh\n" +
				"printf '%s\\n' \"$@\" > \"$H5_ARGV_CAPTURE\"\n" +
				"printf '%s\\n' \"${H5_EXPORTED-unset}\" > \"$H5_EXPORTED_CAPTURE\"\n" +
				"printf '%s\\n' \"${H5_LOCAL-unset}\" > \"$H5_LOCAL_CAPTURE\"\n" +
				"pwd > " + shellQuote(cwdCapture) + "\n"
			if err := os.WriteFile(helper, []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			invocation := contextHelperInvocation(helper, "launch_task5_01")
			script := tc.setup(cwd, argvCapture, exportedCapture, localCapture, invocation)
			cmd := exec.Command(tc.path, tc.args(script)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("native %s launch failed: %v\n%s", tc.name, err, out)
			}
			assertTask5File(t, argvCapture, "__context_exec_helper\nlaunch_task5_01")
			assertTask5File(t, exportedCapture, "task5-exported")
			assertTask5File(t, localCapture, "unset")
			assertTask5File(t, cwdCapture, cwd)
		})
	}
}

func task5LaunchDeps(t *testing.T, port *recordingCommandPort, executable string) Dependencies {
	t.Helper()
	root := t.TempDir()
	return Dependencies{Executable: executable, RuntimeDir: root, Command: port}
}

func assertTask5File(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("%s=%q want=%q", filepath.Base(path), got, want)
	}
}

func task5NativeShellCases() []struct {
	name  string
	path  string
	args  func(string) []string
	setup func(string, string, string, string, string) string
} {
	return []struct {
		name  string
		path  string
		args  func(string) []string
		setup func(string, string, string, string, string) string
	}{
		{
			name: "fish", path: "/opt/homebrew/bin/fish",
			args: func(script string) []string { return []string{"-c", script} },
			setup: func(cwd, argv, exported, local, invocation string) string {
				return "set -gx H5_ARGV_CAPTURE " + shellQuote(argv) + "\n" +
					"set -gx H5_EXPORTED_CAPTURE " + shellQuote(exported) + "\n" +
					"set -gx H5_LOCAL_CAPTURE " + shellQuote(local) + "\n" +
					"set -gx H5_EXPORTED task5-exported\n" +
					"set -g H5_LOCAL task5-local\n" +
					"cd " + shellQuote(cwd) + "\n" + invocation + "\n"
			},
		},
		{
			name: "zsh", path: "/bin/zsh",
			args: func(script string) []string { return []string{"-f", "-c", script} },
			setup: func(cwd, argv, exported, local, invocation string) string {
				return "export H5_ARGV_CAPTURE=" + shellQuote(argv) + "\n" +
					"export H5_EXPORTED_CAPTURE=" + shellQuote(exported) + "\n" +
					"export H5_LOCAL_CAPTURE=" + shellQuote(local) + "\n" +
					"export H5_EXPORTED=task5-exported\n" +
					"H5_LOCAL=task5-local\n" +
					"cd " + shellQuote(cwd) + "\n" + invocation + "\n"
			},
		},
		{
			name: "bash", path: "/bin/bash",
			args: func(script string) []string { return []string{"--noprofile", "--norc", "-c", script} },
			setup: func(cwd, argv, exported, local, invocation string) string {
				return "export H5_ARGV_CAPTURE=" + shellQuote(argv) + "\n" +
					"export H5_EXPORTED_CAPTURE=" + shellQuote(exported) + "\n" +
					"export H5_LOCAL_CAPTURE=" + shellQuote(local) + "\n" +
					"export H5_EXPORTED=task5-exported\n" +
					"H5_LOCAL=task5-local\n" +
					"cd " + shellQuote(cwd) + "\n" + invocation + "\n"
			},
		},
	}
}
