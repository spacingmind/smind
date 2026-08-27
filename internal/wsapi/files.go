package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/files"
	"github.com/spacingmind/smind/internal/workspace"
)

// taskWorktreePath looks up taskID's real worktree path via wm, erroring
// clearly if the task doesn't exist or has no worktree (e.g. it was
// archived, which removes the worktree -- see workspace.Manager.ArchiveTask)
// rather than letting a nil WorktreePath reach files.List/Read/Write as an
// empty-string root.
func taskWorktreePath(wm *workspace.Manager, taskID int64) (string, error) {
	t, err := wm.GetTask(taskID)
	if err != nil {
		return "", err
	}
	if t.WorktreePath == nil {
		return "", fmt.Errorf("task %d has no worktree", taskID)
	}
	return *t.WorktreePath, nil
}

// fileReadResult is the terminal result of a successful file.read.
type fileReadResult struct {
	Content string `json:"content"`
}

// handleFileList serves file.list: the entries of {taskId, path} (path
// defaults to the task's worktree root) *within that task's worktree*.
// Path sandboxing (rejecting any path that would resolve outside the
// worktree) is enforced by internal/files, not here -- see files.List's
// doc comment.
func handleFileList(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64  `json:"taskId"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("file.list: invalid params: %w", err)
		}
		root, err := taskWorktreePath(wm, p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("file.list: %w", err)
		}
		entries, err := files.List(root, p.Path)
		if err != nil {
			return nil, fmt.Errorf("file.list: %w", err)
		}
		return entries, nil
	}
}

// handleFileRead serves file.read: {taskId, path}'s content as a UTF-8
// string, sandboxed the same way as file.list.
func handleFileRead(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64  `json:"taskId"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("file.read: invalid params: %w", err)
		}
		root, err := taskWorktreePath(wm, p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("file.read: %w", err)
		}
		content, err := files.Read(root, p.Path)
		if err != nil {
			return nil, fmt.Errorf("file.read: %w", err)
		}
		return fileReadResult{Content: content}, nil
	}
}

// handleFileWrite serves file.write: writes {taskId, path, content} inside
// the task's worktree, creating the file if it doesn't exist, sandboxed the
// same way as file.list/file.read.
func handleFileWrite(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID  int64  `json:"taskId"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("file.write: invalid params: %w", err)
		}
		root, err := taskWorktreePath(wm, p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("file.write: %w", err)
		}
		if err := files.Write(root, p.Path, p.Content); err != nil {
			return nil, fmt.Errorf("file.write: %w", err)
		}
		return struct{}{}, nil
	}
}
