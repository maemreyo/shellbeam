package delegatedtmux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const privacyStateSchemaVersion = 1

type privacyState struct {
	SchemaVersion        int                 `json:"schema_version"`
	ProviderRef          string              `json:"provider_ref"`
	SessionID            string              `json:"session_id"`
	ProviderGeneration   string              `json:"provider_generation"`
	HandoffID            string              `json:"handoff_id"`
	AuthorityEpoch       core.AuthorityEpoch `json:"authority_epoch"`
	HandleRef            string              `json:"handle_ref"`
	Active               bool                `json:"active"`
	PrivateFromFirstByte bool                `json:"private_from_first_byte"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

func (s privacyState) validate() error {
	if s.SchemaVersion != privacyStateSchemaVersion || !safeOpaque(s.ProviderRef, 128) || !safeOpaque(s.SessionID, 128) || !safeOpaque(s.ProviderGeneration, 128) || !safeOpaque(s.HandoffID, 128) || !safeOpaque(s.HandleRef, 128) || !s.PrivateFromFirstByte || s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return fmt.Errorf("invalid delegated privacy state")
	}
	return s.AuthorityEpoch.Validate()
}

func (s privacyState) spec() app.PrivacySpec {
	return app.PrivacySpec{HandoffID: s.HandoffID, AuthorityEpoch: s.AuthorityEpoch}
}

func (s privacyState) handle() app.PrivacyHandle {
	return app.PrivacyHandle{OpaqueRef: s.HandleRef, Generation: s.ProviderGeneration}
}

type privacyStateStore struct{ root string }

func (s privacyStateStore) path(ref string) string {
	if !safeOpaque(ref, 128) {
		return ""
	}
	return filepath.Join(s.root, "provider-privacy", ref+".json")
}

func (s privacyStateStore) load(ref string) (privacyState, error) {
	var out privacyState
	path := s.path(ref)
	if path == "" {
		return out, fmt.Errorf("invalid privacy provider ref")
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	if err := out.validate(); err != nil || out.ProviderRef != ref {
		return out, fmt.Errorf("privacy state mismatch")
	}
	return out, nil
}

func (s privacyStateStore) save(state privacyState) error {
	if err := state.validate(); err != nil {
		return err
	}
	path := s.path(state.ProviderRef)
	if path == "" {
		return fmt.Errorf("invalid privacy provider ref")
	}
	dir := filepath.Dir(path)
	if err := ensurePrivateDirectory(dir); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".privacy-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (s privacyStateStore) remove(ref string) error {
	path := s.path(ref)
	if path == "" {
		return fmt.Errorf("invalid privacy provider ref")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (p *Provider) InspectPrivacy(ctx context.Context, ref core.ProviderRef) (app.PrivacyObservation, error) {
	state, err := p.privacyProviderState(ctx, ref)
	if err != nil {
		return app.PrivacyObservation{}, err
	}
	observation := app.PrivacyObservation{ProviderGeneration: state.ProviderGeneration, ObservedAt: time.Now().UTC()}
	privacy, err := p.privacy.load(ref.Ref)
	if errors.Is(err, os.ErrNotExist) {
		return observation, observation.Validate()
	}
	if err != nil {
		return app.PrivacyObservation{}, privacyBarrierFailure("", "privacy_state_invalid", err)
	}
	if privacy.ProviderGeneration != state.ProviderGeneration || privacy.SessionID != ref.SessionID || privacy.ProviderRef != ref.Ref {
		return app.PrivacyObservation{}, privacyBarrierFailure(privacy.HandoffID, "privacy_generation_mismatch", nil)
	}
	observation.Active = privacy.Active
	observation.ReleasePending = privacy.Active
	return observation, observation.Validate()
}

func (p *Provider) ArmPrivateObservation(ctx context.Context, ref core.ProviderRef, spec app.PrivacySpec) (app.PrivacyHandle, error) {
	if err := spec.Validate(); err != nil {
		return app.PrivacyHandle{}, failure.New(failure.InvalidInput, map[string]string{"field": "privacy_spec"}, err)
	}
	state, err := p.privacyProviderState(ctx, ref)
	if err != nil {
		return app.PrivacyHandle{}, err
	}
	if existing, err := p.privacy.load(ref.Ref); err == nil {
		if existing.ProviderGeneration != state.ProviderGeneration {
			return app.PrivacyHandle{}, privacyBarrierFailure(spec.HandoffID, "active_binding_conflict", nil)
		}
		if !existing.Active {
			if existing.HandoffID == spec.HandoffID {
				return app.PrivacyHandle{}, privacyBarrierFailure(spec.HandoffID, "privacy_already_released", nil)
			}
			if spec.AuthorityEpoch <= existing.AuthorityEpoch {
				return app.PrivacyHandle{}, privacyBarrierFailure(spec.HandoffID, "stale_privacy_epoch", nil)
			}
			handle := newPrivacyHandle(ref, state.ProviderGeneration, spec)
			if err := p.armPrivateObserver(ctx, ref, state, spec, handle); err != nil {
				return app.PrivacyHandle{}, err
			}
			return handle, nil
		}
		if existing.HandoffID != spec.HandoffID {
			return app.PrivacyHandle{}, privacyBarrierFailure(spec.HandoffID, "active_binding_conflict", nil)
		}
		if err := p.ensureCurrentObserverPrivate(ctx, ref, state); err != nil {
			return app.PrivacyHandle{}, err
		}
		if spec.AuthorityEpoch < existing.AuthorityEpoch {
			return app.PrivacyHandle{}, privacyBarrierFailure(spec.HandoffID, "stale_privacy_epoch", nil)
		}
		if spec.AuthorityEpoch == existing.AuthorityEpoch {
			return existing.handle(), nil
		}
		handle := newPrivacyHandle(ref, state.ProviderGeneration, spec)
		existing.AuthorityEpoch = spec.AuthorityEpoch
		existing.HandleRef = handle.OpaqueRef
		existing.UpdatedAt = time.Now().UTC()
		if err := p.privacy.save(existing); err != nil {
			return app.PrivacyHandle{}, privacyBarrierFailure(spec.HandoffID, "privacy_state_write", err)
		}
		return handle, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return app.PrivacyHandle{}, privacyBarrierFailure(spec.HandoffID, "privacy_state_invalid", err)
	}

	handle := newPrivacyHandle(ref, state.ProviderGeneration, spec)
	if err := p.armPrivateObserver(ctx, ref, state, spec, handle); err != nil {
		return app.PrivacyHandle{}, err
	}
	return handle, nil
}

func (p *Provider) ProvePrivateObservation(ctx context.Context, ref core.ProviderRef, handle app.PrivacyHandle) (app.PrivateObservationProof, error) {
	if err := handle.Validate(); err != nil {
		return app.PrivateObservationProof{}, failure.New(failure.InvalidInput, map[string]string{"field": "privacy_handle"}, err)
	}
	state, err := p.privacyProviderState(ctx, ref)
	if err != nil {
		return app.PrivateObservationProof{}, err
	}
	privacy, err := p.privacy.load(ref.Ref)
	if err != nil || !privacy.Active || privacy.handle() != handle || privacy.ProviderGeneration != state.ProviderGeneration {
		return app.PrivateObservationProof{}, privacyBarrierFailure(privacy.HandoffID, "private_proof_unavailable", err)
	}
	p.mu.Lock()
	control := p.controls[ref.Ref]
	p.mu.Unlock()
	if control == nil || !control.isPrivateObservation() {
		return app.PrivateObservationProof{}, privacyBarrierFailure(privacy.HandoffID, "observer_not_private", nil)
	}
	facts, err := p.queryFacts(ctx, control, state.TmuxSession)
	if err != nil {
		return app.PrivateObservationProof{}, privacyBarrierFailure(privacy.HandoffID, "observer_proof", err)
	}
	if err := p.verifyFacts(ctx, control, state, facts); err != nil {
		return app.PrivateObservationProof{}, err
	}
	proof := app.PrivateObservationProof{Handle: handle, ProviderGeneration: state.ProviderGeneration, PrivateFromFirstByte: true, ObservedAt: time.Now().UTC()}
	return proof, proof.Validate()
}

func (p *Provider) ReleasePrivateObservation(ctx context.Context, ref core.ProviderRef, handle app.PrivacyHandle, boundary app.ForwardBoundary) error {
	if err := handle.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "privacy_handle"}, err)
	}
	state, err := p.privacyProviderState(ctx, ref)
	if err != nil {
		return err
	}
	privacy, err := p.privacy.load(ref.Ref)
	if err != nil || privacy.handle() != handle || privacy.ProviderGeneration != state.ProviderGeneration {
		return privacyReleaseFailure("privacy_binding_missing", err)
	}
	if err := boundary.ValidateFor(privacy.spec()); err != nil {
		return privacyReleaseFailure("boundary_unproven", err)
	}
	if !privacy.Active {
		return nil
	}
	p.mu.Lock()
	control := p.controls[ref.Ref]
	p.mu.Unlock()
	if control == nil || !control.isPrivateObservation() {
		return privacyReleaseFailure("observer_not_private", nil)
	}
	facts, err := p.queryFacts(ctx, control, state.TmuxSession)
	if err != nil {
		return privacyReleaseFailure("observer_proof", err)
	}
	if err := p.verifyFacts(ctx, control, state, facts); err != nil {
		return err
	}
	if err := p.controlCommand(ctx, control, "refresh-client", "-f", "!no-output"); err != nil {
		return privacyReleaseFailure("release_command", err)
	}
	control.setPrivateObservation(false)
	privacy.Active = false
	privacy.UpdatedAt = time.Now().UTC()
	if err := p.privacy.save(privacy); err != nil {
		return privacyReleaseFailure("privacy_state_write", err)
	}
	return nil
}

func (p *Provider) privacyProviderState(ctx context.Context, ref core.ProviderRef) (privateState, error) {
	state, err := p.humanProviderState(ctx, ref)
	if err != nil {
		return privateState{}, err
	}
	return state, nil
}

func (p *Provider) armPrivateObserver(ctx context.Context, ref core.ProviderRef, state privateState, spec app.PrivacySpec, handle app.PrivacyHandle) error {
	p.mu.Lock()
	old := p.controls[ref.Ref]
	p.mu.Unlock()
	if old == nil {
		return privacyBarrierFailure(spec.HandoffID, "observer_missing", nil)
	}
	pane, sink := old.targetSnapshot()
	if pane != state.PaneID || sink == nil {
		return privacyBarrierFailure(spec.HandoffID, "observer_target_missing", nil)
	}
	privateControl, err := p.startPrivateControl(ctx, state.SocketPath, state.TmuxSession)
	if err != nil {
		return privacyBarrierFailure(spec.HandoffID, "private_attach", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = privateControl.close()
		}
	}()
	facts, err := p.queryFacts(ctx, privateControl, state.TmuxSession)
	if err != nil {
		return privacyBarrierFailure(spec.HandoffID, "private_attach_proof", err)
	}
	if err := p.verifyFacts(ctx, privateControl, state, facts); err != nil {
		return err
	}
	privateControl.shareOutputCounter(old)
	if err := privateControl.setTarget(state.PaneID, sink); err != nil {
		return privacyBarrierFailure(spec.HandoffID, "private_target", err)
	}
	p.mu.Lock()
	if p.controls[ref.Ref] != old {
		p.mu.Unlock()
		return privacyBarrierFailure(spec.HandoffID, "observer_changed", nil)
	}
	p.controls[ref.Ref] = privateControl
	p.mu.Unlock()
	keep = true
	_ = old.close()
	now := time.Now().UTC()
	record := privacyState{SchemaVersion: privacyStateSchemaVersion, ProviderRef: ref.Ref, SessionID: ref.SessionID, ProviderGeneration: state.ProviderGeneration, HandoffID: spec.HandoffID, AuthorityEpoch: spec.AuthorityEpoch, HandleRef: handle.OpaqueRef, Active: true, PrivateFromFirstByte: true, CreatedAt: now, UpdatedAt: now}
	if err := p.privacy.save(record); err != nil {
		return privacyBarrierFailure(spec.HandoffID, "privacy_state_write", err)
	}
	return nil
}

func newPrivacyHandle(ref core.ProviderRef, generation string, spec app.PrivacySpec) app.PrivacyHandle {
	logical := ref.Ref + "\x00" + generation + "\x00" + spec.HandoffID + "\x00" + fmt.Sprint(spec.AuthorityEpoch)
	sum := sha256.Sum256([]byte(logical))
	return app.PrivacyHandle{OpaqueRef: "privacy_" + hex.EncodeToString(sum[:16]), Generation: generation}
}

func privacyBarrierFailure(handoffID, reason string, cause error) error {
	details := map[string]string{"reason": reason}
	if safeOpaque(handoffID, 128) {
		details["handoff_id"] = handoffID
	}
	return failure.New(failure.PrivateOutputBarrierFailed, details, cause)
}

func privacyReleaseFailure(reason string, cause error) error {
	return failure.New(failure.PrivacyReleaseUnproven, map[string]string{"reason": reason}, cause)
}
