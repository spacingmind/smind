package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/plugins/host"
)

// The plugin-scoped session (ADR-0042 section 4, PB.2). A plugin process
// reaches the daemon through its own stdio channel; every request it sends is
// executed in-process by pluginSession.Request, never by the general wsapi
// dispatcher. The path is deny-by-default:
//
//  1. a method on the never-list (plugin.*, provider.*, account.*, permission
//     answering, ...) is refused `forbidden` even if someone adds it to the
//     table by mistake;
//  2. a method not in PluginCapabilityTable is `method_not_found`: a wsapi
//     method added tomorrow is denied until a human lists it here;
//  3. a listed method whose capability group the manifest did not declare is
//     `forbidden`, naming the missing capability;
//  4. only then does the handler run, with a run-scoped-like request context
//     (runScoped true, local false, plugin id set) so the human-only and
//     local-only guards inside handlers refuse it as well, and its result goes
//     through the redactor.
//
// This is mediation, not a sandbox (ADR-0042 section 2): a plugin process can
// still read ~/.smind/token as the same user.

// Capability groups a plugin may declare, as the manifest names them.
const (
	CapTasksRead      = "tasks.read"
	CapEventsObserve  = "events.observe"
	CapMcpRead        = "mcp.read"
	CapProfilesRead   = "profiles.read"
	pluginRPCSubscibe = "events.subscribe"
	pluginRPCUnsub    = "events.unsubscribe"
)

// PluginMethod is one method of the plugin API. Everything in the table is
// read-only (a list/get/tree verb) unless Why says otherwise. Native methods
// are answered by the plugin session itself and are not in methodHandlers.
type PluginMethod struct {
	Name   string
	Native bool
	Why    string
}

// PluginCapabilityTable is THE documented plugin API: capability group ->
// methods. Groups the manifest knows but that are not here (tasks.write,
// files.read, ui.notify, hooks.beforeRun) grant nothing yet; they arrive with
// their own PRs and their own test updates. Deliberately left out of
// tasks.read: run.logs and task.diff (transcripts and file contents),
// workspace.get (returns the setup/teardown scripts), chat.get/queue reads
// (prompt text).
var PluginCapabilityTable = map[string][]PluginMethod{
	CapTasksRead: {
		{Name: "task.list"}, {Name: "task.get"}, {Name: "task.tree"},
		{Name: "run.list"}, {Name: "chat.list"},
		{Name: "workspace.list"}, {Name: "space.list"}, {Name: "space.get"},
	},
	CapMcpRead:      {{Name: "mcp.list"}, {Name: "mcp.get"}},
	CapProfilesRead: {{Name: "profile.list"}, {Name: "profile.get"}},
	CapEventsObserve: {
		{Name: pluginRPCSubscibe, Native: true, Why: "changes only this plugin's own subscription to allowlisted, redacted topics"},
		{Name: pluginRPCUnsub, Native: true, Why: "changes only this plugin's own subscription"},
	},
}

// pluginNeverMethods and pluginNeverPrefixes are what a plugin session can
// never call, whatever the table says: administration of plugins, providers,
// accounts, relay devices, push, learning, terminals, forge sign-in, every
// write of the user's own data, and anything that answers or alters a
// permission request (ADR-0019). TestPluginSession_CapabilityTable_EveryMethodExists
// fails if the table lists one.
var pluginNeverPrefixes = []string{
	"plugin.", "provider.", "account.", "relay.", "push.", "learning.", "terminal.",
	"forge.", "automation.", "settings.", "permission.", "device.", "auth.", "fs.", "file.",
	"usage.", "daemon.",
}

var pluginNeverMethods = []string{
	"run.respondPermission", "run.setPermissionMode", "run.setConfigOption", "run.start", "run.stop",
	"run.attach", "run.attachment", "run.logs", "run.listConfigOptions",
	"session.identify", "daemon.setPassword", "set-password",
	"mcp.create", "mcp.update", "mcp.delete", "mcp.setEnabled",
	"profile.create", "profile.update", "profile.delete",
	"workspace.create", "workspace.delete", "workspace.get", "workspace.setScripts",
	"task.prompt", "task.diff", "task.fileDiff", "task.files",
}

func pluginNever(method string) bool {
	for _, m := range pluginNeverMethods {
		if m == method {
			return true
		}
	}
	for _, p := range pluginNeverPrefixes {
		if strings.HasPrefix(method, p) {
			return true
		}
	}
	return false
}

// pluginMethodCap maps each table method to its capability group.
var pluginMethodCap = func() map[string]string {
	m := map[string]string{}
	for capName, ms := range PluginCapabilityTable {
		for _, pm := range ms {
			m[pm.Name] = capName
		}
	}
	return m
}()

// PluginEventTopics is the allowlist of topics a plugin with events.observe
// may subscribe to. Left out on purpose: chat.queueUpdated (queued prompt
// text), profile.* (role text), account.*, mcpServer.* (config), provider.*,
// task.git (diffs), forge.status, learning.* and every plugin-private topic.
var PluginEventTopics = []string{
	TopicTaskStatus, TopicTaskCreated, TopicTaskUpdated, TopicTaskArchived, TopicTaskDeleted, TopicTaskStalled,
	TopicRunStatus, TopicRunFailover,
	TopicPermissionPending, TopicPermissionResolved,
	TopicWorkspaceCreated, TopicWorkspaceDeleted, TopicSpaceCreated, TopicSpaceDeleted,
	TopicChatCreated, TopicChatUpdated, TopicChatArchived,
	TopicPluginUpdated,
}

var pluginTopicSet = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range PluginEventTopics {
		m[t] = true
	}
	return m
}()

// pluginEventDropFields are removed from a payload before a plugin sees it,
// by topic. permission.pending's summary is the free-text body of a pending
// request (a command line, a file diff preview): a plugin may learn that a
// request is pending and which, never what it says. The task row's setup
// output tail can hold anything a setup script printed.
var pluginEventDropFields = map[string][]string{
	TopicPermissionPending: {"summary"},
}

// pluginAlwaysDrop are keys removed at any depth of any payload or result a
// plugin receives.
var pluginAlwaysDrop = []string{"SetupLog", "SetupError"}

// secretKeyParts: a STRING value under a key containing one of these is
// replaced outright (belt and braces: the redactor catches token shapes, this
// catches a bare value under a telling name).
var secretKeyParts = []string{"token", "secret", "password", "passwd", "apikey", "api_key", "authorization", "credential", "cookie"}

const (
	pluginCallTimeout = 25 * time.Second
	redactedValue     = "[redacted]"
	// maxPluginSubscriptions bounds one session's topic set.
	maxPluginSubscriptions = 64
)

// pluginRegistry is the host.Registry the daemon wires into the plugin host.
// It is created before the plugin manager (which needs it in its options) and
// bound to the handler map and event bus once those exist; until then it
// answers nothing.
type pluginRegistry struct {
	mu   sync.RWMutex
	hs   map[string]handlerFunc
	bus  *eventBus
	red  *redact.Redactor
	conn *conn
}

func newPluginRegistry() *pluginRegistry { return &pluginRegistry{} }

// bind installs the handler map plugin sessions execute against (a private
// copy: later changes to the daemon's map cannot widen the plugin API, and the
// table, not the map, decides what is callable) and the event bus.
func (r *pluginRegistry) bind(hs map[string]handlerFunc, bus *eventBus) {
	cp := make(map[string]handlerFunc, len(hs))
	for k, v := range hs {
		cp[k] = v
	}
	var homes []string
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		homes = append(homes, h)
	}
	r.mu.Lock()
	r.hs, r.bus = cp, bus
	r.red = redact.New(nil, homes)
	r.conn = newConn(nopTransport{}, nil)
	r.mu.Unlock()
}

// Request and Notify make pluginRegistry a host.Registry; a connection that
// did not open a session (it always does) is refused.
func (r *pluginRegistry) Request(_ context.Context, _, method string, _ json.RawMessage) (any, *host.RPCError) {
	return nil, &host.RPCError{Code: host.ErrCodeMethodNotFound, Message: "method " + clipMethod(method) + " is not available to plugins"}
}
func (r *pluginRegistry) Notify(string, string, json.RawMessage) {}

// Open starts a plugin process's session.
func (r *pluginRegistry) Open(p host.Plugin, push host.PushFunc) host.Session {
	caps := map[string]bool{}
	for _, c := range p.Capabilities {
		caps[c] = true
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &pluginSession{reg: r, id: p.ID, caps: caps, push: push, ctx: ctx, cancel: cancel}
}

type nopTransport struct{}

func (nopTransport) Receive() ([]byte, error) { return nil, errors.New("no transport") }
func (nopTransport) Send([]byte) error        { return nil }
func (nopTransport) Close() error             { return nil }

func clipMethod(s string) string {
	if len(s) > 64 {
		return s[:64] + "..."
	}
	return s
}

// pluginSession is one plugin process's session.
type pluginSession struct {
	reg  *pluginRegistry
	id   string
	caps map[string]bool
	push host.PushFunc

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	sub    *subscriber
	closed bool
}

func (s *pluginSession) Notify(string, json.RawMessage) {}

// Close ends the session: its subscription is removed and its pump stops.
func (s *pluginSession) Close() {
	s.mu.Lock()
	s.closed = true
	sub := s.sub
	s.mu.Unlock()
	s.cancel()
	if sub != nil {
		s.reg.bus.unregister(sub)
		sub.close()
	}
}

func rpcErr(code int, msg string, data any) *host.RPCError {
	return &host.RPCError{Code: code, Message: msg, Data: data}
}

// Request resolves one plugin-to-host call. See the file comment for the order.
func (s *pluginSession) Request(ctx context.Context, method string, params json.RawMessage) (any, *host.RPCError) {
	if pluginNever(method) {
		return nil, rpcErr(host.ErrCodeForbidden, "method "+clipMethod(method)+" is never available to plugins", nil)
	}
	capName, ok := pluginMethodCap[method]
	if !ok {
		return nil, rpcErr(host.ErrCodeMethodNotFound, "method "+clipMethod(method)+" is not part of the plugin API", nil)
	}
	if !s.caps[capName] {
		return nil, rpcErr(host.ErrCodeForbidden, "method "+method+" needs the "+capName+" capability, which this plugin did not declare", map[string]string{"capability": capName})
	}
	switch method {
	case pluginRPCSubscibe, pluginRPCUnsub:
		return s.subscription(method, params)
	}
	s.reg.mu.RLock()
	h, red, c := s.reg.hs[method], s.reg.red, s.reg.conn
	s.reg.mu.RUnlock()
	if h == nil || red == nil {
		return nil, rpcErr(host.ErrCodeMethodNotFound, "method "+method+" is not available", nil)
	}
	rc := &requestContext{conn: c, id: "plugin:" + s.id, runScoped: true, local: false, channel: ChannelPlain}
	ctx, cancel := context.WithTimeout(ctx, pluginCallTimeout)
	defer cancel()
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("{}")
	}
	res, err := h(ctx, rc, params)
	if err != nil {
		code, msg := codedErrorParts(err)
		return nil, rpcErr(rpcCodeFor(code), red.String(msg), nil)
	}
	return redactForPlugin(red, res, nil), nil
}

// codedErrorParts and rpcCodeFor map a handler error onto a JSON-RPC code.
func codedErrorParts(err error) (code, msg string) {
	e := codedError(err)
	if e == nil {
		return "", err.Error()
	}
	return e.Code, e.Message
}

func rpcCodeFor(code string) int {
	switch code {
	case codeForbidden:
		return host.ErrCodeForbidden
	case codeInvalid:
		return host.ErrCodeInvalidParams
	}
	return host.ErrCodeInternal
}

// subscription implements events.subscribe / events.unsubscribe for this
// session. Only allowlisted topics are accepted, in any case.
func (s *pluginSession) subscription(method string, params json.RawMessage) (any, *host.RPCError) {
	var p struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal(params, &p); err != nil || len(p.Topics) == 0 || len(p.Topics) > maxPluginSubscriptions {
		return nil, rpcErr(host.ErrCodeInvalidParams, method+": topics must be a non-empty list of topic names", nil)
	}
	for _, t := range p.Topics {
		if !pluginTopicSet[t] {
			return nil, rpcErr(host.ErrCodeInvalidParams, method+": topic "+clipMethod(t)+" is not observable by plugins (allowed: "+strings.Join(PluginEventTopics, ", ")+")", nil)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, rpcErr(host.ErrCodeInternal, "the session has ended", nil)
	}
	if s.sub == nil {
		if method == pluginRPCUnsub {
			return map[string]any{"topics": []string{}}, nil
		}
		s.sub = newSubscriber()
		s.reg.bus.register(s.sub)
		go s.pump(s.sub)
	}
	if method == pluginRPCSubscibe {
		return subscribeResult{Topics: s.sub.subscribe(p.Topics)}, nil
	}
	return subscribeResult{Topics: s.sub.unsubscribe(p.Topics)}, nil
}

// pump delivers this session's queued events to the plugin as `event`
// notifications ({topic, seq, payload}, seq monotonic per session) and
// `event.dropped` ({count}) after an overflow. The bus never waits for it: a
// plugin that stops reading blocks only this goroutine (the host's write
// timeout then ends the connection), while offers keep dropping its oldest.
func (s *pluginSession) pump(sub *subscriber) {
	var seq int64
	s.reg.mu.RLock()
	red := s.reg.red
	s.reg.mu.RUnlock()
	for {
		e, dropped, ok := sub.next()
		if dropped > 0 {
			seq++
			if s.push("event.dropped", eventDroppedPayload{Count: dropped}) != nil {
				return
			}
		}
		if !ok {
			if !sub.wait(s.ctx) {
				return
			}
			continue
		}
		seq++
		payload := redactForPlugin(red, e.Payload, pluginEventDropFields[e.Topic])
		if s.push("event", eventNotification{Topic: e.Topic, Seq: seq, Payload: payload}) != nil {
			return
		}
	}
}

// redactForPlugin returns a redacted COPY of v: it is marshalled and decoded
// into plain JSON values (so no live object, pointer or shared slice leaves
// the daemon), the drop fields and the always-drop keys are removed at any
// depth, a string under a secret-looking key is replaced, and every string
// goes through the redactor (token shapes, home paths). A value that cannot be
// marshalled becomes null.
func redactForPlugin(red *redact.Redactor, v any, drop []string) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil
	}
	dropSet := map[string]bool{}
	for _, k := range drop {
		dropSet[k] = true
	}
	for _, k := range pluginAlwaysDrop {
		dropSet[k] = true
	}
	return scrub(red, generic, dropSet)
}

func scrub(red *redact.Redactor, v any, drop map[string]bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if drop[k] {
				continue
			}
			if str, ok := t[k].(string); ok && secretKey(k) && str != "" {
				out[k] = redactedValue
				continue
			}
			if str, ok := t[k].(string); ok && isHexID(str) {
				out[k] = str // a commit or content hash is an id, not a secret (the redactor would blank it)
				continue
			}
			out[k] = scrub(red, t[k], drop)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = scrub(red, t[i], drop)
		}
		return out
	case string:
		return red.String(t)
	}
	return v
}

func secretKey(k string) bool {
	l := strings.ToLower(k)
	for _, p := range secretKeyParts {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// isHexID reports a whole string that is a 40 (SHA-1) or 64 (SHA-256) digit
// lowercase hex id.
func isHexID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
