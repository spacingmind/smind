package wsapi

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/terminal"
	"github.com/spacingmind/smind/internal/workspace"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// wsTransport adapts a real gorilla *websocket.Conn to Transport: it always
// sends text frames (the wire protocol is JSON text, per wsapi.go's doc
// comment) and discards the message type gorilla's ReadMessage returns,
// exactly as the pre-refactor conn.serve loop did.
type wsTransport struct{ ws *websocket.Conn }

func (t wsTransport) Receive() ([]byte, error) {
	_, data, err := t.ws.ReadMessage()
	return data, err
}

func (t wsTransport) Send(data []byte) error {
	return t.ws.WriteMessage(websocket.TextMessage, data)
}

func (t wsTransport) Close() error { return t.ws.Close() }

// API bundles the /ws http.Handler with the registries New constructs
// internally, for callers that need to reach a registry directly rather
// than only through the wire protocol -- specifically, Runs.CloseAll and
// Terminals.CloseAll on graceful daemon shutdown, so no agent subprocess or
// PTY-backed shell outlives the daemon itself (each Registry's own CloseAll
// doc comment covers what "outlives" is verified to mean) -- and, via
// ServeTransport, for a non-WebSocket connection (a relay-bridged mobile
// session) to reach the exact same RPC dispatch table a browser tab's /ws
// connection uses. Every other caller (existing tests, wsclient) only needs
// the http.Handler and should keep using Handler below.
type API struct {
	Handler   http.Handler
	Runs      *runs.Registry
	Terminals *terminal.Registry

	hs  map[string]handlerFunc
	bus *eventBus
}

// ServeTransport serves one RPC connection to completion over tr -- same
// handler dispatch table and event subscription as a WebSocket /ws
// connection, just over a different Transport. It blocks until tr's
// connection ends (a read error) or ctx is cancelled, exactly like a
// WebSocket connection's conn.serve. Used both by New's own /ws handler
// (via wsTransport) and by a relay bridge (cmd/smind) wrapping a
// *client.DataConn, so a mobile-originated request gets the same answer a
// browser tab's would.
func (a *API) ServeTransport(ctx context.Context, tr Transport) {
	c := newConn(tr, a.hs)
	c.eventsSub = newSubscriber()
	c.eventsBus = a.bus
	c.serve(ctx)
}

// New builds the full /ws API: one shared *runs.Registry and one shared
// *terminal.Registry, both backed by db (see runs.New's and terminal.New's
// doc comments on the reconciliation/rehydration each performs
// synchronously here, so New itself can fail if that startup work does),
// for every connection the returned Handler accepts (see Handler's doc
// comment for why a Run or terminal session's lifetime must be independent
// of any one connection), plus the http.Handler itself.
func New(wm *workspace.Manager, acctReg *accounts.Registry, runner *taskrunner.Runner, db *store.Store, token string) (*API, error) {
	reg, err := runs.New(db)
	if err != nil {
		return nil, fmt.Errorf("wsapi: new: %w", err)
	}
	treg, err := terminal.New(db)
	if err != nil {
		return nil, fmt.Errorf("wsapi: new: %w", err)
	}
	profReg := profiles.New(db)
	mcpReg := mcpservers.New(db)
	bus := newEventBus()
	wm.SetNotifier(busWorkspaceNotifier{bus: bus})
	reg.SetNotifier(busRunNotifier{bus: bus})
	profReg.SetNotifier(busProfileNotifier{bus: bus})
	mcpReg.SetNotifier(busMcpServerNotifier{bus: bus})

	// ADR-0021 §3/§5: give the Registry the dependencies its queue
	// delivery needs (the same wm/runner every Start caller passes in),
	// then deliver every chat's oldest queued prompt -- items that
	// survived a daemon restart keep delivering (decision 5, as amended).
	reg.SetStarter(wm, runner)
	reg.DeliverQueued()

	acctReg.SetNotifier(busAccountNotifier{bus: bus})
	coord := accounts.NewDefaultLoginCoordinator(acctReg)
	hs := methodHandlers(wm, acctReg, runner, reg, treg, profReg, mcpReg, coord, db)
	api := &API{Runs: reg, Terminals: treg, hs: hs, bus: bus}
	api.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		api.ServeTransport(r.Context(), wsTransport{ws})
	})
	return api, nil
}

// busWorkspaceNotifier adapts the shared event bus to workspace.Notifier,
// translating each Manager lifecycle notification into its ADR-0005/0009
// wire payload.
type busWorkspaceNotifier struct {
	bus *eventBus
}

func (b busWorkspaceNotifier) NotifyTaskStatus(taskID int64, status string) {
	b.bus.Publish(Event{Topic: TopicTaskStatus, Payload: taskStatusPayload{TaskID: taskID, Status: status}})
}

func (b busWorkspaceNotifier) NotifyWorkspaceCreated(w store.Workspace) {
	b.bus.Publish(Event{Topic: TopicWorkspaceCreated, Payload: workspaceCreatedPayload{Workspace: w}})
}

func (b busWorkspaceNotifier) NotifyWorkspaceDeleted(id int64) {
	b.bus.Publish(Event{Topic: TopicWorkspaceDeleted, Payload: workspaceDeletedPayload{ID: id}})
}

func (b busWorkspaceNotifier) NotifySpaceCreated(sp store.Space) {
	b.bus.Publish(Event{Topic: TopicSpaceCreated, Payload: spaceCreatedPayload{Space: sp}})
}

func (b busWorkspaceNotifier) NotifySpaceDeleted(id, workspaceID int64) {
	b.bus.Publish(Event{Topic: TopicSpaceDeleted, Payload: spaceDeletedPayload{ID: id, WorkspaceID: workspaceID}})
}

func (b busWorkspaceNotifier) NotifyTaskCreated(t store.Task) {
	b.bus.Publish(Event{Topic: TopicTaskCreated, Payload: taskCreatedPayload{Task: t}})
}

func (b busWorkspaceNotifier) NotifyTaskUpdated(t store.Task) {
	b.bus.Publish(Event{Topic: TopicTaskUpdated, Payload: taskUpdatedPayload{Task: t}})
}

func (b busWorkspaceNotifier) NotifyTaskArchived(t store.Task) {
	b.bus.Publish(Event{Topic: TopicTaskArchived, Payload: taskArchivedPayload{Task: t}})
}

func (b busWorkspaceNotifier) NotifyTaskDeleted(id, workspaceID int64, spaceID *int64) {
	b.bus.Publish(Event{Topic: TopicTaskDeleted, Payload: taskDeletedPayload{ID: id, WorkspaceID: workspaceID, SpaceID: spaceID}})
}

func (b busWorkspaceNotifier) NotifyChatCreated(c store.Chat) {
	b.bus.Publish(Event{Topic: TopicChatCreated, Payload: chatCreatedPayload{Chat: c}})
}

func (b busWorkspaceNotifier) NotifyChatUpdated(c store.Chat) {
	b.bus.Publish(Event{Topic: TopicChatUpdated, Payload: chatUpdatedPayload{Chat: c}})
}

func (b busWorkspaceNotifier) NotifyChatArchived(c store.Chat) {
	b.bus.Publish(Event{Topic: TopicChatArchived, Payload: chatArchivedPayload{Chat: c}})
}

// busAccountNotifier adapts the shared event bus to accounts.Notifier,
// translating each Registry lifecycle notification into its ADR-0015 wire
// payload -- the accountResult snapshot for account.updated (never a bare
// store.Account, so no credential_data can ride along), just the id for
// account.removed.
type busAccountNotifier struct {
	bus *eventBus
}

func (b busAccountNotifier) NotifyAccountUpdated(a store.Account) {
	result := accountResult{
		ID: a.ID, Provider: a.Provider, Label: a.Label, CredentialType: a.CredentialType,
		CreatedAt: a.CreatedAt.Format(time.RFC3339), UpdatedAt: a.UpdatedAt.Format(time.RFC3339),
	}
	b.bus.Publish(Event{Topic: TopicAccountUpdated, Payload: accountUpdatedPayload{Account: result}})
}

func (b busAccountNotifier) NotifyAccountRemoved(id int64) {
	b.bus.Publish(Event{Topic: TopicAccountRemoved, Payload: accountRemovedPayload{ID: id}})
}

// busProfileNotifier adapts the shared event bus to profiles.Notifier,
// translating each Registry lifecycle notification into its ADR-0014 wire
// payload -- mirrors busWorkspaceNotifier's shape.
type busProfileNotifier struct {
	bus *eventBus
}

func (b busProfileNotifier) NotifyProfileCreated(p store.AgentProfile) {
	b.bus.Publish(Event{Topic: TopicProfileCreated, Payload: profileCreatedPayload{Profile: p}})
}

func (b busProfileNotifier) NotifyProfileUpdated(p store.AgentProfile) {
	b.bus.Publish(Event{Topic: TopicProfileUpdated, Payload: profileUpdatedPayload{Profile: p}})
}

func (b busProfileNotifier) NotifyProfileDeleted(id int64) {
	b.bus.Publish(Event{Topic: TopicProfileDeleted, Payload: profileDeletedPayload{ID: id}})
}

// busMcpServerNotifier adapts the shared event bus to mcpservers.Notifier,
// translating each Registry lifecycle notification into its ADR-0018 wire
// payload -- mirrors busProfileNotifier's shape, redacting env/headers via
// mcpServerResultFrom on every publish (created/updated, including the
// setEnabled toggle, which also calls NotifyMcpServerUpdated).
type busMcpServerNotifier struct {
	bus *eventBus
}

func (b busMcpServerNotifier) NotifyMcpServerCreated(m store.McpServer) {
	b.bus.Publish(Event{Topic: TopicMcpServerCreated, Payload: mcpServerCreatedPayload{Server: mcpServerResultFrom(m)}})
}

func (b busMcpServerNotifier) NotifyMcpServerUpdated(m store.McpServer) {
	b.bus.Publish(Event{Topic: TopicMcpServerUpdated, Payload: mcpServerUpdatedPayload{Server: mcpServerResultFrom(m)}})
}

func (b busMcpServerNotifier) NotifyMcpServerDeleted(id int64) {
	b.bus.Publish(Event{Topic: TopicMcpServerDeleted, Payload: mcpServerDeletedPayload{ID: id}})
}

// busRunNotifier adapts the shared event bus to runs.Notifier, translating
// each Registry lifecycle notification into its ADR-0005 wire payload.
type busRunNotifier struct {
	bus *eventBus
}

func (b busRunNotifier) NotifyRunStatus(s runs.RunStatus) {
	b.bus.Publish(Event{Topic: TopicRunStatus, Payload: runStatusPayload{
		RunID: s.ID, TaskID: s.TaskID, ChatID: s.ChatID, Status: string(s.Status),
		StopReason: s.StopReason, Err: s.Err,
	}})
}

func (b busRunNotifier) NotifyPermissionPending(runID string, taskID, chatID int64, requestID, summary string, options []taskrunner.PermissionOption) {
	b.bus.Publish(Event{Topic: TopicPermissionPending, Payload: permissionPendingPayload{
		RunID: runID, TaskID: taskID, ChatID: chatID, RequestID: requestID, Summary: summary,
		Options: toPermissionOptionParams(options),
	}})
}

func (b busRunNotifier) NotifyChatQueueUpdated(chatID int64, items []store.ChatQueueItem) {
	b.bus.Publish(Event{Topic: TopicChatQueueUpdated, Payload: chatQueueUpdatedPayload{
		ChatID: chatID, Items: toChatQueueItemParams(items),
	}})
}

// Handler returns the http.Handler for the /ws endpoint alone -- a thin
// wrapper over New for callers that don't need direct registry access.
// See New's doc comment for what it wires up; see API's doc comment for
// why a separate constructor exists at all.
//
// Each accepted connection gets its own conn (see conn.go) serving requests
// until the client disconnects; conn.serve blocks for the connection's
// whole lifetime, so this handler doesn't return until then.
//
// All connections Handler accepts share one *runs.Registry, since a Run
// started on one connection (via task.prompt or a future run.start) must
// be reachable from any other connection's run.list/run.attach/run.logs/
// run.stop -- that's the whole point of tracking it server-side instead of
// inline in the request that started it. The same reasoning applies to the
// shared *terminal.Registry for terminal.create/attach/write/resize/close/
// list.
func Handler(wm *workspace.Manager, acctReg *accounts.Registry, runner *taskrunner.Runner, db *store.Store, token string) (http.Handler, error) {
	api, err := New(wm, acctReg, runner, db, token)
	if err != nil {
		return nil, err
	}
	return api.Handler, nil
}
