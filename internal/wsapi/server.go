package wsapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/ghctx"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/terminal"
	"github.com/spacingmind/smind/internal/workspace"
)

// Subprotocol is the WebSocket subprotocol every client may offer. Browsers
// cannot set an Authorization header on a WebSocket, so a session token (see
// internal/auth.SessionStore) travels as a second offered subprotocol,
// "smind.bearer.<token>"; the server always answers with Subprotocol itself,
// never the token, and never reads a session token from the URL (proxies log
// URLs).
const Subprotocol = "smind"

// BearerSubprotocolPrefix prefixes the token in the offered subprotocol.
const BearerSubprotocolPrefix = "smind.bearer."

// upgrader keeps gorilla's default CheckOrigin (a request with an Origin must
// be same-origin with its Host), ADR-0013 / ADR-0031 D8.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	Subprotocols:    []string{Subprotocol},
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

	hs      map[string]handlerFunc
	admit   *seatAdmission
	bus     *eventBus
	git     *gitstate.Service
	focus   *focusRegistry
	prFocus *focusRegistry
	info    *DaemonInfo
	// pushRegistered reports whether a device has a push registration; set by SetPush when enabled.
	pushRegistered func(string) bool

	// github backs forge.search (SetGithubClient).
	github *githubHolder

	// forgeAuth backs forge.status/setToken/logout and the https credential
	// helper for the daemon's own git network calls.
	forgeAuth *forgeAuth

	// learning backs the human-only learning.* RPCs (SetLearning).
	learning *learningHost

	// authorize decides whether a /ws upgrade may proceed. New installs the
	// legacy rule (the daemon token in the "token" query parameter);
	// SetAuthorizer replaces it. Set before the first request is served.
	authorize func(*http.Request) bool

	// connsMu guards conns and closing: the live /ws connections, and whether
	// CloseConnections has run (later upgrades are closed immediately).
	connsMu sync.Mutex
	conns   map[*websocket.Conn]struct{}
	closing bool
}

// StartGitState runs the git state service's loop (ADR-0039 §4) until ctx is
// done or the returned stop func is called. The loop does nothing while no
// connection is subscribed to task.git.
func (a *API) StartGitState(ctx context.Context) (stop func()) {
	a.git.Start(ctx)
	return a.git.Stop
}

// Close stops every still-running agent run and terminal session the API
// owns and waits for their goroutines to finish (runs.Registry.CloseAll,
// terminal.Registry.CloseAll). Call it after the HTTP server stops
// accepting connections and before the store closes, so no background
// writer is left touching the database or a worktree directory.
func (a *API) Close() {
	a.git.Stop()
	a.Runs.CloseAll()
	a.Terminals.CloseAll()
}

// SetAuthorizer replaces the /ws admission rule. internal/server uses it to
// accept session tokens (direct connections with a password, ADR-0031 D8) and
// to keep the daemon-token query rule for true loopback callers only. It is not
// safe to call while requests are being served.
func (a *API) SetAuthorizer(fn func(*http.Request) bool) { a.authorize = fn }

// ServeTransport serves one RPC connection to completion over tr (channel
// "e2ee": a relay-bridged session) -- same
// handler dispatch table and event subscription as a WebSocket /ws
// connection, just over a different Transport. It blocks until tr's
// connection ends (a read error) or ctx is cancelled, exactly like a
// WebSocket connection's conn.serve. Used both by New's own /ws handler
// (via wsTransport) and by a relay bridge (cmd/smind) wrapping a
// *client.DataConn, so a mobile-originated request gets the same answer a
// browser tab's would.
func (a *API) ServeTransport(ctx context.Context, tr Transport) {
	a.serve(ctx, tr, false, RelayMaxMessage, "", ChannelE2EE)
}

// ServeDevice is ServeTransport for a relay session whose device proved its identity (device_hello): handlers see its device id in requestContext.deviceID.
func (a *API) ServeDevice(ctx context.Context, tr Transport, deviceID string) {
	a.serve(ctx, tr, false, RelayMaxMessage, deviceID, ChannelE2EE)
}

// serveLocal is ServeTransport for the token-authenticated /ws connection:
// the one surface allowed to administer the daemon itself (relay.device.*).
func (a *API) serveLocal(ctx context.Context, tr Transport, ch Channel) {
	a.serve(ctx, tr, true, DirectMaxMessage, "", ch)
}

func (a *API) serve(ctx context.Context, tr Transport, local bool, maxMessage int, deviceID string, ch Channel) {
	c := newConn(tr, a.hs)
	c.local = local
	c.deviceID = deviceID
	c.channel = ch
	c.maxMessage = maxMessage
	c.admit = a.admit
	c.runs = a.Runs // run-scoped identity is set later by session.identify
	c.eventsSub = newSubscriber()
	c.eventsBus = a.bus
	c.focus = a.focus
	c.prFocus = a.prFocus
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
	return newAPI(wm, acctReg, runner, db, token, gitOptions{})
}

func newAPI(wm *workspace.Manager, acctReg *accounts.Registry, runner *taskrunner.Runner, db *store.Store, token string, gopts gitOptions) (*API, error) {
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
	focus := newFocusRegistry()
	prFocus := newFocusRegistry()
	gh := &githubHolder{c: ghctx.New()}
	fauth := newForgeAuth(gh, bus)
	gitSvc := newGitService(wm, db, bus, focus, gh, reg, fauth.networkGit, gopts)
	fauth.git = gitSvc
	// task.create with a prNumber asks the same provider, looked up per
	// call, whether the PR comes from a fork (ADR-0039 section 7).
	wm.SetForge(func() forge.Provider {
		if gh.c == nil {
			return nil
		}
		return gh.c
	})
	// Archive's branch cleanup decides on the service's fresh snapshot, which
	// carries the poller's cached PR state (ADR-0039 section 9).
	wm.SetGitFacts(gitSvc.Fresh)
	wm.SetNotifier(busWorkspaceNotifier{bus: bus, db: db, git: gitSvc})
	reg.SetNotifier(busRunNotifier{bus: bus, db: db, git: gitSvc})
	profReg.SetNotifier(busProfileNotifier{bus: bus})
	mcpReg.SetNotifier(busMcpServerNotifier{bus: bus})

	// ADR-0021 §3/§5: give the Registry the dependencies its queue
	// delivery needs (the same wm/runner every Start caller passes in),
	// then deliver every chat's oldest queued prompt -- items that
	// survived a daemon restart keep delivering (decision 5, as amended).
	reg.SetStarter(wm, runner)
	reg.DeliverQueued()
	// ADR-0041 B3: tell parents about child runs that finished (or were
	// interrupted by the restart) without having been announced.
	reg.NotifyPendingChildren()
	// ADR-0024 §2: a prompt queued while a task's setup script ran is
	// delivered when setup finishes (ok or failed).
	wm.SetSetupFinishedHook(func(int64) { reg.DeliverQueued() })

	acctReg.SetNotifier(busAccountNotifier{bus: bus})
	coord := accounts.NewDefaultLoginCoordinator(acctReg)
	hs := methodHandlers(wm, acctReg, runner, reg, treg, profReg, mcpReg, coord, db, gh)
	info := newDaemonInfo(time.Now())
	for method, h := range gitHandlers(gitSvc, focus, prFocus) {
		hs[method] = h
	}
	for method, h := range prHandlers(wm, gitSvc, gh, reg) {
		hs[method] = h
	}
	for _, method := range []string{"task.commit", "task.stage", "task.setBase"} {
		hs[method] = invalidateAfter(hs[method], gitSvc)
	}
	// task.retrySetup carries the task as "id" and may clear untrusted_source,
	// which TaskGit.untrusted mirrors (ADR-0039 section 7).
	retrySetup := hs["task.retrySetup"]
	hs["task.retrySetup"] = func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		res, err := retrySetup(ctx, rc, raw)
		if err == nil {
			var p struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(raw, &p) == nil && p.ID != 0 {
				gitSvc.Invalidate(p.ID)
			}
		}
		return res, err
	}
	hs["provider.setDefaultModel"] = handleProviderSetDefaultModel(runner, bus)
	hs["session.identify"] = handleSessionIdentify(reg)
	admit := newSeatAdmission(reg, profReg, db)
	hs["profile.list"] = admit.wrapProfileList(hs["profile.list"])
	hs["daemon.info"] = handleDaemonInfo(info, reg)
	hs["run.attachment"] = handleRunAttachment(reg)
	hs["forge.search"] = handleForgeSearch(wm, gh)
	hs["forge.status"] = handleForgeStatus(fauth)
	hs["forge.setToken"] = handleForgeSetToken(fauth)
	hs["forge.logout"] = handleForgeLogout(fauth)
	learning := newLearningHost(db, bus)
	for method, h := range learningHandlers(learning) {
		hs[method] = h
	}
	hs["workspace.delete"] = learning.beforeWorkspaceDelete(hs["workspace.delete"])
	api := &API{Runs: reg, Terminals: treg, hs: hs, admit: admit, bus: bus, info: info, learning: learning, github: gh, forgeAuth: fauth, git: gitSvc, focus: focus, prFocus: prFocus, conns: map[*websocket.Conn]struct{}{}}
	api.authorize = func(r *http.Request) bool {
		return subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(token)) == 1
	}
	api.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !api.authorize(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		api.connsMu.Lock()
		if api.closing {
			api.connsMu.Unlock()
			sendGoingAway(ws)
			return
		}
		api.conns[ws] = struct{}{}
		api.connsMu.Unlock()
		defer func() {
			api.connsMu.Lock()
			delete(api.conns, ws)
			api.connsMu.Unlock()
		}()
		// Gorilla answers an oversized message with close code 1009.
		ws.SetReadLimit(DirectMaxMessage)

		api.serveLocal(r.Context(), wsTransport{ws}, ChannelOf(r))
	})
	return api, nil
}

// sendGoingAway writes a 1001 close frame; errors are ignored (peer may be gone).
func sendGoingAway(ws *websocket.Conn) {
	_ = ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, "daemon shutting down"),
		time.Now().Add(time.Second))
}

// CloseConnections closes every live /ws connection with a 1001 (going away)
// close frame and makes later upgrades close immediately the same way. The
// daemon calls it first on shutdown so clients see the stop at once instead of
// after run teardown. Safe to call more than once.
func (a *API) CloseConnections() {
	a.connsMu.Lock()
	a.closing = true
	snapshot := make([]*websocket.Conn, 0, len(a.conns))
	for ws := range a.conns {
		snapshot = append(snapshot, ws)
	}
	a.connsMu.Unlock()
	for _, ws := range snapshot {
		sendGoingAway(ws)
		_ = ws.Close()
	}
}

// busWorkspaceNotifier adapts the shared event bus to workspace.Notifier,
// translating each Manager lifecycle notification into its ADR-0005/0009
// wire payload.
type busWorkspaceNotifier struct {
	bus *eventBus
	db  *store.Store
	git *gitstate.Service
}

// isSystemTask reports whether taskID is a hidden system task (ADR-0026);
// notifications about one are dropped. A lookup failure (e.g. the row was
// just deleted) counts as not-system so a user task's event is never lost.
func isSystemTask(db *store.Store, taskID int64) bool {
	if db == nil {
		return false
	}
	t, err := db.GetTask(taskID)
	return err == nil && t.Kind == store.TaskKindSystem
}

func (b busWorkspaceNotifier) NotifyTaskStatus(taskID int64, status string) {
	if isSystemTask(b.db, taskID) {
		return
	}
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
	if t.Kind == store.TaskKindSystem {
		return
	}
	b.bus.Publish(Event{Topic: TopicTaskCreated, Payload: taskCreatedPayload{Task: t}})
}

func (b busWorkspaceNotifier) NotifyTaskUpdated(t store.Task) {
	if t.Kind == store.TaskKindSystem {
		return
	}
	b.bus.Publish(Event{Topic: TopicTaskUpdated, Payload: taskUpdatedPayload{Task: t}})
}

func (b busWorkspaceNotifier) NotifyTaskArchived(t store.Task) {
	b.git.Drop(t.ID)
	if t.Kind == store.TaskKindSystem {
		return
	}
	b.bus.Publish(Event{Topic: TopicTaskArchived, Payload: taskArchivedPayload{Task: t}})
}

func (b busWorkspaceNotifier) NotifyTaskDeleted(id, workspaceID int64, spaceID *int64) {
	b.git.Drop(id)
	if isSystemTask(b.db, id) {
		return
	}
	b.bus.Publish(Event{Topic: TopicTaskDeleted, Payload: taskDeletedPayload{ID: id, WorkspaceID: workspaceID, SpaceID: spaceID}})
}

func (b busWorkspaceNotifier) NotifyChatCreated(c store.Chat) {
	if isSystemTask(b.db, c.TaskID) {
		return
	}
	b.bus.Publish(Event{Topic: TopicChatCreated, Payload: chatCreatedPayload{Chat: c}})
}

func (b busWorkspaceNotifier) NotifyChatUpdated(c store.Chat) {
	if isSystemTask(b.db, c.TaskID) {
		return
	}
	b.bus.Publish(Event{Topic: TopicChatUpdated, Payload: chatUpdatedPayload{Chat: c}})
}

func (b busWorkspaceNotifier) NotifyChatArchived(c store.Chat) {
	if isSystemTask(b.db, c.TaskID) {
		return
	}
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
		HasIdentity: a.Identity != "",
		CreatedAt:   a.CreatedAt.Format(time.RFC3339), UpdatedAt: a.UpdatedAt.Format(time.RFC3339),
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
	db  *store.Store
	git *gitstate.Service
}

func (b busRunNotifier) NotifyRunStatus(s runs.RunStatus) {
	if s.Purpose != "" {
		return
	}
	// A run starting blocks Create PR (run_active); one ending may have
	// changed the branch.
	b.git.Invalidate(s.TaskID)
	b.bus.Publish(Event{Topic: TopicRunStatus, Payload: runStatusPayload{
		RunID: s.ID, TaskID: s.TaskID, ChatID: s.ChatID, Status: string(s.Status),
		StopReason: s.StopReason, Disposition: s.Disposition, Err: s.Err,
		FailureKind: s.FailureKind, Window: s.FailureWindow, ResetAt: runs.FailureResetRFC3339(s),
		BaseSHA: derefStr(s.BaseSHA), HeadSHA: derefStr(s.HeadSHA), Dirty: s.Dirty,
		ProfileID: s.ProfileID, FailoverOf: s.FailoverOf,
	}})
}

func (b busRunNotifier) NotifyRunFailover(fromRunID, toRunID string, taskID, toChatID int64) {
	if isSystemTask(b.db, taskID) {
		return
	}
	b.bus.Publish(Event{Topic: TopicRunFailover, Payload: runFailoverPayload{
		FromRunID: fromRunID, ToRunID: toRunID, TaskID: taskID, ToChatID: toChatID,
	}})
}

func (b busRunNotifier) NotifyPermissionPending(runID string, taskID, chatID int64, requestID, summary string, options []taskrunner.PermissionOption) {
	if isSystemTask(b.db, taskID) {
		return
	}
	b.bus.Publish(Event{Topic: TopicPermissionPending, Payload: permissionPendingPayload{
		RunID: runID, TaskID: taskID, ChatID: chatID, RequestID: requestID, Summary: summary,
		Options: toPermissionOptionParams(options),
	}})
}

func (b busRunNotifier) NotifyPermissionResolved(runID string, taskID, chatID int64, requestID, optionID, reason string) {
	if isSystemTask(b.db, taskID) {
		return
	}
	b.bus.Publish(Event{Topic: TopicPermissionResolved, Payload: permissionResolvedPayload{
		RunID: runID, TaskID: taskID, ChatID: chatID, RequestID: requestID, OptionID: optionID, Reason: reason,
	}})
}

func (b busRunNotifier) NotifyChatQueueUpdated(chatID int64, items []store.ChatQueueItem) {
	if b.db != nil {
		if c, err := b.db.GetChat(chatID); err == nil && isSystemTask(b.db, c.TaskID) {
			return
		}
	}
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
