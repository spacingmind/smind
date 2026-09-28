package runs

import (
	"context"
	"fmt"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// SetPermissionMode switches runID's live session to mode, one of its
// provider's own mode ids (ADR-0019), via the provider's own mechanism
// (taskrunner.Runner.SetPermissionMode) -- Claude Code's
// set_permission_mode, an ACP agent's session/set_mode. Codex fails with an
// error wrapping taskrunner.ErrModeSwitchNotSupported. The mode is
// validated against the provider's catalog first, and recorded on the run
// only once the provider accepted it.
//
// A finished run has no session left to switch, so this fails on one; an
// unknown runID returns ErrNotFound (via reg.get), same as every other
// Registry lookup.
func (reg *Registry) SetPermissionMode(ctx context.Context, runID, mode string) error {
	r, err := reg.get(runID)
	if err != nil {
		return err
	}
	if r.runner == nil {
		return fmt.Errorf("runs: set permission mode: run %q has already finished", runID)
	}
	catalog, ok := r.runner.ProviderCatalogFor(r.provider)
	if !ok {
		return fmt.Errorf("runs: set permission mode: unknown provider %q", r.provider)
	}
	if mode == "" {
		return fmt.Errorf("runs: set permission mode: mode is required")
	}
	if err := taskrunner.ValidatePermissionSettings(catalog, taskrunner.PermissionSettings{Mode: mode}); err != nil {
		return fmt.Errorf("runs: set permission mode: %w", err)
	}
	r.modeSwitchMu.Lock()
	defer r.modeSwitchMu.Unlock()
	if err := r.requireRunning("set permission mode"); err != nil {
		return err
	}
	if err := r.runner.SetPermissionMode(ctx, r.chatID, r.provider, mode); err != nil {
		return fmt.Errorf("runs: set permission mode: %w", err)
	}

	r.mu.Lock()
	r.perm.Mode = mode
	r.mu.Unlock()
	if err := reg.persistPermission(r); err != nil {
		// The provider already switched, so the in-memory mode stays at
		// the new value (RunStatus must not lie about the live session);
		// only the durable row is stale.
		return fmt.Errorf("runs: set permission mode: %w", err)
	}
	reg.notifyRunStatus(r)
	return nil
}

// SetAutoAccept flips runID's AutoAccept (ACP providers only), taking
// effect for the next permission request runPermissionDecider.Decide
// evaluates -- a request already pending a human is not retroactively
// resolved.
func (reg *Registry) SetAutoAccept(runID string, autoAccept bool) error {
	r, err := reg.get(runID)
	if err != nil {
		return err
	}
	info, ok := taskrunner.ProviderInfoFor(r.provider)
	if !ok || (autoAccept && !info.SupportsAutoAccept) {
		return fmt.Errorf("runs: set auto-accept: autoAccept is not supported for provider %q", r.provider)
	}
	r.mu.Lock()
	if r.status != StatusRunning {
		r.mu.Unlock()
		return fmt.Errorf("runs: set auto-accept: run %q has already finished", runID)
	}
	r.perm.AutoAccept = autoAccept
	r.mu.Unlock()
	if err := reg.persistPermission(r); err != nil {
		return fmt.Errorf("runs: set auto-accept: %w", err)
	}
	reg.notifyRunStatus(r)
	return nil
}

// persistPermission writes r's current permission settings to its store
// row, so a mid-run switch (SetPermissionMode, SetAutoAccept, or the
// agent-reported mode applyReportedMode records) survives a daemon
// restart: the rehydrated run shows the mode it ended in, not the one it
// started in.
func (reg *Registry) persistPermission(r *run) error {
	perm := r.getPerm()
	if err := reg.st.UpdateRunPermission(r.id, perm.Mode, perm.AutoAccept); err != nil {
		return fmt.Errorf("persist run %q permission: %w", r.id, err)
	}
	return nil
}

// requireRunning fails unless r is still StatusRunning.
func (r *run) requireRunning(op string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != StatusRunning {
		return fmt.Errorf("runs: %s: run %q has already finished", op, r.id)
	}
	return nil
}

// applyReportedMode records the permission mode the provider reports the
// run's session is actually in (taskrunner.EventTypePermissionModeChanged:
// an ACP run's start mode once applied, or the agent's own later switch),
// so RunStatus never shows a mode the agent isn't in. The reported mode is
// persisted too (best-effort, like record: this runs on drive's forwarding
// goroutine with no caller to return an error to) so the stored row keeps
// up with the agent's own switches, not just smind-initiated ones.
func (reg *Registry) applyReportedMode(r *run, mode string) {
	r.mu.Lock()
	changed := r.perm.Mode != mode
	r.perm.Mode = mode
	r.mu.Unlock()
	if changed {
		_ = reg.persistPermission(r)
		reg.notifyRunStatus(r)
	}
}
