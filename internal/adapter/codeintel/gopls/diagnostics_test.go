package gopls

import (
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

func TestDiagnosticsCollectorCorrelatesMatchingDocumentVersionToExactSourceRef(t *testing.T) {
	session := newFakeSession()
	documentURI := uri.File("/workspace/main.go")
	doc := synchronizedDocument{
		URI: documentURI, Version: 1,
		SourceRef: core.SourceRefID("src_01ARZ3NDEKTSV4RRFFQ69G5FB3"), LogicalPath: "main.go",
		Bytes: []byte("package p\nvar X = missing\n"),
	}
	session.pushDiagnostics(documentURI, lspadapter.DiagnosticNotification{
		URI: documentURI, Version: 1, HasVersion: true, Sequence: 1,
		Diagnostics: []lspadapter.NormalizedDiagnostic{{
			Range:    protocol.Range{Start: protocol.Position{Line: 1, Character: 8}, End: protocol.Position{Line: 1, Character: 15}},
			Severity: protocol.DiagnosticSeverityError, Code: "UndeclaredName", Source: "compiler", Message: "undefined: missing",
		}},
	})
	collector := newDiagnosticCollector(session, protocol.PositionEncodingKindUTF8, 50*time.Millisecond)
	response := collector.Collect(t.Context(), []synchronizedDocument{doc})
	if response.Status != core.StatusReady || len(response.Diagnostics) != 1 {
		t.Fatalf("response=%+v", response)
	}
	location := response.Diagnostics[0].Location
	if location.Kind != core.LocationResolved || location.Resolved == nil || location.Resolved.SourceRefID != string(doc.SourceRef) {
		t.Fatalf("location=%+v", location)
	}
	if location.Resolved.Display == nil || location.Resolved.Display.Path != "main.go" || location.Resolved.Display.Line != 2 || location.Resolved.Display.Column != 9 || location.Resolved.Display.EndLine != 2 || location.Resolved.Display.EndColumn != 16 || location.Resolved.Display.Preview != "var X = missing" {
		t.Fatalf("display=%+v", location.Resolved.Display)
	}
	if response.Diagnostics[0].Severity != core.SeverityError || response.Diagnostics[0].ProviderSource != "compiler" {
		t.Fatalf("diagnostic=%+v", response.Diagnostics[0])
	}
}

func TestDiagnosticsCollectorSkipsOldVersionUntilCurrentArrives(t *testing.T) {
	session := newFakeSession()
	documentURI := uri.File("/workspace/main.go")
	doc := synchronizedDocument{
		URI: documentURI, Version: 2,
		SourceRef: core.SourceRefID("src_01ARZ3NDEKTSV4RRFFQ69G5FB4"),
		Bytes:     []byte("package p\nvar B = 2\n"),
	}
	session.pushDiagnostics(documentURI, lspadapter.DiagnosticNotification{
		URI: documentURI, Version: 1, HasVersion: true, Sequence: 1,
		Diagnostics: []lspadapter.NormalizedDiagnostic{{Message: "stale A"}},
	})
	session.pushDiagnostics(documentURI, lspadapter.DiagnosticNotification{
		URI: documentURI, Version: 2, HasVersion: true, Sequence: 2,
		Diagnostics: []lspadapter.NormalizedDiagnostic{{
			Range:    protocol.Range{Start: protocol.Position{}, End: protocol.Position{Character: 1}},
			Severity: protocol.DiagnosticSeverityWarning, Message: "current B",
		}},
	})
	response := newDiagnosticCollector(session, protocol.PositionEncodingKindUTF8, 50*time.Millisecond).
		Collect(t.Context(), []synchronizedDocument{doc})
	if response.Status != core.StatusReady || len(response.Diagnostics) != 1 || response.Diagnostics[0].Message != "current B" {
		t.Fatalf("response=%+v", response)
	}
}

func TestDiagnosticsCollectorTimeoutIsStartingOrPartialNotFakeReady(t *testing.T) {
	session := newFakeSession()
	firstURI := uri.File("/workspace/first.go")
	secondURI := uri.File("/workspace/second.go")
	first := synchronizedDocument{
		URI: firstURI, Version: 1,
		SourceRef: core.SourceRefID("src_01ARZ3NDEKTSV4RRFFQ69G5FB5"), Bytes: []byte("package p\n"),
	}
	second := synchronizedDocument{
		URI: secondURI, Version: 1,
		SourceRef: core.SourceRefID("src_01ARZ3NDEKTSV4RRFFQ69G5FB6"), Bytes: []byte("package p\n"),
	}
	collector := newDiagnosticCollector(session, protocol.PositionEncodingKindUTF8, 5*time.Millisecond)
	starting := collector.Collect(t.Context(), []synchronizedDocument{first})
	if starting.Status != core.StatusStarting || len(starting.Diagnostics) != 0 {
		t.Fatalf("starting=%+v", starting)
	}

	session.pushDiagnostics(firstURI, lspadapter.DiagnosticNotification{
		URI: firstURI, Version: 1, HasVersion: true, Sequence: 1,
		Diagnostics: []lspadapter.NormalizedDiagnostic{{Severity: protocol.DiagnosticSeverityInformation, Message: "known"}},
	})
	partial := collector.Collect(t.Context(), []synchronizedDocument{first, second})
	if partial.Status != core.StatusPartial || len(partial.Diagnostics) != 1 {
		t.Fatalf("partial=%+v", partial)
	}
}

func TestDiagnosticsCollectorIgnoresNonSelectedSemanticFile(t *testing.T) {
	session := newFakeSession()
	selectedURI := uri.File("/workspace/selected.go")
	otherURI := uri.File("/workspace/other.go")
	doc := synchronizedDocument{
		URI: selectedURI, Version: 1,
		SourceRef: core.SourceRefID("src_01ARZ3NDEKTSV4RRFFQ69G5FB7"), Bytes: []byte("package p\n"),
	}
	session.pushDiagnostics(otherURI, lspadapter.DiagnosticNotification{
		URI: otherURI, Version: 1, HasVersion: true, Sequence: 1,
		Diagnostics: []lspadapter.NormalizedDiagnostic{{Severity: protocol.DiagnosticSeverityError, Message: "other"}},
	})
	session.pushDiagnostics(selectedURI, lspadapter.DiagnosticNotification{
		URI: selectedURI, Version: 1, HasVersion: true, Sequence: 2,
		Diagnostics: []lspadapter.NormalizedDiagnostic{{Severity: protocol.DiagnosticSeverityWarning, Message: "selected"}},
	})
	response := newDiagnosticCollector(session, protocol.PositionEncodingKindUTF8, 50*time.Millisecond).
		Collect(t.Context(), []synchronizedDocument{doc})
	if len(response.Diagnostics) != 1 || response.Diagnostics[0].Message != "selected" {
		t.Fatalf("response=%+v", response)
	}
}

func TestRangeToByteRangeHandlesUTF16EmojiAndRejectsMidRunePositions(t *testing.T) {
	source := []byte("a🙂b\n")
	got, err := lspRangeToByteRange(source, protocol.PositionEncodingKindUTF16, protocol.Range{
		Start: protocol.Position{Line: 0, Character: 1},
		End:   protocol.Position{Line: 0, Character: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Start != 1 || got.End != 5 {
		t.Fatalf("range=%+v", got)
	}
	if _, err := lspRangeToByteRange(source, protocol.PositionEncodingKindUTF16, protocol.Range{
		Start: protocol.Position{Line: 0, Character: 2}, End: protocol.Position{Line: 0, Character: 3},
	}); err == nil {
		t.Fatal("UTF-16 position inside surrogate pair unexpectedly accepted")
	}
}
