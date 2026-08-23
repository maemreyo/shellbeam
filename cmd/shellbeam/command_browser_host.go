package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"
)

const browserHostUsage = "usage: shellbeam browser-host <install --extension-id=ID --host-path=PATH|uninstall>"

func runBrowserHost(_ context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", browserHostUsage)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		var extensionID, hostPath string
		fs := flag.NewFlagSet("browser-host install", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.StringVar(&extensionID, "extension-id", "", "the single Firefox extension id to allow")
		fs.StringVar(&hostPath, "host-path", "", "absolute path to the shellbeam-browser-host binary")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path, err := bridgeapp.InstallManifest(runtime.GOOS, home, hostPath, extensionID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "installed %s\n", path)
		return nil
	case "uninstall":
		path, err := bridgeapp.RemoveManifest(runtime.GOOS, home)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %s\n", path)
		return nil
	default:
		return fmt.Errorf("%s", browserHostUsage)
	}
}
