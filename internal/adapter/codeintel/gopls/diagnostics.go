package gopls

import (
	"context"
	"time"

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
	return lspadapter.RangeToByteRange(source, value, encoding)
}
