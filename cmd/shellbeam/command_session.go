package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

type sessionAttachOptions struct{ handoffID string }

func parseSessionAttachArgs(args []string) (sessionAttachOptions, error) {
	var out sessionAttachOptions
	fs := flag.NewFlagSet("session attach", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&out.handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return out, err
	}
	if fs.NArg() != 0 || !validSessionHandoffID(out.handoffID) {
		return out, fmt.Errorf("usage: shellbeam session attach --handoff-id <id>")
	}
	return out, nil
}

func runSession(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 || args[0] != "attach" {
		return fmt.Errorf("usage: shellbeam session attach --handoff-id <id>")
	}
	opts, err := parseSessionAttachArgs(args[1:])
	if err != nil {
		return err
	}
	return runSessionAttach(ctx, opts.handoffID, os.Stdin, stdout, stderr)
}

func validSessionHandoffID(v string) bool {
	if len(v) < 1 || len(v) > 128 || !sessionHandoffAlphaNumeric(v[0]) {
		return false
	}
	for i := 1; i < len(v); i++ {
		b := v[i]
		if sessionHandoffAlphaNumeric(b) || b == '_' || b == '-' {
			continue
		}
		return false
	}
	return true
}

func sessionHandoffAlphaNumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
