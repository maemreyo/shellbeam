// Package browserbridge adapts the ShellBeam IPC client to the browser
// bridge read-only port. It is the only place in the browser bridge that
// knows a transport exists.
package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
)

type DaemonReader struct{ client *ipc.Client }

func NewDaemonReader(socket string) *DaemonReader {
	return &DaemonReader{client: ipc.NewClient(socket)}
}

func (r *DaemonReader) Read(ctx context.Context, req ipc.RequestV2) (ipc.ResponseV2, error) {
	return r.client.CallV2(ctx, req)
}
