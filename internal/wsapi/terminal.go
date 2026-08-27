package wsapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/spacingmind/smind/internal/terminal"
	"github.com/spacingmind/smind/internal/workspace"
)

// terminalCreateResult is the terminal result of a successful
// terminal.create: just the new session's id, returned as soon as the PTY
// is spawned -- terminal.create never blocks on anything the shell does
// afterward, mirroring run.start's runStartResult.
type terminalCreateResult struct {
	TerminalID string `json:"terminalId"`
}

// handleTerminalCreate spawns a new PTY-backed shell session for a task,
// with its cwd set to that task's real git worktree (store.Task's
// WorktreePath), and returns its id immediately -- see
// internal/terminal.Registry.Create's doc comment.
func handleTerminalCreate(wm *workspace.Manager, treg *terminal.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("terminal.create: invalid params: %w", err)
		}

		task, err := wm.GetTask(p.TaskID)
		if err != nil {
			return nil, fmt.Errorf("terminal.create: %w", err)
		}
		if task.WorktreePath == nil {
			return nil, fmt.Errorf("terminal.create: task %d has no worktree yet", p.TaskID)
		}

		id, err := treg.Create(p.TaskID, *task.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("terminal.create: %w", err)
		}
		return terminalCreateResult{TerminalID: id}, nil
	}
}

// terminalDataParams is the params payload of every "data" event
// terminal.attach emits: one chunk of raw PTY output, base64-encoded.
// Base64 rather than a plain JSON string deliberately -- a PTY's byte
// stream isn't guaranteed to be valid UTF-8 at arbitrary chunk boundaries
// (a multi-byte character split across two reads, or genuinely binary
// output from a program running inside the shell), and encoding/json
// silently mangles invalid UTF-8 in a plain string rather than erroring,
// which would corrupt exactly the bytes a terminal emulator most needs
// byte-exact. See internal/terminal.Event's doc comment.
type terminalDataParams struct {
	Data string `json:"data"`
}

// terminalAttachResult is the terminal result of terminal.attach once the
// session itself closes (the shell exited, or terminal.close was called).
// Unlike a Run, a terminal session has no notion of succeeding or
// failing -- just running or closed -- so there's no stopReason/error
// analog here.
type terminalAttachResult struct {
	TerminalID string `json:"terminalId"`
}

// handleTerminalAttach streams terminalId's backfilled-then-live output
// as "data" events, exactly like run.attach's backfill+live contract
// (see that handler's doc comment), until the session closes. Unlike
// terminal.close, this request's own context going Done (connection
// closing, or a task.cancel naming this request's id) only detaches --
// the shell keeps running -- matching the "detaching does not kill the
// session" requirement from docs/plans/active/web-ui-terminal.md.
func handleTerminalAttach(treg *terminal.Registry) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TerminalID string `json:"terminalId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("terminal.attach: invalid params: %w", err)
		}

		events, unsubscribe, err := treg.Subscribe(p.TerminalID)
		if err != nil {
			return nil, fmt.Errorf("terminal %s: %w", p.TerminalID, err)
		}
		defer unsubscribe()

		for {
			select {
			case e, ok := <-events:
				if !ok {
					return terminalAttachResult{TerminalID: p.TerminalID}, nil
				}
				rc.Emit("data", terminalDataParams{Data: base64.StdEncoding.EncodeToString(e.Data)})
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
}

// handleTerminalWrite sends data to terminalId's PTY as if it had been
// typed into the shell -- this is how keystrokes/input from a client
// reach the shell. Unlike terminal.attach's output-direction "data"
// events, data here is a plain (not base64) JSON string: it always
// originates as a JS string from a browser's own keyboard/paste input
// (xterm.js's onData), which is always valid UTF-8 on the wire, so there
// is no analog to the output side's split-multi-byte-character or
// binary-output risk.
func handleTerminalWrite(treg *terminal.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TerminalID string `json:"terminalId"`
			Data       string `json:"data"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("terminal.write: invalid params: %w", err)
		}
		if err := treg.Write(p.TerminalID, []byte(p.Data)); err != nil {
			return nil, fmt.Errorf("terminal.write: %w", err)
		}
		return struct{}{}, nil
	}
}

// handleTerminalResize resizes terminalId's PTY to a real cols x rows,
// so the shell (and any TUI program running inside it) sees a real
// window-size change, not a fixed default -- see
// internal/terminal.Registry.Resize.
func handleTerminalResize(treg *terminal.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TerminalID string `json:"terminalId"`
			Cols       uint16 `json:"cols"`
			Rows       uint16 `json:"rows"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("terminal.resize: invalid params: %w", err)
		}
		if err := treg.Resize(p.TerminalID, p.Cols, p.Rows); err != nil {
			return nil, fmt.Errorf("terminal.resize: %w", err)
		}
		return struct{}{}, nil
	}
}

// handleTerminalClose actually kills terminalId's shell process and
// closes its PTY, regardless of which connection (if any) created it --
// analogous to run.stop, except a terminal session has no "already
// finished" state to be a no-op against the same way a Run does; see
// internal/terminal.Registry.Close for exactly what "actually kills" is
// verified to mean.
func handleTerminalClose(treg *terminal.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TerminalID string `json:"terminalId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("terminal.close: invalid params: %w", err)
		}
		if err := treg.Close(p.TerminalID); err != nil {
			return nil, fmt.Errorf("terminal.close: %w", err)
		}
		return struct{}{}, nil
	}
}

// handleTerminalList returns a summary of every terminal session
// belonging to taskId the Registry currently knows about -- mirrors
// run.list's reasoning (a UI reconnecting, or opening a second tab, needs
// to discover an already-running session rather than always creating a
// new one), except filtered server-side by taskId rather than
// client-side, since a terminal session inherently belongs to exactly one
// task's worktree (see internal/terminal.Registry.List's doc comment).
func handleTerminalList(treg *terminal.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("terminal.list: invalid params: %w", err)
		}
		return treg.List(p.TaskID), nil
	}
}
