package main

import (
	"context"
	"encoding/json"
	"fmt"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"
	control "github.com/maemreyo/shellbeam/internal/app/control"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	"github.com/maemreyo/shellbeam/internal/ownership"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func runDoctor(args []string, out io.Writer) error {
	r, err := doctorReport(args)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(out).Encode(r); err != nil {
		return err
	}
	if r.ExitCode() != 0 {
		return fmt.Errorf("doctor found unsafe boundary")
	}
	return nil
}

// requireReadyFlag makes an unresponsive daemon a failure instead of a warning.
//
// doctor reports warnings with exit code 0 by design, so a caller that only
// checks the exit status cannot tell "no daemon yet" from "daemon ready". That
// is fine for a human reading the report and wrong for a startup gate: the
// socket is published before startup recovery runs, so a gate that waits for
// the socket and then runs doctor proceeds against a daemon that is not
// serving yet. Startup reconciliation alone takes most of a second on a large
// store, so the window is real rather than theoretical.
const requireReadyFlag = "--require-ready"

func doctorReport(args []string) (control.Report, error) {
	requireReady := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == requireReadyFlag {
			requireReady = true
			continue
		}
		filtered = append(filtered, arg)
	}
	args = filtered
	cfg, paths, err := loadCommon("doctor", args)
	report := control.Report{SchemaVersion: 1}
	if err != nil {
		report.Checks = append(report.Checks, control.Check{ID: "config", Status: control.Fail, Message: "configuration invalid", Hint: err.Error()})
		return report, nil
	}
	report.Checks = append(report.Checks, control.Check{ID: "config", Status: control.Pass, Message: "configuration valid"})
	for _, item := range []struct {
		id, path string
		mode     os.FileMode
	}{{"state", paths.StateDir, 0700}, {"runtime", paths.RuntimeDir, 0700}} {
		info, e := os.Lstat(item.path)
		if os.IsNotExist(e) {
			report.Checks = append(report.Checks, control.Check{ID: item.id, Status: control.Warn, Message: "directory not created yet"})
			continue
		}
		if e != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			report.Checks = append(report.Checks, control.Check{ID: item.id, Status: control.Fail, Message: "unsafe directory"})
		} else {
			report.Checks = append(report.Checks, control.Check{ID: item.id, Status: control.Pass, Message: "directory permissions safe"})
		}
	}
	socket := doctorSocketCheck(paths.Socket)
	if requireReady && socket.Status != control.Pass {
		socket.Status = control.Fail
	}
	report.Checks = append(report.Checks,
		doctorOwnerCheck("runtime_owner", paths.RuntimeDir),
		doctorOwnerCheck("state_owner", paths.StateDir),
		socket,
	)
	if path, e := exec.LookPath("tunnel-client"); e == nil {
		report.Checks = append(report.Checks, control.Check{ID: "tunnel_client", Status: control.Pass, Message: "tunnel-client executable found: " + filepath.Base(path)})
	} else {
		report.Checks = append(report.Checks, control.Check{ID: "tunnel_client", Status: control.Warn, Message: "tunnel-client not found", Hint: "install OpenAI Secure MCP Tunnel client separately"})
	}
	report.Checks = append(report.Checks, doctorFreeSpaceCheck(paths.StateDir, cfg.MinFreeSpaceBytes))
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		report.Checks = append(report.Checks, browserBridgeCheck(runtime.GOOS, home))
	}
	return report, nil
}

// doctorFreeSpaceCheck reports room on the volume holding the state store.
//
// min_free_space_bytes was configured, defaulted and validated for a long time
// without anything ever reading it, so the floor it describes was never applied
// at all. It is applied here as a warning and nowhere as a refusal, and that is
// deliberate: the number is a guess about a filesystem ShellBeam shares with
// every other process on the machine, and a daemon that declined to start over
// it would remove the operator's shell at the moment they most need one to go
// and free some room. Reporting is useful; gating on it is not.
//
// The store's own byte budget answers a different question -- how much this
// store has written -- and cannot see the disk filling underneath it.
func doctorFreeSpaceCheck(stateDir string, minimum int64) control.Check {
	available, err := storeadapter.AvailableBytes(stateDir)
	if err != nil {
		return control.Check{ID: "disk_space", Status: control.Warn, Message: "free space undetermined", Hint: err.Error()}
	}
	hint := fmt.Sprintf("available=%dMiB minimum=%dMiB", available>>20, minimum>>20)
	if minimum > 0 && available < minimum {
		return control.Check{
			ID: "disk_space", Status: control.Warn,
			Message: "free space below the configured minimum",
			Hint:    hint + "; free space on this volume or move state_dir",
		}
	}
	return control.Check{ID: "disk_space", Status: control.Pass, Message: "free space above the configured minimum", Hint: hint}
}

// doctorOwnerCheck reports who holds a directory's lifetime lease.
//
// Startup gates used to identify a running daemon by matching process command
// lines, which silently misses a daemon started with different arguments. The
// lease answers the question directly, and names the process to stop.
func doctorOwnerCheck(id, dir string) control.Check {
	held, err := ownership.Held(dir)
	if err != nil {
		return control.Check{ID: id, Status: control.Warn, Message: "daemon ownership undetermined", Hint: err.Error()}
	}
	if !held {
		return control.Check{ID: id, Status: control.Pass, Message: "no daemon owns this directory"}
	}
	owner, found, err := ownership.ReadOwner(dir)
	if err != nil || !found {
		return control.Check{ID: id, Status: control.Pass, Message: "a daemon owns this directory"}
	}
	return control.Check{
		ID: id, Status: control.Pass, Message: "a daemon owns this directory",
		Hint: fmt.Sprintf("pid=%d incarnation=%s since=%s", owner.PID, owner.Incarnation, owner.AcquiredAt.Format(time.RFC3339)),
	}
}

func doctorSocketCheck(socket string) control.Check {
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return control.Check{ID: "socket", Status: control.Warn, Message: "daemon socket unavailable"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	response, err := ipcadapter.NewClient(socket).CallV2(ctx, ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "doctor", Action: "inspect.server",
	})
	if err != nil || !response.OK || response.Server == nil {
		return control.Check{ID: "socket", Status: control.Warn, Message: "daemon IPC unavailable", Hint: "start or restart the ShellBeam daemon"}
	}
	return control.Check{ID: "socket", Status: control.Pass, Message: "daemon IPC responsive"}
}

// browserBridgeCheck reports the bootstrap facts the extension cannot see.
//
// Firefox cannot spawn a host whose manifest is missing, so host absence can
// never arrive as a protocol reply. The bridge is optional, so all bootstrap
// problems remain warnings rather than unsafe-boundary failures.
func browserBridgeCheck(goos, home string) control.Check {
	dir, err := bridgeapp.ManifestDir(goos, home)
	if err != nil {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge unsupported on this platform", Hint: err.Error()}
	}
	path := filepath.Join(dir, bridgeapp.HostName+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge manifest not installed", Hint: "run: shellbeam browser-host install --extension-id=ID --host-path=PATH"}
	}
	var manifest struct {
		Path              string   `json:"path"`
		AllowedExtensions []string `json:"allowed_extensions"`
	}
	if json.Unmarshal(raw, &manifest) != nil || len(manifest.AllowedExtensions) != 1 {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge manifest unreadable or not pinned to one extension", Hint: "reinstall with: shellbeam browser-host install"}
	}
	if _, err := os.Stat(manifest.Path); err != nil {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge host binary missing", Hint: manifest.Path}
	}
	return control.Check{ID: "browser_bridge", Status: control.Pass, Message: fmt.Sprintf("browser bridge manifest pinned to %s, protocol %d", manifest.AllowedExtensions[0], protocol.ProtocolVersion)}
}
