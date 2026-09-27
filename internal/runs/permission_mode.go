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
	if err := r.requireRunning("set permission mode"); err != nil {
		return err
	}
	if err := r.runner.SetPermissionMode(ctx, r.chatID, r.provider, mode); err != nil {
		return fmt.Errorf("runs: set permission mode: %w", err)
	}

	r.mu.Lock()
	r.perm.Mode = mode
	r.mu.Unlock()
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
	reg.notifyRunStatus(r)
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
