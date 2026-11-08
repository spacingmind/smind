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

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// codeForkUnavailable is the wire code of a native fork that cannot be made; its
// data names the reason and says the seeded fallback is open.
const codeForkUnavailable = "fork_unavailable"

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
func chatViewsOf(db *store.Store, taskID int64, chats []store.Chat) []chatView {
	forks, err := db.ListChatForksByTask(taskID)
	if err != nil {
		log.Printf("wsapi: chat list: fork records of task %d: %v", taskID, err)
	}
	out := make([]chatView, len(chats))
	for i, c := range chats {
		out[i] = chatView{Chat: c}
		if f, ok := forks[c.ID]; ok {
			out[i].Fork = forkInfoOf(f)
		}
	}
	return out
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
		parent, err := db.GetChat(p.ChatID)
		if err != nil {
			return nil, &rpcCodeError{code: codeNotFound, msg: m + ": no such chat"}
		}
		run, err := db.GetRun(p.AtRunID)
		if err != nil || run.ChatID != parent.ID || run.Status != string(runs.StatusDone) {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": atRunId must be a finished run of this chat"}
		}
		if parent.Provider == nil {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": the chat has not run yet"}
		}
		title := forkTitle(parent.Title)

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
					if json.Unmarshal([]byte(js), &totals) == nil {
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
				return db.InsertChatFork(store.ChatFork{ChatID: c.ID, ParentChatID: parent.ID, ForkedAtRunID: p.AtRunID, Kind: store.ChatForkKindFork, Mode: store.ChatForkModeNative})
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
				t, _ := reg.FinalAgentText(id)
				return t
			})
			if seed == "" {
				return nil, &rpcCodeError{code: codeInvalid, msg: m + ": the earlier turns have nothing to carry"}
			}
			setup = func(c store.Chat) error {
				if _, err := db.BindChatProvider(c.ID, *parent.Provider); err != nil {
					return err
				}
				return db.InsertChatFork(store.ChatFork{ChatID: c.ID, ParentChatID: parent.ID, ForkedAtRunID: p.AtRunID, Kind: store.ChatForkKindFork, Mode: store.ChatForkModeSeeded, Seed: seed, CreatedAt: time.Now()})
			}
		}
		child, err := wm.CreateChatThen(parent.TaskID, title, setup)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		return chatViewOf(db, child), nil
	}
}

func forkTitle(parent string) string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		parent = "Chat"
	}
	if len(parent) > 60 {
		parent = parent[:60]
	}
	return "Fork of " + parent
}
