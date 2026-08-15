package outputview

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type Store interface {
	OutputExtent(context.Context, operation.SessionID) (Extent, error)
	ReadOutput(context.Context, operation.SessionID, int64, int) ([]byte, int64, error)
}
