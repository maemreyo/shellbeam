package gopls

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

type diagnosticCollector struct {
	session  semanticSession
	encoding protocol.PositionEncodingKind
	wait     time.Duration
}

func newDiagnosticCollector(session semanticSession, encoding protocol.PositionEncodingKind, wait time.Duration) *diagnosticCollector {
	return &diagnosticCollector{session: session, encoding: encoding, wait: wait}
}

func (c *diagnosticCollector) Collect(ctx context.Context, documents []synchronizedDocument) appcodeintel.ProviderResponse {
	if len(documents) == 0 {
		return appcodeintel.ProviderResponse{Status: core.StatusReady}
	}
	waitCtx, cancel := context.WithTimeout(ctx, c.wait)
	defer cancel()
	response := appcodeintel.ProviderResponse{Status: core.StatusStarting}
	completed := 0
	conversionIncomplete := false
	for _, document := range documents {
		notification, ok := c.currentNotification(waitCtx, document)
		if !ok {
			continue
		}
		completed++
		for _, diagnostic := range notification.Diagnostics {
			converted, err := c.convertDiagnostic(document, diagnostic)
			if err != nil {
				conversionIncomplete = true
				continue
			}
			response.Diagnostics = append(response.Diagnostics, converted)
		}
	}
	switch {
	case completed == len(documents) && !conversionIncomplete:
		response.Status = core.StatusReady
	case completed > 0:
		response.Status = core.StatusPartial
	default:
		response.Status = core.StatusStarting
	}
	return response
}

func (c *diagnosticCollector) currentNotification(ctx context.Context, document synchronizedDocument) (lspadapter.DiagnosticNotification, bool) {
	after := document.DiagnosticAfter
	if latest, ok := c.session.LatestDiagnostics(document.URI); ok && latest.Sequence > after {
		if notificationMatchesDocument(latest, document) {
			return latest, true
		}
		after = latest.Sequence
	}
	for {
		notification, err := c.session.WaitDiagnostics(ctx, document.URI, after)
		if err != nil {
			return lspadapter.DiagnosticNotification{}, false
		}
		if notificationMatchesDocument(notification, document) {
			return notification, true
		}
		if notification.Sequence <= after {
			return lspadapter.DiagnosticNotification{}, false
		}
		after = notification.Sequence
	}
}

func notificationMatchesDocument(notification lspadapter.DiagnosticNotification, document synchronizedDocument) bool {
	return notification.URI == document.URI && notification.HasVersion && notification.Version == document.Version
}

func (c *diagnosticCollector) convertDiagnostic(document synchronizedDocument, diagnostic lspadapter.NormalizedDiagnostic) (appcodeintel.ProviderDiagnostic, error) {
	byteRange, err := lspRangeToByteRange(document.Bytes, c.encoding, diagnostic.Range)
	if err != nil {
		return appcodeintel.ProviderDiagnostic{}, err
	}
	return appcodeintel.ProviderDiagnostic{
		Severity: mapSeverity(diagnostic.Severity),
		Code:     diagnostic.Code,
		Message:  diagnostic.Message,
		Location: core.SourceLocation{
			Kind: core.LocationResolved,
			Resolved: &core.ResolvedSourceLocation{
				SourceRefID: string(document.SourceRef), StartByte: byteRange.Start, EndByte: byteRange.End,
			},
		},
		ProviderSource: diagnostic.Source,
		Authority:      core.AuthorityMechanical,
		Completeness:   core.CompletenessProviderReported,
	}, nil
}

func mapSeverity(severity protocol.DiagnosticSeverity) core.Severity {
	switch severity {
	case protocol.DiagnosticSeverityError:
		return core.SeverityError
	case protocol.DiagnosticSeverityWarning:
		return core.SeverityWarning
	default:
		return core.SeverityInfo
	}
}

func lspRangeToByteRange(source []byte, encoding protocol.PositionEncodingKind, value protocol.Range) (core.ByteRange, error) {
	start, err := lspPositionToByteOffset(source, encoding, value.Start)
	if err != nil {
		return core.ByteRange{}, err
	}
	end, err := lspPositionToByteOffset(source, encoding, value.End)
	if err != nil {
		return core.ByteRange{}, err
	}
	result := core.ByteRange{Start: start, End: end}
	if err := result.Validate(); err != nil {
		return core.ByteRange{}, err
	}
	return result, nil
}

func lspPositionToByteOffset(source []byte, encoding protocol.PositionEncodingKind, position protocol.Position) (int64, error) {
	if !utf8.Valid(source) {
		return 0, fmt.Errorf("invalid UTF-8 source")
	}
	lineStart, line, err := sourceLine(source, position.Line)
	if err != nil {
		return 0, err
	}
	within, err := encodedCharacterToByteOffset(line, encoding, position.Character)
	if err != nil {
		return 0, err
	}
	return int64(lineStart + within), nil
}

func sourceLine(source []byte, target uint32) (int, []byte, error) {
	start := 0
	line := uint32(0)
	for i, b := range source {
		if line == target && b == '\n' {
			end := i
			if end > start && source[end-1] == '\r' {
				end--
			}
			return start, source[start:end], nil
		}
		if b == '\n' {
			line++
			start = i + 1
		}
	}
	if line != target {
		return 0, nil, fmt.Errorf("LSP line out of range")
	}
	end := len(source)
	if end > start && source[end-1] == '\r' {
		end--
	}
	return start, source[start:end], nil
}

func encodedCharacterToByteOffset(line []byte, encoding protocol.PositionEncodingKind, character uint32) (int, error) {
	switch encoding {
	case protocol.PositionEncodingKindUTF8:
		if int(character) > len(line) {
			return 0, fmt.Errorf("UTF-8 position out of range")
		}
		if character < uint32(len(line)) && !utf8.RuneStart(line[character]) {
			return 0, fmt.Errorf("UTF-8 position inside rune")
		}
		return int(character), nil
	case protocol.PositionEncodingKindUTF16, protocol.PositionEncodingKindUTF32:
		return runeEncodedCharacterToByteOffset(line, encoding, character)
	default:
		return 0, fmt.Errorf("unsupported LSP position encoding %q", encoding)
	}
}

func runeEncodedCharacterToByteOffset(line []byte, encoding protocol.PositionEncodingKind, character uint32) (int, error) {
	units := uint32(0)
	byteOffset := 0
	for byteOffset < len(line) {
		if units == character {
			return byteOffset, nil
		}
		r, size := utf8.DecodeRune(line[byteOffset:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("invalid UTF-8 source")
		}
		increment := uint32(1)
		if encoding == protocol.PositionEncodingKindUTF16 && r > 0xFFFF {
			increment = 2
		}
		if units+increment > character {
			return 0, fmt.Errorf("LSP position inside encoded rune")
		}
		units += increment
		byteOffset += size
	}
	if units == character {
		return byteOffset, nil
	}
	return 0, fmt.Errorf("LSP character out of range")
}
