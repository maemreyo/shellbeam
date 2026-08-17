package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/maemreyo/shellbeam/api/schema"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/media"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func validateMediaInput(v input) error {
	if _, err := media.ParseLogicalPath(v.Path); err != nil {
		return err
	}
	switch {
	case v.WorkspaceID != "" && v.CWD == "":
		_, err := workspace.ParseWorkspaceID(v.WorkspaceID)
		return err
	case v.WorkspaceID == "" && v.CWD != "":
		return media.ValidateCWD(v.CWD)
	default:
		return fmt.Errorf("read_media requires exactly one address base")
	}
}

func mediaRequestFromInput(v input) *daemonapp.MediaRequest {
	return &daemonapp.MediaRequest{WorkspaceID: v.WorkspaceID, CWD: v.CWD, Path: v.Path}
}

func mediaCatalogAvailable(c capability.Catalog) bool {
	return c.Media != nil && c.Features[capability.FeatureRichLocalMedia] == capability.Available
}

func mediaCatalogForHandler(c capability.Catalog, h interface{ EffectiveCatalog() capability.Catalog }) capability.Catalog {
	out := c.Clone()
	if out.Features == nil {
		out.Features = map[capability.Feature]capability.Availability{}
	}
	effective := h.EffectiveCatalog()
	out.Media = nil
	out.Features[capability.FeatureRichLocalMedia] = capability.Unavailable
	if mediaCatalogAvailable(effective) {
		support := effective.Media.Clone()
		out.Media = &support
		out.Features[capability.FeatureRichLocalMedia] = capability.Available
	}
	return out
}

func composeMediaInputSchema() json.RawMessage {
	base := loadSchemaDocument(schema.MCPInputV2)
	fragment := loadSchemaDocument(schema.MCPReadMediaInputV1)
	stripNestedSchemaIdentity(fragment)
	branches, _ := base["oneOf"].([]any)
	base["oneOf"] = append(append([]any(nil), branches...), fragment)
	return marshalSchemaDocument(base)
}

func composeMediaOutputSchema() json.RawMessage {
	base := loadSchemaDocument(schema.MCPOutputV2)
	fragment := loadSchemaDocument(schema.MCPReadMediaOutputV1)
	stripNestedSchemaIdentity(fragment)
	inlineDisplayAddress(fragment)

	properties, _ := base["properties"].(map[string]any)
	properties = cloneAnyMap(properties)
	properties["media"] = fragment
	base["properties"] = properties
	if action, ok := properties["action"].(map[string]any); ok {
		action = cloneAnyMap(action)
		values, _ := action["enum"].([]any)
		action["enum"] = append(append([]any(nil), values...), "read_media")
		properties["action"] = action
	}
	branches, _ := base["oneOf"].([]any)
	mediaBranch := map[string]any{
		"properties": map[string]any{"ok": map[string]any{"const": true}, "action": map[string]any{"const": "read_media"}},
		"required":   []any{"media"},
	}
	base["oneOf"] = append(append([]any(nil), branches...), mediaBranch)
	return marshalSchemaDocument(base)
}

func loadSchemaDocument(name schema.Name) map[string]any {
	raw, _ := schema.Load(name)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	return doc
}

func marshalSchemaDocument(doc map[string]any) json.RawMessage {
	raw, _ := json.Marshal(doc)
	return raw
}

func stripNestedSchemaIdentity(doc map[string]any) {
	delete(doc, "$schema")
	delete(doc, "$id")
	delete(doc, "title")
}

func inlineDisplayAddress(fragment map[string]any) {
	defs, _ := fragment["$defs"].(map[string]any)
	display, _ := defs["display_address"].(map[string]any)
	props, _ := fragment["properties"].(map[string]any)
	if display != nil && props != nil {
		props["display_address"] = display
	}
	delete(fragment, "$defs")
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
