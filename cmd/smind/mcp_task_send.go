package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/wsclient"
)

// task_send, ADR-0017's one write tool: a non-blocking wrapper over the
// same run.start RPC `smind task send` uses (params shape identical, bar
// the streaming half the CLI bolts on afterward), returning {runId}
// immediately for the orchestrator to feed into task_wait/task_logs.
//
// It also carries ADR-0019 decision 6's guard (carried over from
// docs/plans/active/provider-native-permission-modes.md AC11/S17): an
// orchestrating agent may not pick an auto-approving permission
// configuration itself. Caller-supplied permissionMode/autoAccept values
// are checked with taskrunner.ModeAutoApproves against the provider.list
// catalog and rejected when they auto-approve; the same settings are
// allowed when they come from the human-authored agent profile named by
// profileId (and mixing profileId with explicit caller values is always
// rejected -- explicit values are never trusted).

// taskSendInput is task_send's argument shape. Provider is optional only
// when it can be inferred -- from the profile named by profileId, or from
// an already provider-bound chatId -- mirroring how run.start binds a
// provider to a chat on first prompt.
type taskSendInput struct {
	TaskID         int64  `json:"taskId" jsonschema:"id of the task to send the prompt to"`
	Prompt         string `json:"prompt" jsonschema:"the prompt to send"`
	Provider       string `json:"provider,omitempty" jsonschema:"provider id (e.g. glm, claude-native, codex-native); optional when profileId names one or chatId names an already-bound chat"`
	ChatID         *int64 `json:"chatId,omitempty" jsonschema:"optional id of the chat (conversation thread) to send to; defaults to the task's default chat"`
	PermissionMode string `json:"permissionMode,omitempty" jsonschema:"one of the provider's own permission mode ids (see provider.list); may not name an auto-approving mode"`
	AutoAccept     bool   `json:"autoAccept,omitempty" jsonschema:"approve every permission prompt without asking a human (ACP providers only); may not be set by an orchestrating agent"`
	ProfileID      *int64 `json:"profileId,omitempty" jsonschema:"optional id of a human-authored agent profile supplying the provider and permission settings"`
	WhenBusy       string `json:"whenBusy,omitempty" jsonschema:"what to do when the chat already has a running run: reject (error), queue (append, delivered at the next turn boundary -- the default), or interrupt (stop the running run and deliver first)"`
	FromTaskID     *int64 `json:"fromTaskId,omitempty" jsonschema:"optional: your own task id, for the [message from task #T, chat #C] provenance header on the delivered prompt"`
	FromChatID     *int64 `json:"fromChatId,omitempty" jsonschema:"optional: your own chat id, paired with fromTaskId"`
}

// taskSendOutput is task_send's structured output: either the started
// run's id, or (when the chat was busy and whenBusy was queue/interrupt --
// the default) {queued: true, queueItemId} -- this tool never blocks on
// the run finishing either way.
type taskSendOutput struct {
	RunID string `json:"runId,omitempty"`
	// Queued/QueueItemID are set instead of RunID when the prompt was
	// queued (ADR-0021 §2).
	Queued      bool  `json:"queued,omitempty"`
	QueueItemID int64 `json:"queueItemId,omitempty"`
}

// mcpTaskSend wraps run.start (the non-blocking half of task.prompt, the
// same RPC cmdTaskSend calls) behind the ADR-0019 permission guard.
func mcpTaskSend(client *wsclient.Client) mcp.ToolHandlerFor[taskSendInput, taskSendOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in taskSendInput) (*mcp.CallToolResult, taskSendOutput, error) {
		if strings.TrimSpace(in.Prompt) == "" {
			return nil, taskSendOutput{}, fmt.Errorf("task_send: prompt is required")
		}
		if in.TaskID <= 0 {
			return nil, taskSendOutput{}, fmt.Errorf("task_send: taskId is required")
		}

		provider := in.Provider
		settings := taskrunner.PermissionSettings{Mode: in.PermissionMode, AutoAccept: in.AutoAccept}
		explicitMode := in.PermissionMode != ""
		explicitAutoAccept := in.AutoAccept

		// A profile is the one trusted source of auto-approving settings:
		// its provider/mode/autoAccept were chosen by a human. Explicit
		// caller values alongside it are never trusted -- reject rather
		// than merge.
		if in.ProfileID != nil {
			if explicitMode || explicitAutoAccept {
				return nil, taskSendOutput{}, fmt.Errorf("task_send: permissionMode/autoAccept cannot be combined with profileId -- explicit caller values are never trusted (ADR-0019)")
			}
			var profile struct {
				Provider       string `json:"Provider"`
				PermissionMode string `json:"PermissionMode"`
				AutoAccept     bool   `json:"AutoAccept"`
			}
			if err := callWS(ctx, client, "task_send", "profile.get", map[string]any{"id": *in.ProfileID}, &profile); err != nil {
				return nil, taskSendOutput{}, err
			}
			if provider == "" {
				provider = profile.Provider
			} else if provider != profile.Provider {
				return nil, taskSendOutput{}, fmt.Errorf("task_send: provider %q does not match profileId %d's provider %q", provider, *in.ProfileID, profile.Provider)
			}
			settings = taskrunner.PermissionSettings{Mode: profile.PermissionMode, AutoAccept: profile.AutoAccept}
		} else if provider == "" && in.ChatID != nil {
			// An already-bound chat pins the provider; a fresh chat does
			// not, and run.start's bind-on-first-prompt needs one.
			p, err := chatProviderFor(ctx, client, in.TaskID, *in.ChatID)
			if err != nil {
				return nil, taskSendOutput{}, err
			}
			provider = p
		}
		if provider == "" {
			return nil, taskSendOutput{}, fmt.Errorf("task_send: provider is required (or a profileId/chatId it can be inferred from)")
		}

		// The guard itself, only over caller-supplied settings -- a
		// profile's auto-approving configuration was human-approved.
		if in.ProfileID == nil {
			catalog, err := providerCatalogFor(ctx, client, provider)
			if err != nil {
				return nil, taskSendOutput{}, err
			}
			if settings.AutoAccept {
				return nil, taskSendOutput{}, fmt.Errorf("task_send: autoAccept may not be set by an orchestrating agent; ask a human, or pass a profileId whose profile carries it (ADR-0019)")
			}
			if explicitMode && taskrunner.ModeAutoApproves(catalog, settings) {
				return nil, taskSendOutput{}, fmt.Errorf("task_send: permissionMode %q auto-approves tool calls without asking a human; an orchestrating agent may not pick it -- pass a profileId whose profile carries it instead (ADR-0019)", settings.Mode)
			}
		}

		whenBusy := in.WhenBusy
		if whenBusy == "" {
			// ADR-0021 §9: MCP task_send defaults to queue, so a peer
			// escalation to a busy lead is never lost.
			whenBusy = "queue"
		}
		// Provenance (ADR-0021 §4): an agent names itself with
		// fromTaskId/fromChatId; anything else is an orchestrator. A
		// profileId send is the one non-human call that may carry
		// auto-approving settings, because the profile itself is the
		// human-authored authorization (ADR-0019 decision 6) -- so it
		// rides as a human-sourced item.
		source := "orchestrator"
		if in.FromTaskID != nil && in.FromChatID != nil {
			source = "agent"
		}
		// Known limitation (ADR-0021 §4 vs ADR-0019 decision 6): a
		// profileId send rides as source=human, so the server's
		// non-human auto-approve guard accepts the profile's
		// human-authored settings -- which also means agent provenance
		// (the [message from task #T, chat #C] header) is dropped for
		// profile sends. A profile-carrying agent that wants the header
		// must forgo the profile or accept the loss.
		if in.ProfileID != nil {
			source = "human"
		}
		params := map[string]any{
			"taskId": in.TaskID, "provider": provider, "prompt": in.Prompt,
			"permissionMode": settings.Mode, "autoAccept": settings.AutoAccept,
			"whenBusy": whenBusy, "source": source,
		}
		if in.ChatID != nil {
			params["chatId"] = *in.ChatID
		}
		if source == "agent" {
			params["fromTaskId"] = *in.FromTaskID
			params["fromChatId"] = *in.FromChatID
		}
		var start struct {
			RunID       string `json:"runId"`
			Queued      bool   `json:"queued"`
			QueueItemID int64  `json:"queueItemId"`
		}
		if err := callWS(ctx, client, "task_send", "run.start", params, &start); err != nil {
			return nil, taskSendOutput{}, err
		}
		return nil, taskSendOutput{RunID: start.RunID, Queued: start.Queued, QueueItemID: start.QueueItemID}, nil
	}
}

// providerCatalogFor fetches provider.list and returns provider's entry,
// decoded into taskrunner's own ProviderInfo so ModeAutoApproves sees the
// same catalog shape the daemon validated against.
func providerCatalogFor(ctx context.Context, client *wsclient.Client, provider string) (taskrunner.ProviderInfo, error) {
	var listed struct {
		Providers []taskrunner.ProviderInfo `json:"providers"`
	}
	if err := callWS(ctx, client, "task_send", "provider.list", nil, &listed); err != nil {
		return taskrunner.ProviderInfo{}, err
	}
	for _, p := range listed.Providers {
		if string(p.ID) == provider {
			return p, nil
		}
	}
	return taskrunner.ProviderInfo{}, fmt.Errorf("task_send: unknown provider %q", provider)
}

// chatProviderFor returns the provider chatID is already bound to, via
// chat.list -- an unbound chat yields "" (the caller's error, not ours).
func chatProviderFor(ctx context.Context, client *wsclient.Client, taskID, chatID int64) (string, error) {
	var chats []map[string]any
	if err := callWS(ctx, client, "task_send", "chat.list", map[string]any{"taskId": taskID, "includeArchived": true}, &chats); err != nil {
		return "", err
	}
	for _, chat := range chats {
		if id, ok := chat["ID"].(float64); ok && int64(id) == chatID {
			if provider, ok := chat["Provider"].(string); ok {
				return provider, nil
			}
			return "", nil
		}
	}
	return "", fmt.Errorf("task_send: chat %d does not belong to task %d", chatID, taskID)
}
