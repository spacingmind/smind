package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// fileReadResult is the terminal result of a successful file.read: the
// content plus the read-time mtime, which a conditional file.write
// (expectedMtime) echoes back to detect drift -- see the
// file-conflict-detection plan.
type fileReadResult struct {
	Content string   `json:"content"`
	Mtime   DateTime `json:"mtime"`
}

// fileWriteResult is the terminal result of a successful file.write: the
// written file's current mtime, so a conditional writer can chain it into
// its next expectedMtime without a re-read.
type fileWriteResult struct {
	Mtime DateTime `json:"mtime"`
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
// string plus its mtime, sandboxed the same way as file.list.
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
		content, mtime, err := files.Read(root, p.Path)
		if err != nil {
			return nil, fmt.Errorf("file.read: %w", err)
		}
		return fileReadResult{Content: content, Mtime: DateTime(mtime)}, nil
	}
}

// handleFileWrite serves file.write: writes {taskId, path, content,
// expectedMtime?} inside the task's worktree, creating the file if it
// doesn't exist, sandboxed the same way as file.list/file.read.
//
// expectedMtime (optional) makes it a conditional write: when set and the
// file's current mtime differs -- or the file is gone -- nothing is
// written and a coded "conflict"/"conflict_deleted" error goes back over
// the wire. When omitted, the write is the unconditional
// last-write-wins it always was.
func handleFileWrite(wm *workspace.Manager) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID        int64    `json:"taskId"`
			Path          string   `json:"path"`
			Content       string   `json:"content"`
			ExpectedMtime DateTime `json:"expectedMtime"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("file.write: invalid params: %w", err)
		}
		root, err := taskWorktreePath(wm, p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("file.write: %w", err)
		}
		var expected *time.Time
		if t := time.Time(p.ExpectedMtime); !t.IsZero() {
			expected = &t
		}
		mtime, err := files.Write(root, p.Path, p.Content, expected)
		if err != nil {
			return nil, fmt.Errorf("file.write: %w", err)
		}
		return fileWriteResult{Mtime: DateTime(mtime)}, nil
	}
}

// DateTime is time.Time marshaled as RFC 3339 nanoseconds -- an alias
// rather than raw time.Time so that (a) zero values collapse to null on
// the wire instead of 0001-01-01T00:00:00Z, making the optional params
// (expectedMtime) and results self-describing, and (b) the wire shape is
// fixed regardless of time.Time's own MarshalJSON staying stable.
type DateTime time.Time

func (d DateTime) MarshalJSON() ([]byte, error) {
	t := time.Time(d)
	if t.IsZero() {
		return []byte("null"), nil
	}
	return []byte(strconv.Quote(t.Format(time.RFC3339Nano))), nil
}

func (d *DateTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*d = DateTime{}
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	*d = DateTime(t)
	return nil
}
