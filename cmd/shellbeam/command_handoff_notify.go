package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	shelladapter "github.com/maemreyo/shellbeam/internal/adapter/shellintegration"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

type handoffNotifyOptions struct {
	socket, handoffID, eventID, shellRuntimeID, event, satisfied string
	epoch                                                        uint64
}

func parseHandoffNotifyArgs(args []string) (handoffNotifyOptions, error) {
	var out handoffNotifyOptions
	fs := flag.NewFlagSet("__handoff_notify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&out.socket, "socket", "", "")
	fs.StringVar(&out.handoffID, "handoff-id", "", "")
	fs.Uint64Var(&out.epoch, "epoch", 0, "")
	fs.StringVar(&out.eventID, "event-id", "", "")
	fs.StringVar(&out.shellRuntimeID, "shell-runtime-id", "", "")
	fs.StringVar(&out.event, "event", "", "")
	fs.StringVar(&out.satisfied, "satisfied", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return out, fmt.Errorf("invalid handoff notification")
	}
	if out.epoch == 0 || out.epoch > ^uint64(0)>>1 {
		return out, fmt.Errorf("invalid handoff notification epoch")
	}
	if _, err := strconv.ParseBool(out.satisfied); err != nil {
		return out, fmt.Errorf("invalid handoff notification satisfied state")
	}
	return out, nil
}

func runHandoffNotify(ctx context.Context, args []string) error {
	opts, err := parseHandoffNotifyArgs(args)
	if err != nil {
		return err
	}
	satisfied, _ := strconv.ParseBool(opts.satisfied)
	msg := shelladapter.Notification{
		HandoffID: opts.handoffID, AuthorityEpoch: delegated.AuthorityEpoch(opts.epoch), EventID: opts.eventID,
		ShellRuntimeID: opts.shellRuntimeID, Event: shelladapter.NotificationEvent(opts.event), Satisfied: satisfied,
	}
	return shelladapter.SendNotification(ctx, opts.socket, msg)
}
