// Firefox execs the native messaging host directly from the manifest path
// with no arguments of ours, so the host is a separate binary rather than a
// shellbeam subcommand. It reads one message, writes one message, and exits.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	adapter "github.com/maemreyo/shellbeam/internal/adapter/browserbridge"
	bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"
	"github.com/maemreyo/shellbeam/internal/config"
)

const readTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "shellbeam-browser-host: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	paths, err := config.ResolvePaths(runtime.GOOS, os.Getuid(), home, map[string]string{
		"XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME"),
		"XDG_STATE_HOME":  os.Getenv("XDG_STATE_HOME"),
		"XDG_RUNTIME_DIR": os.Getenv("XDG_RUNTIME_DIR"),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	planner := bridgeapp.NewPlanner(adapter.NewDaemonReader(paths.Socket))
	payload, err := bridgeapp.ReadFramed(os.Stdin)
	if err != nil {
		return err
	}
	var reply bytes.Buffer
	if err := bridgeapp.Serve(ctx, planner, bytes.NewReader(payload), &reply); err != nil {
		return err
	}
	return bridgeapp.WriteFramed(os.Stdout, reply.Bytes())
}
