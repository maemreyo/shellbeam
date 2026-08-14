package gopls

import (
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDocumentSyncOpenChangeDirtyToCleanAndDelete(t *testing.T) {
	session := newFakeSession()
	session.capabilities.TextDocumentSync = protocol.TextDocumentSyncKindFull
	ws := testWorkspace(t.TempDir())
	syncer, err := newDocumentSync(session, SyncLimits{MaxOpenDocuments: 4, MaxOpenSourceBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}

	a := boundSource("pkg/a.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FAV", "package pkg\nvar A = 1\n")
	b := boundSource("pkg/a.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FAW", "package pkg\nvar A = 2\n")
	cleanA := boundSource("pkg/a.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FAX", "package pkg\nvar A = 1\n")

	first, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: a}}, workspacecore.DeltaSample{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Version != 1 || first[0].SourceRef != a.Ref.ID {
		t.Fatalf("first=%+v", first)
	}
	if len(session.openCalls) != 1 || session.openCalls[0].TextDocument.Text != string(a.Bytes) {
		t.Fatalf("didOpen=%+v", session.openCalls)
	}

	second, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: b}}, workspacecore.DeltaSample{})
	if err != nil {
		t.Fatal(err)
	}
	third, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: cleanA}}, workspacecore.DeltaSample{})
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Version != 2 || third[0].Version != 3 || len(session.changeCalls) != 2 {
		t.Fatalf("versions second=%+v third=%+v changes=%d", second, third, len(session.changeCalls))
	}
	if got := session.changeCalls[1].TextDocument.Version; got != 3 {
		t.Fatalf("dirty->clean didChange version=%d", got)
	}

	_, err = syncer.Synchronize(t.Context(), ws, nil, workspacecore.DeltaSample{Changes: []workspacecore.ChangeRecord{{
		PathTransition: workspacecore.PathDeleted, OldPath: "pkg/a.go",
		SourceTransition: workspacecore.SourceAvailabilityChanged, VCSTransition: workspacecore.VCSOther,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.closeDocs) != 1 || session.closeDocs[0].TextDocument.URI != uri.File(ws.Root+"/pkg/a.go") {
		t.Fatalf("didClose=%+v", session.closeDocs)
	}
	if len(session.watchedCalls) != 1 || len(session.watchedCalls[0].Changes) != 1 ||
		session.watchedCalls[0].Changes[0].Type != protocol.FileChangeTypeDeleted {
		t.Fatalf("watched=%+v", session.watchedCalls)
	}
}

func TestDocumentSyncIncrementalUsesMonotonicWholeReplacementRange(t *testing.T) {
	session := newFakeSession()
	session.capabilities.TextDocumentSync = protocol.TextDocumentSyncKindIncremental
	session.capabilities.PositionEncoding = protocol.PositionEncodingKindUTF16
	ws := testWorkspace(t.TempDir())
	syncer, err := newDocumentSync(session, SyncLimits{MaxOpenDocuments: 2, MaxOpenSourceBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	a := boundSource("emoji.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FAY", "package p\nvar _ = \"🙂\"\n")
	b := boundSource("emoji.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FAZ", "package p\nvar _ = \"🙂x\"\n")
	if _, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: a}}, workspacecore.DeltaSample{}); err != nil {
		t.Fatal(err)
	}
	if _, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: b}}, workspacecore.DeltaSample{}); err != nil {
		t.Fatal(err)
	}
	if len(session.changeCalls) != 1 || len(session.changeCalls[0].ContentChanges) != 1 {
		t.Fatalf("changes=%+v", session.changeCalls)
	}
	partial, ok := session.changeCalls[0].ContentChanges[0].(*protocol.TextDocumentContentChangePartial)
	if !ok || partial.Range.Start != (protocol.Position{}) || partial.Range.End.Line == 0 {
		t.Fatalf("incremental replacement=%T %+v", session.changeCalls[0].ContentChanges[0], session.changeCalls[0].ContentChanges[0])
	}
}

func TestDocumentSyncEvictsIdleEntryByClosingItBeforeAdmission(t *testing.T) {
	session := newFakeSession()
	ws := testWorkspace(t.TempDir())
	syncer, err := newDocumentSync(session, SyncLimits{MaxOpenDocuments: 1, MaxOpenSourceBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	first := boundSource("a.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FB0", "package p\n")
	second := boundSource("b.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FB1", "package p\n")
	if _, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: first}}, workspacecore.DeltaSample{}); err != nil {
		t.Fatal(err)
	}
	if _, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: second}}, workspacecore.DeltaSample{}); err != nil {
		t.Fatal(err)
	}
	if len(session.closeDocs) != 1 || len(session.openCalls) != 2 {
		t.Fatalf("close=%d open=%d", len(session.closeDocs), len(session.openCalls))
	}
	if session.closeDocs[0].TextDocument.URI != uri.File(ws.Root+"/a.go") {
		t.Fatalf("evicted uri=%q", session.closeDocs[0].TextDocument.URI)
	}
}

func TestGoSemanticContextTriggersAreAdapterOwnedAndHeadOnlyDoesNotReload(t *testing.T) {
	session := newFakeSession()
	ws := testWorkspace(t.TempDir())
	syncer, err := newDocumentSync(session, SyncLimits{MaxOpenDocuments: 2, MaxOpenSourceBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	_, err = syncer.Synchronize(t.Context(), ws, nil, workspacecore.DeltaSample{Changes: []workspacecore.ChangeRecord{{
		PathTransition: workspacecore.PathModified, NewPath: "go.mod",
		SourceTransition: workspacecore.SourceBytesChanged, VCSTransition: workspacecore.VCSOther,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.watchedCalls) != 1 || session.watchedCalls[0].Changes[0].URI != uri.File(ws.Root+"/go.mod") {
		t.Fatalf("go context notification=%+v", session.watchedCalls)
	}

	_, err = syncer.Synchronize(t.Context(), ws, nil, workspacecore.DeltaSample{Changes: []workspacecore.ChangeRecord{{
		PathTransition:   workspacecore.PathNone,
		SourceTransition: workspacecore.SourceUnchanged, VCSTransition: workspacecore.VCSHead,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.watchedCalls) != 1 {
		t.Fatalf("HEAD-only transition forced semantic reload: %d", len(session.watchedCalls))
	}
}

func TestDocumentSyncEvictsUnselectedBytesBeforeGrowingExistingDocument(t *testing.T) {
	session := newFakeSession()
	ws := testWorkspace(t.TempDir())
	syncer, err := newDocumentSync(session, SyncLimits{MaxOpenDocuments: 2, MaxOpenSourceBytes: 60})
	if err != nil {
		t.Fatal(err)
	}
	a := boundSource("a.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FB8", "package p\n")
	b := boundSource("b.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FB9", "package p\n")
	if _, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: a}, {Source: b}}, workspacecore.DeltaSample{}); err != nil {
		t.Fatal(err)
	}
	grown := boundSource("a.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBA", "package p\nvar A = \"012345678901234567890123456789\"\n")
	if len(grown.Bytes) >= 60 {
		t.Fatalf("fixture must fit individual budget: %d", len(grown.Bytes))
	}
	if _, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: grown}}, workspacecore.DeltaSample{}); err != nil {
		t.Fatal(err)
	}
	if len(session.closeDocs) != 1 || session.closeDocs[0].TextDocument.URI != uri.File(ws.Root+"/b.go") {
		t.Fatalf("expected unselected b.go eviction, closes=%+v", session.closeDocs)
	}
	if syncer.totalBytes > syncer.limits.MaxOpenSourceBytes {
		t.Fatalf("open source bytes exceeded budget: %d > %d", syncer.totalBytes, syncer.limits.MaxOpenSourceBytes)
	}
}

func TestDocumentSyncRejectsUnboundedSourceBeforeSending(t *testing.T) {
	session := newFakeSession()
	ws := testWorkspace(t.TempDir())
	syncer, err := newDocumentSync(session, SyncLimits{MaxOpenDocuments: 2, MaxOpenSourceBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	source := boundSource("a.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FB2", "package muchtoolong")
	if _, err := syncer.Synchronize(t.Context(), ws, []boundSourceView{{Source: source}}, workspacecore.DeltaSample{}); err == nil {
		t.Fatal("oversized source unexpectedly synchronized")
	}
	if len(session.openCalls) != 0 {
		t.Fatalf("didOpen sent before bound check: %d", len(session.openCalls))
	}
}

var _ = core.ProviderGoSemantic
