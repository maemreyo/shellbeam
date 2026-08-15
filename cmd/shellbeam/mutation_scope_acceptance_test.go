//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	mcpadapter "github.com/maemreyo/shellbeam/internal/adapter/mcp"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestA26RealDaemonOverlapAdvisoriesAndCapability(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	_, workspace := prepareA26Workspace(t, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)

	server := callA26V2(t, client, ipcadapter.RequestV2{Action: "inspect.server", RequestID: "a26-server"})
	assertA26Catalog(t, server.Server)

	first := setA26Scope(t, client, "mutation-a", "scope-a", "activity-a", workspace.ID, core.ModeMutate, []string{"src/**"})
	if len(first.Advisories) != 0 {
		t.Fatalf("first scope advisory=%#v", first.Advisories)
	}
	second := setA26Scope(t, client, "mutation-b", "scope-b", "activity-b", workspace.ID, core.ModeMutate, []string{"src/auth/**"})
	if len(second.Advisories) != 1 {
		t.Fatalf("overlap advisories=%#v", second.Advisories)
	}
	overlap := second.Advisories[0]
	if overlap.ConflictKind != core.ConflictMutateMutate || overlap.ScopeIDs != [2]string{"scope-a", "scope-b"} || overlap.CauseFingerprint == "" {
		t.Fatalf("overlap=%#v", overlap)
	}

	disjoint := setA26Scope(t, client, "mutation-c", "scope-c", "activity-c", workspace.ID, core.ModeMutate, []string{"docs/**"})
	if len(disjoint.Advisories) != 0 {
		t.Fatalf("disjoint advisory=%#v", disjoint.Advisories)
	}
	readOne := setA26Scope(t, client, "mutation-d", "scope-d", "activity-d", workspace.ID, core.ModeRead, []string{"read/**"})
	if len(readOne.Advisories) != 0 {
		t.Fatalf("first read advisory=%#v", readOne.Advisories)
	}
	readTwo := setA26Scope(t, client, "mutation-e", "scope-e", "activity-e", workspace.ID, core.ModeRead, []string{"read/sub/**"})
	if len(readTwo.Advisories) != 0 {
		t.Fatalf("read/read advisory=%#v", readTwo.Advisories)
	}

	inspected := inspectA26Scopes(t, client, workspace.ID, "")
	if inspected.ActiveCount != 5 || len(inspected.ActiveScopes) != 5 || inspected.AdvisoryCount != 1 || len(inspected.Advisories) != 1 {
		t.Fatalf("workspace inspection=%#v", inspected)
	}
	if inspected.Advisories[0].CauseFingerprint != overlap.CauseFingerprint {
		t.Fatalf("advisory changed: set=%#v inspect=%#v", overlap, inspected.Advisories[0])
	}
}

func TestA26RealDaemonScopesNeverBlockShellOrGit(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	repo, workspace := prepareA26Workspace(t, stateDir)
	client := runA1Daemon(t, stateDir, runtimeDir)
	setA26Scope(t, client, "mutation-blocking-proof", "scope-blocking-proof", "activity-git", workspace.ID, core.ModeMutate, []string{"**"})

	commands := []ipcadapter.RequestV2{
		{Action: "start", OperationID: "a26-edit", CWD: repo, Argv: []string{"/bin/sh", "-c", "printf 'active-scope-edit\\n' >> README"}},
		{Action: "start", OperationID: "a26-stash", CWD: repo, Argv: []string{"git", "stash", "push", "-m", "a26-active-scope"}},
		{Action: "start", OperationID: "a26-switch", CWD: repo, Argv: []string{"git", "switch", "-c", "a26-scope-branch"}},
		{Action: "start", OperationID: "a26-stash-pop", CWD: repo, Argv: []string{"git", "stash", "pop"}},
		{Action: "start", OperationID: "a26-reset", CWD: repo, Argv: []string{"git", "reset", "--hard", "HEAD"}},
	}
	for _, request := range commands {
		result := callA1Terminal(t, client, request)
		assertA1ChildSuccess(t, result)
	}
	status := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a26-git-status", CWD: repo, Argv: []string{"git", "status", "--porcelain"},
	})
	assertA1ChildSuccess(t, status)
	if status.Output.Preview != "" {
		t.Fatalf("git status not clean: %q", status.Output.Preview)
	}
	inspected := inspectA26Scopes(t, client, workspace.ID, "activity-git")
	if inspected.ActiveCount != 1 || len(inspected.ActiveScopes) != 1 || inspected.ActiveScopes[0].ScopeID != "scope-blocking-proof" {
		t.Fatalf("active scope changed by ordinary git/shell execution: %#v", inspected)
	}
}

func TestA26RealDaemonNoHiddenWorkPrivacyAndMCPSafeSummary(t *testing.T) {
	fixture := setupA26PrivacyFixture(t)
	setResult := setA26Scope(t, fixture.client, "mutation-private", "scope-private", "activity-private", fixture.workspace.ID, core.ModeMutate, []string{"private/**"})
	_ = inspectA26Scopes(t, fixture.client, fixture.workspace.ID, "")
	released := callA26V2(t, fixture.client, ipcadapter.RequestV2{
		Action: "mutation_scope.release", RequestID: "a26-release", MutationID: "release-private", ScopeID: "scope-private",
	})
	if released.Mutation == nil || released.Mutation.Receipt.Result != core.ResultReleased {
		t.Fatalf("release=%#v", released.Mutation)
	}
	mcpResult := callA26PrivacyMCP(t, fixture)
	ordinary := callA1Terminal(t, fixture.client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a26-no-hidden-work", CWD: "/tmp", Argv: []string{"/bin/sh", "-c", "true"},
	})
	assertA1ChildSuccess(t, ordinary)
	assertA26NoProbeCalls(t, fixture.probeLog)
	assertA26NoForbidden(t, string(readA26Subtree(t, filepath.Join(fixture.stateDir, "mutation-scopes"))), fixture.forbidden, "durable state")
	payload, err := json.Marshal([]any{setResult, released.Mutation, mcpResult.StructuredContent})
	if err != nil {
		t.Fatal(err)
	}
	assertA26NoForbidden(t, string(payload), fixture.forbidden, "public result")
	assertA26SafeMutationEvents(t, fixture)
}

type a26PrivacyFixture struct {
	stateDir  string
	repo      string
	probeLog  string
	workspace workspacecore.Workspace
	client    *ipcadapter.Client
	catalog   capability.Catalog
	forbidden []string
}

func setupA26PrivacyFixture(t *testing.T) a26PrivacyFixture {
	t.Helper()
	stateDir, runtimeDir := a1RuntimeDirs(t)
	repo, workspace := prepareA26Workspace(t, stateDir)
	sourceSentinel := "A26_SOURCE_CONTENT_SENTINEL"
	commandSentinel := "curl --header 'Authorization: Bearer A26_COMMAND_SENTINEL'"
	secretSentinel := "A26_ENV_SECRET_SENTINEL"
	if err := os.WriteFile(filepath.Join(repo, "private-source.txt"), []byte(sourceSentinel+"\n"+commandSentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELLBEAM_A26_SECRET", secretSentinel)
	client := runA1Daemon(t, stateDir, runtimeDir)
	server := callA26V2(t, client, ipcadapter.RequestV2{Action: "inspect.server", RequestID: "a26-privacy-server"})
	assertA26Catalog(t, server.Server)
	probeLog := installA26GuardedExecutables(t)
	return a26PrivacyFixture{
		stateDir: stateDir, repo: repo, probeLog: probeLog, workspace: workspace, client: client, catalog: *server.Server,
		forbidden: []string{repo, sourceSentinel, commandSentinel, secretSentinel},
	}
}

func installA26GuardedExecutables(t *testing.T) string {
	t.Helper()
	probeLog := filepath.Join(t.TempDir(), "a26-probes.log")
	fakeBin := filepath.Join(t.TempDir(), "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"git", "ps", "lsof", "gopls", "go", "node", "python3", "java", "rustc"} {
		writeA25ProbeExecutable(t, fakeBin, name, probeLog)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return probeLog
}

func callA26PrivacyMCP(t *testing.T, fixture a26PrivacyFixture) *mcpgo.CallToolResult {
	t.Helper()
	session, closeMCP := newA26MCPSession(t, fixture.client, fixture.catalog)
	defer closeMCP()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "local_shell" {
		t.Fatalf("MCP tools=%#v", tools.Tools)
	}
	args, err := json.Marshal(map[string]any{
		"action": "mutation_scope.set", "mutation_id": "mutation-mcp", "scope_id": "scope-mcp",
		"activity_id": "activity-mcp", "workspace_id": string(fixture.workspace.ID), "mode": "read", "paths": []string{"mcp-safe/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(args)})
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("MCP result=%#v err=%v", result, err)
	}
	text, ok := result.Content[0].(*mcpgo.TextContent)
	if !ok {
		t.Fatalf("MCP text content=%T", result.Content[0])
	}
	assertA26NoForbidden(t, text.Text, append(append([]string(nil), fixture.forbidden...), "mcp-safe/**"), "MCP safe summary")
	return result
}

func assertA26NoProbeCalls(t *testing.T, probeLog string) {
	t.Helper()
	if data, err := os.ReadFile(probeLog); err == nil && len(data) > 0 {
		t.Fatalf("A2.6/ordinary path invoked guarded executable: %q", data)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertA26NoForbidden(t *testing.T, text string, forbidden []string, label string) {
	t.Helper()
	for _, value := range forbidden {
		if value != "" && strings.Contains(text, value) {
			t.Fatalf("%s leaked %q", label, value)
		}
	}
}

func assertA26SafeMutationEvents(t *testing.T, fixture a26PrivacyFixture) {
	t.Helper()
	events := callA26V2(t, fixture.client, ipcadapter.RequestV2{
		Action: "inspect.events", RequestID: "a26-events",
		Target:    &observationcore.Target{Kind: observationcore.TargetWorkspace, WorkspaceID: string(fixture.workspace.ID)},
		MaxEvents: 256,
	})
	if events.Events == nil {
		t.Fatal("inspect.events missing result")
	}
	mutationEvents := 0
	for _, event := range events.Events.Events {
		if event.Kind != observationcore.EventMutationScopeChanged {
			continue
		}
		mutationEvents++
		if event.Summary != "set" && event.Summary != "released" {
			t.Fatalf("unsafe mutation event summary=%q", event.Summary)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		assertA26NoForbidden(t, string(encoded), fixture.forbidden, "mutation event")
	}
	if mutationEvents < 3 {
		t.Fatalf("mutation events=%d want at least 3", mutationEvents)
	}
}

func prepareA26Workspace(t *testing.T, stateDir string) (string, workspacecore.Workspace) {
	t.Helper()
	repo := initWorkspaceCLIRepo(t)
	store := openA1Store(t, stateDir)
	workspace, err := workspaceapp.New(store, gitadapter.New()).Attach(context.Background(), repo, "a26-workspace")
	if err != nil {
		t.Fatal(err)
	}
	return repo, workspace
}

func callA26V2(t *testing.T, client *ipcadapter.Client, request ipcadapter.RequestV2) ipcadapter.ResponseV2 {
	t.Helper()
	request.IPVersion = 2
	request.Kind = "request"
	if request.RequestID == "" {
		request.RequestID = "a26-request"
	}
	response, err := client.CallV2(context.Background(), request)
	if err != nil || !response.OK {
		t.Fatalf("%s response=%#v envelope=%#v err=%v", request.Action, response, response.Error, err)
	}
	return response
}

func setA26Scope(t *testing.T, client *ipcadapter.Client, mutationID, scopeID, activityID string, workspaceID workspacecore.WorkspaceID, mode core.Mode, paths []string) mutationapp.MutationResult {
	t.Helper()
	response := callA26V2(t, client, ipcadapter.RequestV2{
		Action: "mutation_scope.set", RequestID: mutationID, MutationID: mutationID, ScopeID: scopeID,
		ActivityID: activityID, WorkspaceID: string(workspaceID), Mode: mode, Paths: paths,
	})
	if response.Mutation == nil {
		t.Fatal("mutation_scope.set missing mutation result")
	}
	return *response.Mutation
}

func inspectA26Scopes(t *testing.T, client *ipcadapter.Client, workspaceID workspacecore.WorkspaceID, activityID string) core.InspectResult {
	t.Helper()
	response := callA26V2(t, client, ipcadapter.RequestV2{
		Action: "inspect.mutation_scopes", RequestID: "inspect-" + activityID, WorkspaceID: string(workspaceID), ActivityID: activityID,
	})
	if response.MutationScopes == nil {
		t.Fatal("inspect.mutation_scopes missing result")
	}
	return *response.MutationScopes
}

func assertA26Catalog(t *testing.T, catalog *capability.Catalog) {
	t.Helper()
	if catalog == nil || catalog.Features[capability.FeatureMutationScopes] != capability.Available {
		t.Fatalf("mutation scope catalog=%#v", catalog)
	}
	if len(catalog.MutationScopeSchemaVersions) != 1 || catalog.MutationScopeSchemaVersions[0] != 1 {
		t.Fatalf("mutation scope schemas=%#v", catalog.MutationScopeSchemaVersions)
	}
	limits := catalog.Limits
	if limits.MutationScopeActivePerActivity != core.MaxActiveScopesPerActivity ||
		limits.MutationScopeActivePerWorkspace != core.MaxActiveScopesPerWorkspace ||
		limits.MutationScopePathsPerScope != core.MaxPathsPerScope ||
		limits.MutationScopeSelectorBytes != core.MaxSelectorBytes ||
		limits.MutationScopeAdvisories != core.MaxAdvisories ||
		limits.MutationScopeMinTTLMS != core.MinTTL.Milliseconds() ||
		limits.MutationScopeDefaultTTLMS != core.DefaultTTL.Milliseconds() ||
		limits.MutationScopeMaxTTLMS != core.MaxTTL.Milliseconds() {
		t.Fatalf("mutation scope limits=%#v", limits)
	}
}

func newA26MCPSession(t *testing.T, client *ipcadapter.Client, catalog capability.Catalog) (*mcpgo.ClientSession, func()) {
	t.Helper()
	server := mcpadapter.New(bridge.New(client), catalog)
	serverTransport, clientTransport := mcpgo.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = server.Run(ctx, serverTransport) }()
	mcpClient := mcpgo.NewClient(&mcpgo.Implementation{Name: "a26-acceptance", Version: "1"}, &mcpgo.ClientOptions{Capabilities: &mcpgo.ClientCapabilities{}})
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return session, func() {
		_ = session.Close()
		cancel()
	}
}

func readA26Subtree(t *testing.T, root string) []byte {
	t.Helper()
	var out strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(out.String())
}
