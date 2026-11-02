package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// The two heredoc reveals (docs/plans/active/permission-heredoc-reveal.md).
// Permission cards and tool calls show a heredoc body elided; these RPCs hand
// one body to a human who asked for it, passed through the same redactor the
// audit uses. Both are human-only (a local or relay connection, never a
// run-scoped agent: neither is on RunScopedMethods and each refuses agents),
// never put the body in an event, a log or the audit, and write one
// content-free audit row.

// revealMaxBytes caps a revealed body, matching the 64 KiB command cap.
const revealMaxBytes = 64 << 10

// heredocRevealMethods lists both RPCs; a test walks it.
var heredocRevealMethods = []string{"permission.revealHeredoc", "run.revealToolHeredoc"}

type heredocRevealResult struct {
	Index     int    `json:"index"`
	Bytes     int    `json:"bytes"`
	Text      string `json:"text"`
	Redacted  bool   `json:"redacted"`
	Truncated bool   `json:"truncated"`
}

func revealResult(index int, raw string) heredocRevealResult {
	home, _ := os.UserHomeDir()
	var homes []string
	if home != "" {
		homes = []string{home}
	}
	text := redact.New(nil, homes).String(raw)
	res := heredocRevealResult{Index: index, Bytes: len(raw), Text: text, Redacted: text != raw}
	if len(res.Text) > revealMaxBytes {
		res.Text, res.Truncated = capUTF8Reveal(res.Text, revealMaxBytes), true
	}
	return res
}

// capUTF8Reveal cuts s to at most max bytes on a rune boundary.
func capUTF8Reveal(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && s[max]&0xC0 == 0x80 {
		max--
	}
	return s[:max]
}

// heredocSource is what the reveals read from: *runs.Registry in the daemon.
type heredocSource interface {
	PendingHeredoc(requestID string, index int) (runs.HeredocReveal, error)
	ToolCallHeredoc(runID, toolCallID string, index int) (runs.HeredocReveal, error)
}

func heredocRevealHandlers(reg heredocSource, db *store.Store) map[string]handlerFunc {
	hs := map[string]handlerFunc{}

	audit := func(source string, rev runs.HeredocReveal, requestID, target string, index, n int) {
		// Content-free: where and how big, never the body. A failed write is
		// logged but does not block the reveal the human already asked for.
		_, err := db.AppendPolicyAudit(store.PolicyAuditRow{
			RunID: rev.RunID, TaskID: rev.TaskID, RequestID: requestID,
			Tool: source + ".heredoc_revealed", Target: fmt.Sprintf("%s heredoc %d (%d bytes)", target, index, n),
			Decision: "heredoc_revealed",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "wsapi: heredoc reveal audit: %v\n", err)
		}
	}

	hs["permission.revealHeredoc"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "permission.revealHeredoc"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			PermissionID string `json:"permissionId"`
			Index        int    `json:"index"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.PermissionID == "" {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": permissionId is required"}
		}
		rev, err := reg.PendingHeredoc(p.PermissionID, p.Index)
		if errors.Is(err, runs.ErrNoHeredoc) {
			return nil, &rpcCodeError{code: codeNotFound, msg: m + ": no such heredoc on a pending permission"}
		}
		if err != nil {
			return nil, err
		}
		audit("permission", rev, p.PermissionID, "permission "+p.PermissionID, p.Index, len(rev.Text))
		return revealResult(p.Index, rev.Text), nil
	}

	hs["run.revealToolHeredoc"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "run.revealToolHeredoc"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			RunID      string `json:"runId"`
			ToolCallID string `json:"toolCallId"`
			Index      int    `json:"index"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.RunID == "" || p.ToolCallID == "" {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": runId and toolCallId are required"}
		}
		rev, err := reg.ToolCallHeredoc(p.RunID, p.ToolCallID, p.Index)
		if errors.Is(err, runs.ErrNoHeredoc) {
			return nil, &rpcCodeError{code: codeNotFound, msg: m + ": no such heredoc on that tool call"}
		}
		if err != nil {
			return nil, err
		}
		audit("tool", rev, "", "run "+p.RunID+" call "+p.ToolCallID, p.Index, len(rev.Text))
		return revealResult(p.Index, rev.Text), nil
	}
	return hs
}
