// Package schema embeds ShellBeam's reviewed wire contracts.
package schema

import (
	"embed"
	"fmt"
)

type Name string

const (
	MCPInputV1  Name = "mcp-input-v1.json"
	MCPOutputV1 Name = "mcp-output-v1.json"
	IPCV1       Name = "ipc-v1.json"
	ReceiptV1   Name = "receipt-v1.json"
	ConfigV1    Name = "config-v1.json"
	OperationV1 Name = "operation-v1.json"
	SessionV1   Name = "session-v1.json"
)

//go:embed *.json
var files embed.FS

func Names() []Name {
	return []Name{MCPInputV1, MCPOutputV1, IPCV1, ReceiptV1, ConfigV1, OperationV1, SessionV1}
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
