package main

import (
	"context"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	mcpadapter "github.com/maemreyo/shellbeam/internal/adapter/mcp"
	bridgeapp "github.com/maemreyo/shellbeam/internal/app/bridge"
)

func runMCP(ctx context.Context, args []string) error {
	_, paths, err := loadCommon("mcp", args)
	if err != nil {
		return err
	}
	return mcpadapter.Run(ctx, bridgeapp.New(ipcadapter.NewClient(paths.Socket)))
}
