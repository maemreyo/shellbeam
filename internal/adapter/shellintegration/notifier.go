package shellintegration

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type CommandPort interface {
	WriteShell(context.Context, string) error
}

type Dependencies struct {
	Executable string
	RuntimeDir string
	Command    CommandPort
}

func (v Dependencies) validate() error {
	if !filepath.IsAbs(v.Executable) || filepath.Clean(v.Executable) != v.Executable {
		return fmt.Errorf("shellbeam executable must be an absolute clean path")
	}
	if !filepath.IsAbs(v.RuntimeDir) || filepath.Clean(v.RuntimeDir) != v.RuntimeDir {
		return fmt.Errorf("shell integration runtime must be an absolute clean path")
	}
	if v.Command == nil {
		return fmt.Errorf("shell integration command port unavailable")
	}
	info, err := os.Stat(v.RuntimeDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("shell integration runtime unavailable")
	}
	return nil
}

type NotificationEvent string

const (
	NotificationPromptBoundary NotificationEvent = "prompt_boundary"
	NotificationHookInstalled  NotificationEvent = "hook_installed"
)

type Notification struct {
	HandoffID      string                   `json:"handoff_id"`
	AuthorityEpoch delegated.AuthorityEpoch `json:"authority_epoch"`
	EventID        string                   `json:"event_id"`
	ShellRuntimeID string                   `json:"shell_runtime_id"`
	Event          NotificationEvent        `json:"event"`
	Satisfied      bool                     `json:"satisfied"`
}

func (v Notification) Validate() error {
	if !safeOpaque(v.HandoffID, 128) || !safeOpaque(v.EventID, 128) || !safeOpaque(v.ShellRuntimeID, 256) {
		return fmt.Errorf("invalid shell notification identity")
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	switch v.Event {
	case NotificationPromptBoundary:
	case NotificationHookInstalled:
		if v.Satisfied {
			return fmt.Errorf("hook installation acknowledgement cannot satisfy requirement")
		}
	default:
		return fmt.Errorf("invalid shell notification event")
	}
	return nil
}

func SendNotification(ctx context.Context, socket string, notification Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return fmt.Errorf("invalid notification socket")
	}
	if len(socket) >= 100 {
		return fmt.Errorf("notification socket path too long")
	}
	if err := notification.Validate(); err != nil {
		return err
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	encoder := json.NewEncoder(conn)
	return encoder.Encode(notification)
}

type oneShotWatcher struct {
	req         app.WatchRequest
	expected    Notification
	listener    net.Listener
	socketPath  string
	command     CommandPort
	cleanup     string
	transport   sync.Once
	cleanupOnce sync.Once
	cleanupErr  error
}

func newOneShotWatcher(req app.WatchRequest, deps Dependencies, installBuilder func(app.WatchRequest, string, string, string) (string, string)) (*oneShotWatcher, string, error) {
	if err := req.Validate(); err != nil {
		return nil, "", err
	}
	if err := deps.validate(); err != nil {
		return nil, "", err
	}
	eventID, err := newEventID()
	if err != nil {
		return nil, "", err
	}
	socketPath := filepath.Join(deps.RuntimeDir, ".hn_"+strings.TrimPrefix(eventID, "evt_")+".sock")
	if len(socketPath) >= 100 {
		return nil, "", fmt.Errorf("notification socket path too long")
	}
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, "", err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, "", err
	}
	trueNotify := notifierInvocation(deps.Executable, socketPath, req, eventID, true)
	falseNotify := notifierInvocation(deps.Executable, socketPath, req, eventID, false)
	installedNotify := notifierInvocationForEvent(deps.Executable, socketPath, req, eventID, NotificationHookInstalled, false)
	install, cleanup := installBuilder(req, eventID, trueNotify, falseNotify)
	watcher := &oneShotWatcher{
		req: req, expected: Notification{HandoffID: req.HandoffID, AuthorityEpoch: req.AuthorityEpoch, EventID: eventID, ShellRuntimeID: req.Shell.RuntimeID, Event: NotificationPromptBoundary},
		listener: listener, socketPath: socketPath, command: deps.Command, cleanup: cleanup,
	}
	// Deliver registration and its acknowledgement as one top-level eval. This
	// prevents an interactive prompt from running the newly registered hook
	// between registration and the acknowledgement.
	delivery := "eval " + shellQuote(install+"\n"+installedNotify)
	if err := deps.Command.WriteShell(context.Background(), delivery); err != nil {
		watcher.closeTransport()
		return nil, "", err
	}
	ackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	ack, err := watcher.receiveNotification(ackCtx)
	cancel()
	if err != nil {
		_ = watcher.Close()
		return nil, "", fmt.Errorf("shell hook installation acknowledgement failed: %w", err)
	}
	wantAck := watcher.expected
	wantAck.Event = NotificationHookInstalled
	wantAck.Satisfied = false
	if ack != wantAck {
		_ = watcher.Close()
		return nil, "", fmt.Errorf("shell hook installation acknowledgement mismatch")
	}
	return watcher, delivery, nil
}

func (w *oneShotWatcher) Wait(ctx context.Context) (app.WatchEvent, error) {
	if w == nil || w.listener == nil {
		return app.WatchEvent{}, fmt.Errorf("shell watcher unavailable")
	}
	notification, err := w.receiveNotification(ctx)
	if err != nil {
		_ = w.Close()
		return app.WatchEvent{}, err
	}
	if !sameNotificationIdentity(notification, w.expected) {
		_ = w.Close()
		return app.WatchEvent{}, fmt.Errorf("shell notification authority mismatch")
	}
	// Every qualified shell hook removes itself immediately after the one-shot
	// notifier command returns. Once a current notification has been accepted,
	// sending the cleanup script again would race fresh human input at the prompt.
	w.cleanupOnce.Do(func() {})
	w.closeTransport()
	now := time.Now().UTC()
	state := core.RequirementNotSatisfied
	if notification.Satisfied {
		state = core.RequirementSatisfied
	}
	return app.WatchEvent{
		Result:   core.RequirementResult{Requirement: w.req.Requirement, State: state, Quality: core.RequirementQualityExactShellAdapter, SafeBoundary: true, ObservedAt: now},
		Boundary: core.BoundaryProof{HandoffID: w.req.HandoffID, AuthorityEpoch: w.req.AuthorityEpoch, Shell: w.req.Shell, Quality: core.BoundaryQualityShellPrompt, ObservedAt: now},
	}, nil
}

func sameNotificationIdentity(got, want Notification) bool {
	return got.HandoffID == want.HandoffID && got.AuthorityEpoch == want.AuthorityEpoch && got.EventID == want.EventID && got.ShellRuntimeID == want.ShellRuntimeID && got.Event == want.Event
}

func (w *oneShotWatcher) receiveNotification(ctx context.Context) (Notification, error) {
	if w == nil || w.listener == nil {
		return Notification{}, fmt.Errorf("shell watcher unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Notification{}, err
	}
	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			w.closeTransport()
		case <-cancelDone:
		}
	}()
	defer close(cancelDone)
	conn, err := w.listener.Accept()
	if err != nil {
		if ctx.Err() != nil {
			return Notification{}, ctx.Err()
		}
		return Notification{}, err
	}
	defer conn.Close()
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(conn, 4097)))
	decoder.DisallowUnknownFields()
	var notification Notification
	if err := decoder.Decode(&notification); err != nil {
		return Notification{}, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Notification{}, fmt.Errorf("trailing shell notification")
	}
	if err := notification.Validate(); err != nil {
		return Notification{}, err
	}
	return notification, nil
}

func (w *oneShotWatcher) Close() error {
	if w == nil {
		return nil
	}
	w.closeTransport()
	w.cleanupOnce.Do(func() {
		if w.command != nil && w.cleanup != "" {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			w.cleanupErr = w.command.WriteShell(ctx, w.cleanup)
		}
	})
	return w.cleanupErr
}

func (w *oneShotWatcher) closeTransport() {
	w.transport.Do(func() {
		if w.listener != nil {
			_ = w.listener.Close()
		}
		if w.socketPath != "" {
			_ = os.Remove(w.socketPath)
		}
	})
}

func notifierInvocation(executable, socket string, req app.WatchRequest, eventID string, satisfied bool) string {
	return notifierInvocationForEvent(executable, socket, req, eventID, NotificationPromptBoundary, satisfied)
}

func notifierInvocationForEvent(executable, socket string, req app.WatchRequest, eventID string, event NotificationEvent, satisfied bool) string {
	args := []string{
		"/usr/bin/env", "-i", executable, "__handoff_notify",
		"--socket", socket,
		"--handoff-id", req.HandoffID,
		"--epoch", strconv.FormatUint(uint64(req.AuthorityEpoch), 10),
		"--event-id", eventID,
		"--shell-runtime-id", req.Shell.RuntimeID,
		"--event", string(event),
		"--satisfied", strconv.FormatBool(satisfied),
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ") + " >/dev/null 2>&1"
}

func newEventID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "evt_" + hex.EncodeToString(raw[:]), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func safeOpaque(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' || b == ':' {
			continue
		}
		return false
	}
	return true
}
