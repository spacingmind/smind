package wsapi

import (
	"context"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// pluginHooks adapts the plugin manager to the run engine's two seams: the
// before-start hook (runs.LaunchHooks) and the run.finished observer
// (runs.Observer). The run engine never imports the plugin package.
type pluginHooks struct{ m *plugins.Manager }

func (h pluginHooks) BeforeStart(ctx context.Context, in runs.HookStart) (runs.HookEdits, error) {
	res, err := h.m.RunBeforeStart(ctx, plugins.BeforeStartInput{
		RunID: in.RunID, TaskID: in.TaskID, ChatID: in.ChatID, WorkspaceID: in.WorkspaceID,
		Provider: in.Provider, Model: in.Model, PermissionMode: in.PermissionMode, Origin: in.Origin,
	})
	edits := runs.HookEdits{Suffix: res.Suffix, Env: res.Env, Mcp: res.Mcp, Notes: res.Notes}
	for _, f := range res.Failures {
		edits.Notes = append(edits.Notes, "plugin hook failed: "+f.Error())
	}
	return edits, err
}

// OnRunFinished delivers run.finished for ordinary runs; system runs
// (learning, probes) are the daemon's own business.
func (h pluginHooks) OnRunFinished(f runs.RunFinished) {
	if f.Purpose != store.RunPurposeUser {
		return
	}
	h.m.NotifyRunFinished(plugins.RunFinishedInput{
		RunID: f.RunID, TaskID: f.TaskID, ChatID: f.ChatID, Provider: f.Provider, Model: f.Model, Origin: f.Origin,
		Status: f.Status, StopReason: f.StopReason, FailureKind: f.FailureKind, Action: f.FailureAction, ToolCalls: f.ToolCalls,
	})
}
