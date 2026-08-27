package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// methodHandlers returns the full set of RPC methods this package serves,
// bound to wm and runner.
func methodHandlers(wm *workspace.Manager, runner *taskrunner.Runner) map[string]handlerFunc {
	return map[string]handlerFunc{
		"workspace.create": handleWorkspaceCreate(wm),
		"workspace.list":   handleWorkspaceList(wm),
		"workspace.get":    handleWorkspaceGet(wm),
		"space.create":     handleSpaceCreate(wm),
		"space.list":       handleSpaceList(wm),
		"space.get":        handleSpaceGet(wm),
		"task.create":      handleTaskCreate(wm),
		"task.list":        handleTaskList(wm),
		"task.get":         handleTaskGet(wm),
		"task.archive":     handleTaskArchive(wm),
		"task.prompt":      handleTaskPrompt(runner),
	}
}

func handleWorkspaceCreate(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			Path          string  `json:"path"`
			Title         string  `json:"title"`
			RoutingPolicy string  `json:"routingPolicy"`
			AccountIDs    []int64 `json:"accountIds"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.create: invalid params: %w", err)
		}
		return wm.CreateWorkspace(p.Path, p.Title, p.RoutingPolicy, p.AccountIDs)
	}
}

func handleWorkspaceList(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		return wm.ListWorkspaces()
	}
}

func handleWorkspaceGet(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("workspace.get: invalid params: %w", err)
		}
		return wm.GetWorkspace(p.ID)
	}
}

func handleSpaceCreate(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64  `json:"workspaceId"`
			Title       string `json:"title"`
			EnvData     string `json:"envData"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("space.create: invalid params: %w", err)
		}
		return wm.CreateSpace(p.WorkspaceID, p.Title, p.EnvData)
	}
}

func handleSpaceList(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("space.list: invalid params: %w", err)
		}
		return wm.ListSpaces(p.WorkspaceID)
	}
}

func handleSpaceGet(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("space.get: invalid params: %w", err)
		}
		return wm.GetSpace(p.ID)
	}
}

func handleTaskCreate(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64  `json:"workspaceId"`
			SpaceID     *int64 `json:"spaceId"`
			Title       string `json:"title"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.create: invalid params: %w", err)
		}
		return wm.CreateTask(p.WorkspaceID, p.SpaceID, p.Title)
	}
}

func handleTaskList(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.list: invalid params: %w", err)
		}
		return wm.ListTasks(p.WorkspaceID)
	}
}

func handleTaskGet(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.get: invalid params: %w", err)
		}
		return wm.GetTask(p.ID)
	}
}

func handleTaskArchive(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.archive: invalid params: %w", err)
		}
		return wm.ArchiveTask(p.ID)
	}
}

// taskPromptResult is the terminal result of a successful task.prompt.
type taskPromptResult struct {
	StopReason string `json:"stopReason"`
}

// taskChunkParams is the params payload of every "chunk" event task.prompt
// emits.
type taskChunkParams struct {
	Text string `json:"text"`
}

func handleTaskPrompt(runner *taskrunner.Runner) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID   int64               `json:"taskId"`
			Provider taskrunner.Provider `json:"provider"`
			Prompt   string              `json:"prompt"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("task.prompt: invalid params: %w", err)
		}

		events := make(chan taskrunner.Event)
		var stopReason string
		forwardDone := make(chan struct{})
		go func() {
			defer close(forwardDone)
			for e := range events {
				switch e.Type {
				case taskrunner.EventTypeText:
					rc.Emit("chunk", taskChunkParams{Text: e.Text})
				case taskrunner.EventTypeDone:
					stopReason = e.StopReason
				}
			}
		}()

		err := runner.RunPrompt(ctx, p.TaskID, p.Provider, p.Prompt, events)
		<-forwardDone
		if err != nil {
			return nil, fmt.Errorf("task.prompt: %w", err)
		}
		return taskPromptResult{StopReason: stopReason}, nil
	}
}
