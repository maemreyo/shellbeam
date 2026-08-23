//go:build darwin

package integration_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/creack/pty"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestInteractiveHandoffSecretNativeCanaryIsHumanVisibleAndModelPrivate(t *testing.T) {
	m := newH4SecretManualNative(t)
	started := startH4CanarySession(t, m)
	canary := h4RuntimeCanary(t)
	variants := h4CanaryVariants(canary)

	public, human := enterH4SecretHumanOwnership(t, m, started.SessionID, "handoff-h4-secret-canary")
	if public.PrivacyState != handoff.PrivacyPrivate || public.PrivacyRelease != handoff.PrivacyReleasePending || public.CaptureState != handoff.CapturePrivate {
		t.Fatalf("public privacy=%#v", public)
	}
	truth, err := m.store.LoadDelegatedCaptureTruth(t.Context(), operation.SessionID(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if truth.OutputComplete || truth.Quality != receipt.CapturePartial || len(truth.Reasons) != 1 || truth.Reasons[0] != receipt.CaptureReasonPrivateIntervalsOmitted {
		t.Fatalf("capture truth=%#v", truth)
	}

	if _, err := human.master.Write([]byte(canary + "\n")); err != nil {
		t.Fatal(err)
	}
	humanView := h4ReadPTYUntil(t, human.master, "H4_PRIVATE:"+canary)
	if !strings.Contains(humanView, canary) {
		t.Fatalf("human terminal did not observe canary: %q", humanView)
	}

	poll, err := m.svc.Poll(t.Context(), daemonapp.PollRequest{SessionID: started.SessionID, MaxOutputBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	inspect, err := m.svc.InspectHandoffPublic(t.Context(), human.state.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	h4AssertNoCanaryVariants(t, "public daemon surfaces", h4JSON(t, []any{public, inspect, poll}), variants)
	h4AssertTreeNoCanaryVariants(t, m.stateDir, variants)

	ready, err := m.svc.HandoffHumanControl(t.Context(), handoff.ControlSignal{
		HandoffID: human.state.HandoffID, AuthorityEpoch: human.state.AuthorityEpoch,
		ControlID: "h4-secret-manual-ready", Kind: handoff.HumanControlReady,
	})
	if err != nil || ready.Outcome != "ready" || ready.State.Phase != handoff.PhaseAgentOwned || ready.State.PrivacyRelease != handoff.PrivacyReleasePending || ready.State.CaptureState != handoff.CapturePrivate {
		t.Fatalf("manual ready=%#v err=%v", ready, err)
	}
	finish := "finish\n"
	if _, err := m.svc.Write(t.Context(), daemonapp.WriteRequest{
		SessionID: started.SessionID, AuthorityEpoch: ready.State.AuthorityEpoch, InputOffset: 0, Chars: finish,
	}); err != nil {
		t.Fatal(err)
	}
	terminal := waitH1Terminal(t, m.svc, started.SessionID)
	if terminal.Receipt == nil {
		t.Fatal("terminal receipt missing")
	}
	if terminal.Receipt.OutputComplete || terminal.Receipt.CaptureQuality != receipt.CapturePartial || len(terminal.Receipt.CaptureReasons) != 1 || terminal.Receipt.CaptureReasons[0] != receipt.CaptureReasonPrivateIntervalsOmitted {
		t.Fatalf("terminal capture receipt=%#v", terminal.Receipt)
	}
	if terminal.Receipt.EvidenceAuthority != receipt.EvidenceAuthoritySessionLifecycleOnly {
		t.Fatalf("evidence authority=%q", terminal.Receipt.EvidenceAuthority)
	}
	result, err := terminal.StructuredResult()
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.OutputComplete || result.Output.CaptureQuality != receipt.CapturePartial || len(result.Output.CaptureReasons) != 1 || result.Output.CaptureReasons[0] != receipt.CaptureReasonPrivateIntervalsOmitted || result.EvidenceAuthority != receipt.EvidenceAuthoritySessionLifecycleOnly {
		t.Fatalf("structured result=%#v", result)
	}
	h4AssertNoCanaryVariants(t, "terminal receipt/result", h4JSON(t, []any{terminal, result}), variants)
	h4AssertTreeNoCanaryVariants(t, m.stateDir, variants)
}

func TestInteractiveHandoffSecretAbortThenTerminatePreservesPrivateReasonWithProviderLoss(t *testing.T) {
	m := newH4SecretManualNative(t)
	started := startH2ManualSession(t, m)
	_, human := enterH4SecretHumanOwnership(t, m, started.SessionID, "handoff-h4-secret-abort-provider-loss")

	aborted, err := m.svc.HandoffHumanControl(t.Context(), handoff.ControlSignal{
		HandoffID: human.state.HandoffID, AuthorityEpoch: human.state.AuthorityEpoch,
		ControlID: "h4-secret-abort-provider-loss", Kind: handoff.HumanControlAbort,
	})
	if err != nil || aborted.Outcome != "aborted" || aborted.State.HumanIngress != handoff.IngressFenced || aborted.State.PrivacyState != handoff.PrivacyPrivate || aborted.State.CaptureState != handoff.CapturePrivate {
		t.Fatalf("abort=%#v err=%v", aborted, err)
	}
	terminated, err := m.svc.HandoffHumanControl(t.Context(), handoff.ControlSignal{
		HandoffID: aborted.State.HandoffID, AuthorityEpoch: aborted.State.AuthorityEpoch,
		ControlID: "h4-secret-terminate-provider-loss", Kind: handoff.HumanControlTerminate,
	})
	if err != nil || terminated.Outcome != "terminated" {
		t.Fatalf("terminate=%#v err=%v", terminated, err)
	}
	terminal := waitH1Terminal(t, m.svc, started.SessionID)
	if terminal.Receipt == nil {
		t.Fatal("terminal receipt missing")
	}
	want := []receipt.CaptureReason{receipt.CaptureReasonPrivateIntervalsOmitted, receipt.CaptureReasonProviderLost}
	if terminal.Receipt.OutputComplete || terminal.Receipt.CaptureQuality != receipt.CaptureIncomplete || len(terminal.Receipt.CaptureReasons) != len(want) {
		t.Fatalf("terminal capture=%#v", terminal.Receipt)
	}
	for i := range want {
		if terminal.Receipt.CaptureReasons[i] != want[i] {
			t.Fatalf("capture reasons=%v want=%v", terminal.Receipt.CaptureReasons, want)
		}
	}
	if terminal.Receipt.EvidenceAuthority != receipt.EvidenceAuthoritySessionLifecycleOnly {
		t.Fatalf("evidence authority=%q", terminal.Receipt.EvidenceAuthority)
	}
}

func startH4CanarySession(t *testing.T, m *h2ManualNative) daemonapp.View {
	t.Helper()
	command := `stty -echo; printf 'H4_READY\n'; IFS= read -r human; printf 'H4_PRIVATE:%s\n' "$human"; IFS= read -r agent; printf 'H4_AGENT:%s\n' "$agent"; exit 0`
	started, err := m.svc.Start(t.Context(), daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "h4-task9-secret-canary-session", CWD: "/tmp", Command: command,
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream, TimeoutMode: operation.TimeoutModeUnlimited,
		YieldMS: 25, MaxOutputBytes: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitH1Output(t, m.svc, started.SessionID, "H4_READY")
	ref, err := m.store.LoadDelegatedProviderRef(t.Context(), operation.SessionID(started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.provider.Close(t.Context(), ref) })
	return started
}

func newH4SecretManualNative(t *testing.T) *h2ManualNative {
	t.Helper()
	m := newH2ManualNative(t)
	catalog := h1DelegatedCatalog().WithInteractiveHandoff(h4SecretSupport())
	m.svc = daemonapp.NewService(m.store, &h1ImmediateOwner{}, daemonapp.Options{
		Incarnation: "h4-secret-integration", Shell: "/bin/sh", MaxQueuedInputBytes: 1 << 16,
		DelegatedRuntime: m.provider, Capabilities: catalog,
	})
	return m
}

func h4SecretSupport() capability.InteractiveHandoffSupport {
	return capability.InteractiveHandoffSupport{
		ManualStandard: true,
		Secret:         true,
		Privacy: &capability.HandoffPrivacySupport{
			SecretPrivateInterval: true, PrivacyReleaseSeparate: true,
			ObserverTopologyQualified: true, HumanInputPersisted: false,
		},
		CaptureQualities: []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete},
	}
}

func enterH4SecretHumanOwnership(t *testing.T, m *h2ManualNative, sessionID, handoffID string) (handoff.PublicState, h2HumanAttach) {
	t.Helper()
	req := handoff.Request{
		HandoffID: handoffID, SessionID: sessionID, Reason: handoff.ReasonCredentialRequired,
		Privacy: handoff.PrivacySecret, Completion: handoff.Completion{Kind: handoff.CompletionManualReady},
	}
	public, err := m.svc.RequestHandoffPublic(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := m.svc.BootstrapLocalHuman(t.Context(), handoffID)
	if err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		_ = master.Close()
		_ = slave.Close()
		t.Fatal(err)
	}
	attach, err := m.provider.AttachHuman(t.Context(), boot.ProviderRef, delegatedapp.HumanAttachSpec{
		Stdin: slave, Stdout: slave, Stderr: slave,
		Environment: append(os.Environ(), "SHELLBEAM_H4_ATTACH_SENTINEL=must_not_replace_session"),
	})
	_ = slave.Close()
	if err != nil {
		_ = master.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	owned, err := m.svc.BindLocalHuman(t.Context(), handoffID, attach.ClientRef)
	if err != nil {
		t.Fatal(err)
	}
	if owned.Phase != handoff.PhaseHumanOwned || owned.ProviderOwner != delegated.OwnerHuman || owned.HumanIngress != handoff.IngressWritable || owned.PrivacyState != handoff.PrivacyPrivate || owned.CaptureState != handoff.CapturePrivate {
		t.Fatalf("owned=%#v", owned)
	}
	return public, h2HumanAttach{master: master, client: attach.ClientRef, state: owned, ref: boot.ProviderRef}
}

func h4RuntimeCanary(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return "SHELLBEAM_H4_SECRET_" + hex.EncodeToString(raw[:])
}

func h4CanaryVariants(canary string) [][]byte {
	raw := []byte(canary)
	digest := sha256.Sum256(raw)
	return [][]byte{
		raw,
		[]byte(base64.StdEncoding.EncodeToString(raw)),
		[]byte(hex.EncodeToString(raw)),
		[]byte(hex.EncodeToString(digest[:])),
	}
}

func h4ReadPTYUntil(t *testing.T, f *os.File, needle string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var out bytes.Buffer
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
		nready, err := unix.Poll(fds, 100)
		if err != nil {
			t.Fatal(err)
		}
		if nready == 0 || fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, err := f.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if strings.Contains(out.String(), needle) {
				return out.String()
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("PTY did not contain %q: %q", needle, out.String())
	return ""
}

func h4JSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func h4AssertNoCanaryVariants(t *testing.T, surface string, raw []byte, variants [][]byte) {
	t.Helper()
	for _, variant := range variants {
		if len(variant) > 0 && bytes.Contains(raw, variant) {
			t.Fatalf("%s leaked secret-derived variant %q", surface, variant)
		}
	}
}

func h4AssertTreeNoCanaryVariants(t *testing.T, root string, variants [][]byte) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		h4AssertNoCanaryVariants(t, "state file "+path, raw, variants)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
