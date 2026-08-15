package main

import (
	"context"
	"strings"
	"testing"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestDaemonCatalogAdvertisesBoundedOutputViews(t *testing.T) {
	catalog := daemonCatalog(capability.Limits{})
	if catalog.Features[capability.FeatureOutputViews] != capability.Available {
		t.Fatalf("output views feature=%q", catalog.Features[capability.FeatureOutputViews])
	}
	if len(catalog.OutputViewSchemaVersions) != 1 || catalog.OutputViewSchemaVersions[0] != 1 {
		t.Fatalf("output schema versions=%v", catalog.OutputViewSchemaVersions)
	}
	limits := catalog.Limits
	if limits.OutputViewMaxReturnBytes != outputview.MaxReturnBytes || limits.OutputViewMaxWorkBytes != outputview.MaxWorkBytes ||
		limits.OutputViewMaxLines != outputview.MaxLines || limits.OutputViewMaxMatches != outputview.MaxMatches ||
		limits.OutputViewMaxPatternBytes != outputview.MaxPatternBytes || limits.OutputViewMaxContinuationBytes != outputview.MaxContinuationBytes {
		t.Fatalf("output view limits=%#v", limits)
	}
}

func TestOutputViewsRealDaemonAcceptance(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)
	server, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "out-server", Action: "inspect.server"})
	if err != nil || !server.OK || server.Server == nil || server.Server.Features[capability.FeatureOutputViews] != capability.Available {
		t.Fatalf("server=%#v err=%v", server, err)
	}

	terminal := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "output-views-real", CWD: "/tmp",
		Argv: []string{"/bin/sh", "-c", "printf '\\033[31mred\\033[0m\\rblue\\nalpha\\nboom one\\nmiddle\\nboom two\\n'"},
	})
	sessionID := terminal.Operation.SessionID
	if sessionID == "" || terminal.Output.RawBytes == 0 {
		t.Fatalf("terminal=%#v", terminal)
	}

	read := func(selector outputview.Selector, continuation string) outputview.Result {
		t.Helper()
		response, readErr := client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: "read-" + string(selector.Kind), Action: "read_output",
			SessionID: sessionID, Selector: &selector, Continuation: continuation,
		})
		if readErr != nil || !response.OK || response.OutputView == nil {
			t.Fatalf("selector=%#v response=%#v err=%v", selector, response, readErr)
		}
		return *response.OutputView
	}

	raw := read(outputview.Selector{Kind: outputview.SelectorRawRange, MaxBytes: 64}, "")
	if !strings.Contains(raw.Text, "\x1b[31mred") || raw.FrozenCutBytes != terminal.Output.RawBytes {
		t.Fatalf("raw=%#v terminal bytes=%d", raw, terminal.Output.RawBytes)
	}
	byteTail := read(outputview.Selector{Kind: outputview.SelectorTail, TailBytes: 9}, "")
	if byteTail.Text != "boom two\n" {
		t.Fatalf("byte tail=%#v", byteTail)
	}
	lineTail := read(outputview.Selector{Kind: outputview.SelectorTail, TailLines: 2}, "")
	if lineTail.Text != "middle\nboom two\n" {
		t.Fatalf("line tail=%#v", lineTail)
	}
	lines := read(outputview.Selector{Kind: outputview.SelectorLines, StartLine: 2, MaxLines: 2}, "")
	if lines.Text != "alpha\nboom one\n" {
		t.Fatalf("lines=%#v", lines)
	}
	preview := read(outputview.Selector{Kind: outputview.SelectorPreview, HeadBytes: 64}, "")
	if preview.Text != "blue\nalpha\nboom one\nmiddle\nboom two\n" || strings.Contains(preview.Text, "\x1b[") {
		t.Fatalf("preview=%#v", preview)
	}
	literal := read(outputview.Selector{Kind: outputview.SelectorSearch, SearchMode: outputview.SearchLiteral, Pattern: "boom", CaseSensitive: true, MaxMatches: 10}, "")
	if len(literal.Matches) != 2 || literal.Matches[0].Line != 3 || literal.Matches[1].Line != 5 || literal.Continuation != "" {
		t.Fatalf("literal=%#v", literal)
	}
	regexSelector := outputview.Selector{Kind: outputview.SelectorSearch, SearchMode: outputview.SearchRegex, Pattern: `boom (one|two)`, CaseSensitive: true, MaxMatches: 1}
	first := read(regexSelector, "")
	if len(first.Matches) != 1 || first.Matches[0].Line != 3 || first.Continuation == "" || !first.Partial {
		t.Fatalf("regex first=%#v", first)
	}
	second := read(regexSelector, first.Continuation)
	if len(second.Matches) != 1 || second.Matches[0].Line != 5 || second.Continuation != "" || second.Partial {
		t.Fatalf("regex second=%#v", second)
	}
}

type countingOutputStore struct{ extents, reads int }

func (s *countingOutputStore) OutputExtent(context.Context, operation.SessionID) (outputview.Extent, error) {
	s.extents++
	return outputview.Extent{SessionID: "s", State: outputview.RetentionRetained, Bytes: 1}, nil
}
func (s *countingOutputStore) ReadOutput(context.Context, operation.SessionID, int64, int) ([]byte, int64, error) {
	s.reads++
	return []byte("x"), 1, nil
}

type noTaxCoreActions struct{}

func (*noTaxCoreActions) Start(context.Context, daemonapp.StartRequest) (daemonapp.View, error) {
	return daemonapp.View{SessionID: "s"}, nil
}
func (*noTaxCoreActions) Poll(context.Context, daemonapp.PollRequest) (daemonapp.View, error) {
	return daemonapp.View{SessionID: "s"}, nil
}
func (*noTaxCoreActions) Write(context.Context, daemonapp.WriteRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (*noTaxCoreActions) Kill(context.Context, daemonapp.KillRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (*noTaxCoreActions) InspectServer(context.Context) (daemonapp.ServerInfo, error) {
	return daemonapp.ServerInfo{}, nil
}

func TestDaemonOrdinaryStartPollDoNotTouchOutputViewStore(t *testing.T) {
	store := &countingOutputStore{}
	key := observationcore.CursorKeyMaterial{StateRootEpoch: "epoch_00000000000000000000000000000000", Generation: "key_00000000000000000000000000000000", Secret: []byte("00000000000000000000000000000000")}
	codec, err := outputview.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	actions := &daemonActions{Actions: &noTaxCoreActions{}, output: outputview.NewWithCursor(store, codec)}
	if _, err := actions.Start(context.Background(), daemonapp.StartRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Poll(context.Background(), daemonapp.PollRequest{}); err != nil {
		t.Fatal(err)
	}
	if store.extents != 0 || store.reads != 0 {
		t.Fatalf("ordinary path output calls: extents=%d reads=%d", store.extents, store.reads)
	}
	selector := outputview.Selector{Kind: outputview.SelectorRawRange, MaxBytes: 1}
	if _, err := actions.ReadOutputView(context.Background(), outputview.Request{SessionID: "s", Selector: selector}); err != nil {
		t.Fatal(err)
	}
	if store.extents != 1 || store.reads != 1 {
		t.Fatalf("explicit read calls: extents=%d reads=%d", store.extents, store.reads)
	}
}
