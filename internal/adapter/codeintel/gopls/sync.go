package gopls

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type SyncLimits struct {
	MaxOpenDocuments   int
	MaxOpenSourceBytes int
}

type boundSourceView struct {
	Source appcodeintel.BoundSource
}

type openDocument struct {
	URI         uri.URI
	Version     int32
	SourceRef   core.SourceRefID
	LogicalPath string
	Bytes       []byte
	lastUsed    uint64
}

type synchronizedDocument struct {
	URI             uri.URI
	Version         int32
	SourceRef       core.SourceRefID
	LogicalPath     string
	Bytes           []byte
	DiagnosticAfter uint64
}

type documentSync struct {
	session    semanticSession
	limits     SyncLimits
	documents  map[string]*openDocument
	totalBytes int
	clock      uint64
	partial    bool
}

func (l SyncLimits) Validate() error {
	if l.MaxOpenDocuments < 1 || l.MaxOpenDocuments > 4096 ||
		l.MaxOpenSourceBytes < 1 || l.MaxOpenSourceBytes > 64<<20 {
		return fmt.Errorf("invalid gopls sync limits")
	}
	return nil
}

func newDocumentSync(session semanticSession, limits SyncLimits) (*documentSync, error) {
	if session == nil {
		return nil, fmt.Errorf("nil gopls session")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &documentSync{session: session, limits: limits, documents: make(map[string]*openDocument)}, nil
}

func (s *documentSync) Synchronize(ctx context.Context, workspace workspacecore.Workspace, selected []boundSourceView, sample workspacecore.DeltaSample) ([]synchronizedDocument, error) {
	selectedPaths, selectedBytes, err := validateSelectedSources(workspace, selected, s.limits)
	if err != nil {
		return nil, err
	}
	if len(selectedPaths) > s.limits.MaxOpenDocuments || selectedBytes > s.limits.MaxOpenSourceBytes {
		return nil, fmt.Errorf("selected source sync budget exceeded")
	}
	if err := s.applyWorkspaceTransitions(ctx, workspace, sample, selectedPaths); err != nil {
		return nil, err
	}
	result := make([]synchronizedDocument, 0, len(selected))
	for _, view := range selected {
		document, err := s.synchronizeSource(ctx, workspace, view.Source, selectedPaths)
		if err != nil {
			return nil, err
		}
		result = append(result, document)
	}
	return result, nil
}

func (s *documentSync) CoveragePartial() bool { return s.partial }

func validateSelectedSources(workspace workspacecore.Workspace, selected []boundSourceView, limits SyncLimits) (map[string]struct{}, int, error) {
	paths := make(map[string]struct{}, len(selected))
	total := 0
	for _, view := range selected {
		source := view.Source
		if err := source.Ref.Validate(); err != nil || source.Ref.ResolutionQuality != core.ResolutionExact ||
			source.Ref.WorkspaceID != workspace.ID || source.Ref.LogicalPath == "" || !utf8.Valid(source.Bytes) {
			return nil, 0, fmt.Errorf("invalid exact source for gopls synchronization")
		}
		if _, exists := paths[source.Ref.LogicalPath]; exists {
			return nil, 0, fmt.Errorf("duplicate synchronized source path")
		}
		paths[source.Ref.LogicalPath] = struct{}{}
		total += len(source.Bytes)
		if total > limits.MaxOpenSourceBytes {
			return nil, 0, fmt.Errorf("selected source bytes exceed sync budget")
		}
	}
	return paths, total, nil
}

func (s *documentSync) synchronizeSource(ctx context.Context, workspace workspacecore.Workspace, source appcodeintel.BoundSource, selected map[string]struct{}) (synchronizedDocument, error) {
	path := source.Ref.LogicalPath
	absolute := filepath.Join(workspace.Root, filepath.FromSlash(path))
	documentURI := uri.File(absolute)
	s.clock++
	if current := s.documents[path]; current != nil {
		if current.SourceRef == source.Ref.ID {
			current.lastUsed = s.clock
			return snapshotDocument(current, 0), nil
		}
		sequence := latestSequence(s.session, documentURI)
		if growth := len(source.Bytes) - len(current.Bytes); growth > 0 {
			if err := s.ensureByteCapacity(ctx, selected, growth); err != nil {
				return synchronizedDocument{}, err
			}
		}
		if err := s.changeDocument(ctx, current, source); err != nil {
			return synchronizedDocument{}, err
		}
		return snapshotDocument(current, sequence), nil
	}
	if err := s.ensureAdmission(ctx, selected, len(source.Bytes)); err != nil {
		return synchronizedDocument{}, err
	}
	sequence := latestSequence(s.session, documentURI)
	opened := &openDocument{
		URI: documentURI, Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath,
		Bytes: append([]byte(nil), source.Bytes...), lastUsed: s.clock,
	}
	if err := s.session.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{
		URI: documentURI, LanguageID: protocol.LanguageKindGo, Version: opened.Version, Text: string(source.Bytes),
	}}); err != nil {
		return synchronizedDocument{}, fmt.Errorf("gopls didOpen: %w", err)
	}
	s.documents[path] = opened
	s.totalBytes += len(opened.Bytes)
	return snapshotDocument(opened, sequence), nil
}

func (s *documentSync) changeDocument(ctx context.Context, current *openDocument, source appcodeintel.BoundSource) error {
	newVersion := current.Version + 1
	if newVersion <= current.Version {
		return fmt.Errorf("gopls document version overflow")
	}
	changes, err := contentChanges(current.Bytes, source.Bytes, s.session.Capabilities())
	if err != nil {
		return err
	}
	if err := s.session.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: current.URI}, Version: newVersion,
		},
		ContentChanges: changes,
	}); err != nil {
		return fmt.Errorf("gopls didChange: %w", err)
	}
	s.totalBytes -= len(current.Bytes)
	current.Version = newVersion
	current.SourceRef = source.Ref.ID
	current.LogicalPath = source.Ref.LogicalPath
	current.Bytes = append(current.Bytes[:0], source.Bytes...)
	current.lastUsed = s.clock
	s.totalBytes += len(current.Bytes)
	return nil
}

func contentChanges(previous, next []byte, capabilities lspadapter.CapabilityState) ([]protocol.TextDocumentContentChangeEvent, error) {
	if capabilities.TextDocumentSync == protocol.TextDocumentSyncKindIncremental {
		end, err := documentEndPosition(previous, capabilities.PositionEncoding)
		if err != nil {
			return nil, err
		}
		return []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangePartial{
			Range: protocol.Range{Start: protocol.Position{}, End: end}, Text: string(next),
		}}, nil
	}
	return []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: string(next)}}, nil
}

func (s *documentSync) ensureByteCapacity(ctx context.Context, selected map[string]struct{}, growth int) error {
	for s.totalBytes+growth > s.limits.MaxOpenSourceBytes {
		path, document := s.oldestEvictable(selected)
		if document == nil {
			return fmt.Errorf("gopls open source byte budget exhausted")
		}
		if err := s.closeDocument(ctx, path, document); err != nil {
			return err
		}
	}
	return nil
}

func (s *documentSync) ensureAdmission(ctx context.Context, selected map[string]struct{}, incomingBytes int) error {
	for len(s.documents) >= s.limits.MaxOpenDocuments || s.totalBytes+incomingBytes > s.limits.MaxOpenSourceBytes {
		path, document := s.oldestEvictable(selected)
		if document == nil {
			return fmt.Errorf("gopls open document budget exhausted")
		}
		if err := s.closeDocument(ctx, path, document); err != nil {
			return err
		}
	}
	return nil
}

func (s *documentSync) oldestEvictable(selected map[string]struct{}) (string, *openDocument) {
	var chosenPath string
	var chosen *openDocument
	for path, document := range s.documents {
		if _, keep := selected[path]; keep {
			continue
		}
		if chosen == nil || document.lastUsed < chosen.lastUsed {
			chosenPath, chosen = path, document
		}
	}
	return chosenPath, chosen
}

func (s *documentSync) closeDocument(ctx context.Context, path string, document *openDocument) error {
	if err := s.session.DidClose(ctx, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: document.URI},
	}); err != nil {
		return fmt.Errorf("gopls didClose: %w", err)
	}
	delete(s.documents, path)
	s.totalBytes -= len(document.Bytes)
	return nil
}

func (s *documentSync) applyWorkspaceTransitions(ctx context.Context, workspace workspacecore.Workspace, sample workspacecore.DeltaSample, selected map[string]struct{}) error {
	var events []protocol.FileEvent
	for _, change := range sample.Changes {
		if change.PathTransition == workspacecore.PathNone && change.SourceTransition == workspacecore.SourceUnchanged {
			continue
		}
		if change.PathTransition == workspacecore.PathDeleted || change.PathTransition == workspacecore.PathReplaced {
			if document := s.documents[change.OldPath]; document != nil {
				if err := s.closeDocument(ctx, change.OldPath, document); err != nil {
					return err
				}
			}
			if change.OldPath != "" {
				events = append(events, fileEvent(workspace, change.OldPath, protocol.FileChangeTypeDeleted))
			}
		}
		if change.NewPath != "" {
			if _, exact := selected[change.NewPath]; !exact && strings.HasSuffix(change.NewPath, ".go") && change.SourceTransition != workspacecore.SourceUnchanged {
				s.partial = true
			}
			if semanticContextPath(change.NewPath) || shouldNotifyDiskChange(change, selected) {
				events = append(events, fileEvent(workspace, change.NewPath, fileChangeType(change.PathTransition)))
			}
		}
	}
	if sample.SourceViewMayHaveChanged && len(sample.Changes) == 0 {
		s.partial = true
	}
	if len(events) == 0 {
		return nil
	}
	if err := s.session.DidChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{Changes: events}); err != nil {
		return fmt.Errorf("gopls watched-file synchronization: %w", err)
	}
	return nil
}

func shouldNotifyDiskChange(change workspacecore.ChangeRecord, selected map[string]struct{}) bool {
	if _, exact := selected[change.NewPath]; exact {
		return false
	}
	return change.SourceTransition != workspacecore.SourceUnchanged
}

func semanticContextPath(path string) bool {
	base := filepath.Base(filepath.FromSlash(path))
	return base == "go.mod" || base == "go.work"
}

func fileEvent(workspace workspacecore.Workspace, logicalPath string, eventType protocol.FileChangeType) protocol.FileEvent {
	return protocol.FileEvent{
		URI:  uri.File(filepath.Join(workspace.Root, filepath.FromSlash(logicalPath))),
		Type: eventType,
	}
}

func fileChangeType(transition workspacecore.PathTransition) protocol.FileChangeType {
	if transition == workspacecore.PathAdded || transition == workspacecore.PathReplaced {
		return protocol.FileChangeTypeCreated
	}
	return protocol.FileChangeTypeChanged
}

func latestSequence(session semanticSession, documentURI uri.URI) uint64 {
	if notification, ok := session.LatestDiagnostics(documentURI); ok {
		return notification.Sequence
	}
	return 0
}

func snapshotDocument(document *openDocument, after uint64) synchronizedDocument {
	return synchronizedDocument{
		URI: document.URI, Version: document.Version, SourceRef: document.SourceRef, LogicalPath: document.LogicalPath,
		Bytes: append([]byte(nil), document.Bytes...), DiagnosticAfter: after,
	}
}

func documentEndPosition(source []byte, encoding protocol.PositionEncodingKind) (protocol.Position, error) {
	return lspadapter.ByteOffsetToPosition(source, int64(len(source)), encoding)
}
