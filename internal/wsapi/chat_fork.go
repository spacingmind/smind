package wsapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// chatForkInfo is the additive `fork` field of a forked chat (ADR-0057).
type chatForkInfo struct {
	ParentChatID  int64  `json:"parentChatId"`
	ForkedAtRunID string `json:"forkedAtRunId"`
	Kind          string `json:"kind"`
	Mode          string `json:"mode"`
	// Lossy marks a seeded fork: the agent knows what was said, not what it did.
	Lossy bool `json:"lossy"`
}

// chatView is a chat as chat.list, chat.get and chat.fork answer it: the chat's
// own fields, unchanged and in the same order, plus `fork` for a forked chat only
// (omitted otherwise, so an unforked chat's payload is byte-identical to before).
type chatView struct {
	store.Chat
	Fork *chatForkInfo `json:"fork,omitempty"`
}

func forkInfoOf(f store.ChatFork) *chatForkInfo {
	return &chatForkInfo{ParentChatID: f.ParentChatID, ForkedAtRunID: f.ForkedAtRunID, Kind: f.Kind, Mode: f.Mode, Lossy: f.Mode == store.ChatForkModeSeeded}
}

// chatViewsOf decorates chats of one task with their fork records.
func chatViewsOf(db *store.Store, taskID int64, chats []store.Chat) ([]chatView, error) {
	forks, err := db.ListChatForksByTask(taskID)
	if err != nil {
		return nil, err
	}
	out := make([]chatView, len(chats))
	for i, c := range chats {
		out[i] = chatView{Chat: c}
		if f, ok := forks[c.ID]; ok {
			out[i].Fork = forkInfoOf(f)
		}
	}
	return out, nil
}

func chatViewOf(db *store.Store, c store.Chat) chatView {
	v := chatView{Chat: c}
	switch f, err := db.GetChatFork(c.ID); {
	case err == nil:
		v.Fork = forkInfoOf(f)
	case !errors.Is(err, sql.ErrNoRows):
		log.Printf("wsapi: chat %d: fork record: %v", c.ID, err)
	}
	return v
}

// handleChatFork implements chat.fork {chatId, atRunId, mode?}: a new chat of the
// same task that continues from the end of a finished turn of chatId (ADR-0057).
// mode "native" (the default) forks the provider's own session and is refused with
// fork_unavailable when it cannot; "seeded" starts a fresh session whose first
// prompt carries a condensed account of the earlier turns. Human only: a
// run-scoped caller is refused, and it is not an MCP tool. Nothing here writes the
// parent chat or its session.
func handleChatFork(wm *workspace.Manager, reg *runs.Registry, db *store.Store) handlerFunc {
	const m = "chat.fork"
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			ChatID  int64  `json:"chatId"`
			AtRunID string `json:"atRunId"`
			Mode    string `json:"mode"`
			// Kind is "fork" (the default) or "retry" (edit-and-retry: the fork's anchor is the
			// turn BEFORE the one being replaced; the client sends the edited prompt next).
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": invalid params"}
		}
		mode := p.Mode
		if mode == "" {
			mode = store.ChatForkModeNative
		}
		if p.ChatID <= 0 || p.AtRunID == "" || (mode != store.ChatForkModeNative && mode != store.ChatForkModeSeeded) {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": chatId, atRunId and a mode of native or seeded are required"}
		}
		kind := p.Kind
		if kind == "" {
			kind = store.ChatForkKindFork
		}
		if kind != store.ChatForkKindFork && kind != store.ChatForkKindRetry {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": kind must be fork or retry"}
		}
		parent, err := db.GetChat(p.ChatID)
		if err != nil {
			return nil, &rpcCodeError{code: codeNotFound, msg: m + ": no such chat"}
		}
		run, err := db.GetRun(p.AtRunID)
		if err != nil || run.ChatID != parent.ID || run.Status != string(runs.StatusDone) || run.Purpose != "user" {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": atRunId must be a finished run of this chat"}
		}
		if parent.Provider == nil {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": the chat has not run yet"}
		}
		title := forkTitle(parent.Title)
		if kind == store.ChatForkKindRetry {
			title = retryTitle(parent.Title)
		}

		var setup func(store.Chat) error
		switch mode {
		case store.ChatForkModeNative:
			anchor := reg.ForkAnchorOf(ctx, p.AtRunID)
			if !anchor.OK() {
				return nil, &rpcCodeError{code: codeForkUnavailable, msg: fmt.Sprintf("%s: the session cannot be forked here (%s)", m, anchor.Reason),
					data: map[string]any{"reason": string(anchor.Reason), "detail": anchor.Detail, "seededAvailable": true}}
			}
			handle := taskrunner.SessionHandle{Provider: taskrunner.Provider(*parent.Provider), SessionID: anchor.SessionID, Fork: &taskrunner.ForkAt{EntryUUID: anchor.EntryUUID}}
			if run.FinishedAt != nil {
				if js, terr := db.ChatClaudeTotalsAt(parent.ID, *run.FinishedAt); terr != nil {
					log.Printf("wsapi: %s: totals baseline of chat %d: %v", m, parent.ID, terr)
				} else if js != "" {
					var totals taskrunner.ClaudeUsageTotals
					if uerr := json.Unmarshal([]byte(js), &totals); uerr != nil {
						log.Printf("wsapi: %s: totals baseline of chat %d is unreadable, the child starts without one: %v", m, parent.ID, uerr)
					} else {
						handle.UsageTotals = &totals
					}
				}
			}
			data, merr := json.Marshal(handle)
			if merr != nil {
				return nil, merr
			}
			setup = func(c store.Chat) error {
				if _, err := db.BindChatProvider(c.ID, *parent.Provider); err != nil {
					return err
				}
				if _, err := db.SetChatAgentSession(c.ID, string(data)); err != nil {
					return err
				}
				return db.InsertChatFork(store.ChatFork{ChatID: c.ID, ParentChatID: parent.ID, ForkedAtRunID: p.AtRunID, Kind: kind, Mode: store.ChatForkModeNative})
			}
		default:
			all, lerr := db.ListUserRunsByChat(parent.ID)
			if lerr != nil {
				return nil, lerr
			}
			var upTo []store.Run
			for _, r := range all {
				upTo = append(upTo, r)
				if r.ID == p.AtRunID {
					break
				}
			}
			seed := runs.ForkSeed(upTo, func(id string) string {
				t, terr := reg.FinalAgentText(id)
				if terr != nil {
					log.Printf("wsapi: %s: final text of run %s: %v", m, id, terr)
				}
				return t
			})
			if seed == "" {
				return nil, &rpcCodeError{code: codeInvalid, msg: m + ": the earlier turns have nothing to carry"}
			}
			setup = func(c store.Chat) error {
				if _, err := db.BindChatProvider(c.ID, *parent.Provider); err != nil {
					return err
				}
				return db.InsertChatFork(store.ChatFork{ChatID: c.ID, ParentChatID: parent.ID, ForkedAtRunID: p.AtRunID, Kind: kind, Mode: store.ChatForkModeSeeded, Seed: seed, CreatedAt: time.Now()})
			}
		}
		child, err := wm.CreateChatThen(parent.TaskID, title, setup)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		return chatViewOf(db, child), nil
	}
}

func forkTitle(parent string) string { return "Fork of " + titleBase(parent) }

func retryTitle(parent string) string { return "Retry of " + titleBase(parent) }

func titleBase(parent string) string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		parent = "Chat"
	}
	if utf8.RuneCountInString(parent) > forkTitleMax {
		parent = string([]rune(parent)[:forkTitleMax])
	}
	return parent
}

// forkTitleMax caps the parent's title (in characters) inside a fork's title.
const forkTitleMax = 60

// handleChatForkSeed implements chat.forkSeed {chatId}: the text a seeded fork was
// given in place of the earlier turns, so the child chat can show what the agent
// actually received (ADR-0057). Human only; "" for a chat that is not a seeded fork.
func handleChatForkSeed(db *store.Store) handlerFunc {
	const m = "chat.forkSeed"
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			ChatID int64 `json:"chatId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.ChatID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": chatId is required"}
		}
		f, err := db.GetChatFork(p.ChatID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && f.Mode != store.ChatForkModeSeeded) {
			return map[string]any{"seed": ""}, nil
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"seed": f.Seed}, nil
	}
}
