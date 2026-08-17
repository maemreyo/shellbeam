package main

import (
	"context"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	mcpadapter "github.com/maemreyo/shellbeam/internal/adapter/mcp"
	bridgeapp "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func runMCP(ctx context.Context, args []string) error {
	_, paths, err := loadCommon("mcp", args)
	if err != nil {
		return err
	}
	handler, err := bridgeapp.NewNegotiated(ctx, ipcadapter.NewClient(paths.Socket), capability.V1MediaSupport())
	if err != nil {
		return err
	}
	return mcpadapter.Run(ctx, handler)
}
