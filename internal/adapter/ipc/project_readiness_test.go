//go:build linux || darwin

package ipc

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/project"
)

type projectReadinessActions struct {
	fakeActions
	calls int
}

func (a *projectReadinessActions) InspectProjectReadiness(_ context.Context, workspaceID string) (core.Readiness, error) {
	a.calls++
	if workspaceID != "ws_01K00000000000000000000000" {
		return core.Readiness{}, errors.New("wrong workspace")
	}
	return testProjectReadiness(), nil
}

func TestProjectReadinessIPCV2IsClosedToWorkspaceID(t *testing.T) {
	valid := []byte(`{"ipc_version":2,"kind":"request","request_id":"readiness","action":"inspect.readiness","workspace_id":"ws_01K00000000000000000000000"}`)
	got, err := decodeRequestV2(bytesReaderV2(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "inspect.readiness" || got.WorkspaceID != "ws_01K00000000000000000000000" {
		t.Fatalf("decoded=%#v", got)
	}
	for name, raw := range map[string][]byte{
		"missing workspace": []byte(`{"ipc_version":2,"kind":"request","request_id":"readiness","action":"inspect.readiness"}`),
		"cross action":      []byte(`{"ipc_version":2,"kind":"request","request_id":"readiness","action":"inspect.readiness","workspace_id":"ws_01K00000000000000000000000","operation_id":"op-1"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRequestV2(bytesReaderV2(raw)); err == nil {
				t.Fatalf("accepted %s", raw)
			}
		})
	}
}

func TestProjectReadinessIPCV2RoutesWithoutStart(t *testing.T) {
	actions := &projectReadinessActions{}
	runtime, err := os.MkdirTemp("/tmp", "shellbeam-readiness-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	server, err := Listen(runtime, actions)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox blocks Unix sockets")
		}
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve()
	got, err := NewClient(server.SocketPath()).CallV2(context.Background(), RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "readiness", Action: "inspect.readiness",
		WorkspaceID: "ws_01K00000000000000000000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Readiness == nil || got.Readiness.State != core.ReadinessReady || actions.calls != 1 {
		t.Fatalf("response=%#v calls=%d", got, actions.calls)
	}
}

func testProjectReadiness() core.Readiness {
	return core.Readiness{
		SchemaVersion:         core.ReadinessSchemaVersion,
		State:                 core.ReadinessReady,
		RepositoryID:          "repo_01K00000000000000000000000",
		WorkspaceID:           "ws_01K00000000000000000000000",
		ManifestDigest:        strings.Repeat("a", 64),
		ManifestSchemaVersion: core.ManifestSchemaV2,
		CapturedAt:            time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
		CacheQuality:          core.CacheFresh,
		Checks:                []core.ReadinessCheck{{ID: "git", Kind: core.RequirementExecutable, Required: true, Status: core.CheckAvailable}},
	}
}
