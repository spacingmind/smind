package runs

import (
	"context"
	"fmt"
	"sync"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// LaunchHooks lets plugins (ADR-0042 PB.3) shape a run just before its agent
// starts. runs depends on this interface only, so the plugin package stays out
// of the run engine; the daemon adapts the plugin manager to it.
type LaunchHooks interface {
	// BeforeStart returns the validated edits of every hook. A non-nil error
	// stops the run before anything is spawned (a `closed` hook failed).
	BeforeStart(ctx context.Context, in HookStart) (HookEdits, error)
}

// HookStart describes the run being started: ids and resolved settings only.
type HookStart struct {
	RunID          string
	TaskID         int64
	ChatID         int64
	WorkspaceID    int64
	Provider       string
	Model          string
	PermissionMode string
	Origin         string
}

// HookEdits is what the hooks may change, already validated by the host.
type HookEdits struct {
	// Suffix is appended to the prompt the agent receives (the stored run
	// prompt stays the sender's text).
	Suffix string
	// Env is added to the agent's environment, never over an existing key.
	Env map[string]string
	// Mcp is added to the run's MCP servers (never over an existing name).
	Mcp []store.McpServer
	// Notes are timeline lines saying what plugins contributed or why one
	// failed; they carry no instruction text.
	Notes []string
}

type launchHooksSlot struct {
	mu sync.Mutex
	h  LaunchHooks
}

// SetLaunchHooks installs the plugin hooks; nil removes them.
func (reg *Registry) SetLaunchHooks(h LaunchHooks) {
	reg.launchHooksSlot.mu.Lock()
	reg.launchHooksSlot.h = h
	reg.launchHooksSlot.mu.Unlock()
}

func (reg *Registry) launchHooks() LaunchHooks {
	reg.launchHooksSlot.mu.Lock()
	defer reg.launchHooksSlot.mu.Unlock()
	return reg.launchHooksSlot.h
}

// applyLaunchHooks runs the before-start hooks for an ordinary run (system
// runs, probes and isolated runs never get plugin edits). It returns the
// prompt and context to launch with, or an error that stops the run.
func (reg *Registry) applyLaunchHooks(ctx context.Context, r *run, prompt string) (context.Context, string, error) {
	h := reg.launchHooks()
	if h == nil || r.purpose != "" {
		return ctx, prompt, nil
	}
	in := HookStart{RunID: r.id, TaskID: r.taskID, ChatID: r.chatID, Provider: string(r.provider), Model: r.model, PermissionMode: r.getPerm().Mode, Origin: r.origin}
	if t, err := reg.st.GetTask(r.taskID); err == nil {
		in.WorkspaceID = t.WorkspaceID
	}
	edits, err := h.BeforeStart(ctx, in)
	for _, n := range edits.Notes {
		reg.record(r, taskrunner.Event{Type: taskrunner.EventTypeSessionNote, Text: n})
	}
	if err != nil {
		return ctx, prompt, fmt.Errorf("a plugin hook stopped the run: %w", err)
	}
	if len(edits.Env) > 0 || len(edits.Mcp) > 0 {
		ctx = taskrunner.WithLaunchEdits(ctx, taskrunner.LaunchEdits{Env: edits.Env, Mcp: edits.Mcp})
	}
	return ctx, prompt + edits.Suffix, nil
}
