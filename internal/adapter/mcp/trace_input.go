package mcp

import (
	"fmt"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func validateInputTrace(v input) error {
	if v.Action == "start" {
		_, err := trace.NormalizeMode(v.TraceMode)
		return err
	}
	if v.Action != "inspect.trace" {
		return nil
	}
	if _, err := operation.ParseID(v.OperationID); err != nil {
		return err
	}
	if v.MaxResources < 1 || v.MaxResources > trace.MaxPublicResources {
		return fmt.Errorf("invalid max_resources")
	}
	return nil
}
