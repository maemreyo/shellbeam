package main

import (
	"context"
	"encoding/json"
	"fmt"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	control "github.com/maemreyo/shellbeam/internal/app/control"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

func doctorReport(args []string) (control.Report, error) {
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
	report.Checks = append(report.Checks, doctorSocketCheck(paths.Socket))
	if path, e := exec.LookPath("tunnel-client"); e == nil {
		report.Checks = append(report.Checks, control.Check{ID: "tunnel_client", Status: control.Pass, Message: "tunnel-client executable found: " + filepath.Base(path)})
	} else {
		report.Checks = append(report.Checks, control.Check{ID: "tunnel_client", Status: control.Warn, Message: "tunnel-client not found", Hint: "install OpenAI Secure MCP Tunnel client separately"})
	}
	_ = cfg
	return report, nil
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
