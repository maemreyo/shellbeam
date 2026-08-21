package browserbridge

import (
	"context"
	"testing"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
)

func TestDaemonReaderSurfacesTransportFailureRatherThanPanicking(t *testing.T) {
	reader := NewDaemonReader("/nonexistent/shellbeam-test.sock")
	_, err := reader.Read(context.Background(), ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "t", Action: "inspect.activity", ActivityID: "wt"})
	if err == nil {
		t.Fatal("expected a transport error against a missing socket")
	}
}
