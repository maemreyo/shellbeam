package mcp

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/media"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
	_ "golang.org/x/image/webp"
)

func mediaSuccessV2(in input, out bridge.Response) *mcpgo.CallToolResult {
	if out.Media == nil || validateMCPMedia(in, *out.Media) != nil {
		return toolErrorV2("read_media", "invalid_daemon_response", "media result invalid", false)
	}
	result := *out.Media
	metadata := mediaStructuredMetadata(result)
	body := map[string]any{"schema_version": 2, "ok": true, "action": "read_media", "media": metadata}
	summary := fmt.Sprintf("read_media: %s %dx%d, %d bytes", result.MIMEType, result.Width, result.Height, result.ByteSize)
	return &mcpgo.CallToolResult{
		Content: []mcpgo.Content{
			&mcpgo.TextContent{Text: summary},
			&mcpgo.ImageContent{Data: result.Data, MIMEType: result.MIMEType},
		},
		StructuredContent: body,
	}
}

func validateMCPMedia(in input, result media.Result) error {
	expected, err := mediaDisplayAddressFromInput(in)
	if err != nil || result.DisplayAddress != expected || result.DisplayAddress.Validate() != nil {
		return fmt.Errorf("media identity invalid")
	}
	if result.SchemaVersion != 1 || result.Kind != "media" || result.ByteSize != len(result.Data) || result.ByteSize < 1 || result.ByteSize > media.MaxImageBytes {
		return fmt.Errorf("media envelope invalid")
	}
	if result.Width < 1 || result.Height < 1 || result.Width > media.MaxWidth || result.Height > media.MaxHeight || int64(result.Width)*int64(result.Height) > media.MaxPixels {
		return fmt.Errorf("media dimensions invalid")
	}
	format, mime, ok := mediaFormat(result.Format)
	if !ok || mime != result.MIMEType {
		return fmt.Errorf("media format invalid")
	}
	cfg, decodedFormat, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil || decodedFormat != format || cfg.Width != result.Width || cfg.Height != result.Height {
		return fmt.Errorf("media bytes invalid")
	}
	return nil
}

func mediaDisplayAddressFromInput(in input) (media.DisplayAddress, error) {
	if err := validateMediaInput(in); err != nil {
		return media.DisplayAddress{}, err
	}
	if in.WorkspaceID != "" {
		return media.DisplayAddress{AddressKind: media.AddressWorkspace, WorkspaceID: in.WorkspaceID, Path: in.Path}, nil
	}
	return media.DisplayAddress{AddressKind: media.AddressCWD, CWD: in.CWD, Path: in.Path}, nil
}

func mediaFormat(format string) (decoded, mime string, ok bool) {
	switch format {
	case "png":
		return "png", "image/png", true
	case "jpeg":
		return "jpeg", "image/jpeg", true
	case "webp":
		return "webp", "image/webp", true
	default:
		return "", "", false
	}
}

func mediaStructuredMetadata(result media.Result) map[string]any {
	return map[string]any{
		"schema_version":  result.SchemaVersion,
		"kind":            result.Kind,
		"display_address": result.DisplayAddress,
		"mime_type":       result.MIMEType,
		"format":          result.Format,
		"byte_size":       result.ByteSize,
		"width":           result.Width,
		"height":          result.Height,
	}
}
