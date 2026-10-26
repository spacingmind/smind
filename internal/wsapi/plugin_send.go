package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// task.send from a plugin (ADR-0042 section 4, PB.3, capability tasks.write).
//
// It is NOT a wsapi method: the plugin session answers it itself, with exactly
// the parameters below, so a plugin cannot reach the fields run.start has
// (provider, permission mode, auto-accept, profile, notBefore, attachments).
// It queues one prompt on a chat the plugin could read with source
// `plugin:<id>`; delivery stamps `[message from plugin <id>]` on it. It never
// creates a task (so orchestration.maxDepth cannot be exceeded), the per-tree
// concurrency guard and the tree budget apply to the run it makes, and it never
// touches a permission request (ADR-0019).

const (
	CapTasksWrite     = "tasks.write"
	pluginRPCTaskSend = "task.send"

	// Limits (documented in ADR-0042's PB.3 note): sends per plugin and per
	// task tree in a sliding window, and the longest run of consecutive
	// plugin-sourced runs on one chat, which is what stops a
	// run.finished -> task.send loop.
	pluginSendsPerMinute = 6
	pluginSendsPerHour   = 60
	treeSendsPerHour     = 20
	pluginChainMax       = 3
	maxPluginPromptBytes = 16 << 10
)

// sendLimiter is a sliding-window counter per plugin and per task tree.
type sendLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	plugin map[string][]time.Time
	tree   map[int64][]time.Time
}

func newSendLimiter() *sendLimiter {
	return &sendLimiter{now: time.Now, plugin: map[string][]time.Time{}, tree: map[int64][]time.Time{}}
}

func trimWindow(ts []time.Time, now time.Time, window time.Duration) []time.Time {
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= window {
		i++
	}
	return ts[i:]
}

// allow records a send and returns "" when under every limit; otherwise the
// reason (naming the limit and when it frees up) and nothing is recorded.
func (l *sendLimiter) allow(plugin string, root int64) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	ps := trimWindow(l.plugin[plugin], now, time.Hour)
	ts := trimWindow(l.tree[root], now, time.Hour)
	l.plugin[plugin], l.tree[root] = ps, ts
	inMinute := 0
	var oldestMinute time.Time
	for _, t := range ps {
		if now.Sub(t) < time.Minute {
			if inMinute == 0 {
				oldestMinute = t
			}
			inMinute++
		}
	}
	if inMinute >= pluginSendsPerMinute {
		return fmt.Sprintf("plugin %s sent %d messages in the last minute (limit %d per minute); retry in %s", plugin, inMinute, pluginSendsPerMinute, retryIn(oldestMinute.Add(time.Minute).Sub(now)))
	}
	if len(ps) >= pluginSendsPerHour {
		return fmt.Sprintf("plugin %s sent %d messages in the last hour (limit %d per hour); retry in %s", plugin, len(ps), pluginSendsPerHour, retryIn(ps[0].Add(time.Hour).Sub(now)))
	}
	if len(ts) >= treeSendsPerHour {
		return fmt.Sprintf("plugins sent %d messages into task tree #%d in the last hour (limit %d per tree per hour); retry in %s", len(ts), root, treeSendsPerHour, retryIn(ts[0].Add(time.Hour).Sub(now)))
	}
	l.plugin[plugin] = append(ps, now)
	l.tree[root] = append(ts, now)
	return ""
}

func retryIn(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	return d.Round(time.Second).String()
}

// pluginSender answers task.send for every plugin session.
type pluginSender struct {
	wm     *workspace.Manager
	runner *taskrunner.Runner
	reg    *runs.Registry
	db     *store.Store
	mgr    *plugins.Manager
	lim    *sendLimiter
}

func rpcInvalid(format string, a ...any) *host.RPCError {
	return rpcErr(host.ErrCodeInvalidParams, "task.send: "+fmt.Sprintf(format, a...), nil)
}

// refuse is a limiter refusal: typed, with the reason the plugin and the
// Human both see (the Plugins page shows it as the plugin's lastError).
func (s *pluginSender) refuse(plugin, reason string) *host.RPCError {
	if s.mgr != nil {
		s.mgr.NotePluginProblem(plugin, pluginRPCTaskSend, "task.send refused: "+reason)
	}
	return rpcErr(host.ErrCodeRateLimited, "task.send refused: "+reason, map[string]string{"reason": reason})
}

type taskSendResult struct {
	Queued      bool   `json:"queued"`
	QueueItemID int64  `json:"queueItemId,omitempty"`
	RunID       string `json:"runId,omitempty"`
}

// send resolves one task.send. caps are the plugin's declared groups.
func (s *pluginSender) send(plugin string, caps map[string]bool, params json.RawMessage) (any, *host.RPCError) {
	if !caps[CapTasksRead] {
		return nil, rpcErr(host.ErrCodeForbidden, "task.send also needs the "+CapTasksRead+" capability: a plugin may only message a task it could read", map[string]string{"capability": CapTasksRead})
	}
	var p struct {
		TaskID   int64  `json:"taskId"`
		ChatID   int64  `json:"chatId"`
		Prompt   string `json:"prompt"`
		WhenBusy string `json:"whenBusy"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcInvalid("invalid params")
	}
	switch {
	case p.TaskID <= 0:
		return nil, rpcInvalid("taskId is required")
	case strings.TrimSpace(p.Prompt) == "":
		return nil, rpcInvalid("prompt is required")
	case len(p.Prompt) > maxPluginPromptBytes:
		return nil, rpcInvalid("prompt is %d bytes (the limit is %d)", len(p.Prompt), maxPluginPromptBytes)
	}
	whenBusy := runs.WhenBusyQueue
	switch p.WhenBusy {
	case "", "queue":
	case "reject":
		whenBusy = runs.WhenBusyReject
	default:
		return nil, rpcInvalid("whenBusy must be queue or reject")
	}
	// A task the plugin could not read through task.get is "not found" here too.
	task, err := s.wm.GetTask(p.TaskID)
	if err != nil || task.Kind == store.TaskKindSystem || task.ArchivedAt != nil {
		return nil, rpcInvalid("task %d not found", p.TaskID)
	}
	chat, err := s.chatFor(p.TaskID, p.ChatID)
	if err != nil {
		return nil, rpcInvalid("%v", err)
	}
	if chat.Provider == nil {
		return nil, rpcInvalid("chat %d has no provider yet: a human starts it first", chat.ID)
	}
	root, err := s.reg.TreeRoot(p.TaskID)
	if err != nil {
		return nil, rpcErr(host.ErrCodeInternal, "task.send: "+err.Error(), nil)
	}
	if reason := s.loop(plugin, p.TaskID, chat.ID); reason != "" {
		return nil, s.refuse(plugin, reason)
	}
	if reason := s.lim.allow(plugin, root); reason != "" {
		return nil, s.refuse(plugin, reason)
	}
	var opts []runs.StartOption
	if p.Model != "" {
		opts = append(opts, runs.WithModel(p.Model))
	}
	// context.Background: the run outlives this request, as in run.start.
	out, err := s.reg.StartWhenBusy(context.Background(), s.wm, s.runner, p.TaskID, chat.ID, taskrunner.Provider(*chat.Provider), p.Prompt,
		taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified, whenBusy, store.PluginSource(plugin), 0, 0, opts...)
	if err != nil {
		var tb *runs.TreeBusyError
		var be *runs.BudgetExceededError
		if errors.As(err, &tb) || errors.As(err, &be) {
			return nil, s.refuse(plugin, err.Error())
		}
		return nil, rpcInvalid("%v", err)
	}
	return taskSendResult{Queued: out.Queued, QueueItemID: out.QueueItemID, RunID: out.RunID}, nil
}

func (s *pluginSender) chatFor(taskID, chatID int64) (store.Chat, error) {
	if chatID == 0 {
		return s.wm.DefaultChat(taskID)
	}
	chat, err := s.wm.GetChat(chatID)
	if err != nil || chat.TaskID != taskID || chat.ArchivedAt != nil {
		return store.Chat{}, fmt.Errorf("chat %d not found on task %d", chatID, taskID)
	}
	return chat, nil
}

// loop refuses a send into a chat whose most recent runs were all started by
// this plugin's own messages: a plugin answering its own follow-ups
// (run.finished -> task.send -> run.finished ...) gets pluginChainMax runs and
// then stops until something else (a human, an agent, another source) has run.
func (s *pluginSender) loop(plugin string, taskID, chatID int64) string {
	rows, err := s.db.ListRunsByTask(taskID, pluginChainMax+4)
	if err != nil {
		return ""
	}
	src := store.PluginSource(plugin)
	chain := 0
	for _, r := range rows {
		if r.ChatID != chatID {
			continue
		}
		if r.Origin != src {
			break
		}
		chain++
	}
	if chain >= pluginChainMax {
		return fmt.Sprintf("loop stopped: the last %d runs of chat %d were all started by plugin %s's own messages; a message from someone else has to run before it may send again", chain, chatID, plugin)
	}
	return ""
}
