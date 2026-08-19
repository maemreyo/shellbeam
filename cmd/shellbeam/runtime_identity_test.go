package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"

	"github.com/maemreyo/shellbeam/internal/buildinfo"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func TestWithDaemonRuntimeIdentityPublishesSafeStartupIdentity(t *testing.T) {
	modified := false
	started := time.Date(2026, 8, 19, 1, 38, 42, 123, time.UTC)
	catalog := withDaemonRuntimeIdentity(
		capability.Baseline(capability.Limits{}),
		"01M0BTQCZYM47Y3YCPAGDDGKME",
		started,
		buildinfo.ProcessIdentity{
			Version: "v1.2.3", Revision: "86b0cb56cf7a57dd6ab1d0208bf08ffcb3acbbbf",
			VCSModified: &modified, BinarySHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	)
	if catalog.Runtime == nil {
		t.Fatal("missing runtime identity")
	}
	got := catalog.Runtime
	if got.SchemaVersion != 1 || got.Revision != "86b0cb56cf7a57dd6ab1d0208bf08ffcb3acbbbf" || got.DaemonIncarnation != "01M0BTQCZYM47Y3YCPAGDDGKME" || got.DaemonStartedAt != started.Format(time.RFC3339Nano) {
		t.Fatalf("runtime=%#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRealDaemonInspectServerPublishesSafeRuntimeIdentity(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "runtime-identity", Action: "inspect.server",
	})
	if err != nil || !response.OK || response.Server == nil || response.Server.Runtime == nil {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	runtime := response.Server.Runtime
	if err := runtime.Validate(); err != nil {
		t.Fatal(err)
	}
	if runtime.DaemonIncarnation == "" || runtime.DaemonStartedAt == "" || runtime.BinarySHA256 == "" {
		t.Fatalf("incomplete daemon runtime=%#v", runtime)
	}
	encoded, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("executable")) || bytes.Contains(encoded, []byte("/Users/")) || bytes.Contains(encoded, []byte("/private/")) {
		t.Fatalf("runtime identity leaked executable path: %s", encoded)
	}
}
