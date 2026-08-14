// Package schema embeds ShellBeam's reviewed wire contracts.
package schema

import (
	"embed"
	"fmt"
)

type Name string

const (
	MCPInputV1  Name = "mcp-input-v1.json"
	MCPInputV2  Name = "mcp-input-v2.json"
	MCPOutputV1 Name = "mcp-output-v1.json"
	MCPOutputV2 Name = "mcp-output-v2.json"
	IPCV1       Name = "ipc-v1.json"
	IPCV2       Name = "ipc-v2.json"
	ReceiptV1   Name = "receipt-v1.json"
	ReceiptV2   Name = "receipt-v2.json"
	ConfigV1    Name = "config-v1.json"
	OperationV1 Name = "operation-v1.json"
	OperationV2 Name = "operation-v2.json"
	SessionV1   Name = "session-v1.json"
)

const ProjectManifestV1 Name = "project-manifest-v1.json"

//go:embed *.json
var files embed.FS

func Names() []Name {
	return []Name{MCPInputV1, MCPInputV2, MCPOutputV1, MCPOutputV2, IPCV1, IPCV2, ReceiptV1, ReceiptV2, ConfigV1, OperationV1, OperationV2, SessionV1, ProjectManifestV1}
}

func Load(name Name) ([]byte, error) {
	valid := false
	for _, n := range Names() {
		valid = valid || name == n
	}
	if !valid {
		return nil, fmt.Errorf("unknown schema %q", name)
	}
	b, err := files.ReadFile(string(name))
	return append([]byte(nil), b...), err
}
