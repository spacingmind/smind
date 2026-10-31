package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacingmind/smind/internal/inbox"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/tasktree"
	"github.com/spacingmind/smind/internal/validation"
)

// The inbox.* RPCs (ADR-0045 §5): one derived list of what needs a person.
// Reads are open to every connection that may call them, relay sessions
// included. The three setters (seen, snooze, resolve) change only the Human's
// own display state, so they are allowed over the relay too, but never for a
// run-scoped agent (and so never for a task an automation started). There is
// no method that answers a permission, approves anything or merges: acting on
// a permission from the inbox is the existing run.respondPermission, with the
// same params and the same audit.

// TopicInboxChanged is published (empty payload, coalesced) when what an
// inbox client would render changed: a permission appeared or left, a verdict
// landed or went stale, a stall began or ended, or a state setter ran. Clients
// refetch inbox.count and, if open, inbox.list.
const TopicInboxChanged = "inbox.changed"

// inboxPollPeriod is how often the host checks the inbox's fingerprint while
// somebody is subscribed to inbox.changed; inboxDebounce coalesces pokes.
const (
	inboxPollPeriod = 2 * time.Second
	inboxDebounce   = 250 * time.Millisecond
)

// inboxMethods lists every inbox RPC; a test walks it. inboxSetters are the
// ones that change state.
var (
	inboxMethods = []string{"inbox.list", "inbox.count", "inbox.seen", "inbox.snooze", "inbox.resolve"}
	inboxSetters = []string{"inbox.seen", "inbox.snooze", "inbox.resolve"}
)

type inboxHost struct {
	svc *inbox.Service
	// linkIssues is the fleet link-issue source SetFleet installs after the
	// inbox loop is already running, hence atomic.
	linkIssues atomic.Pointer[func() []inbox.HostLinkIssue]
	bus        *eventBus
	poke       chan struct{}
	done       chan struct{}
	once       sync.Once

	mu   sync.Mutex
	last string
}

func newInboxHost(db *store.Store, bus *eventBus, reg *runs.Registry, vals *validation.Service, stalled *stalledSource, hostPairs func() []inbox.HostPair) *inboxHost {
	h := &inboxHost{bus: bus, poke: make(chan struct{}, 1), done: make(chan struct{})}
	h.svc = &inbox.Service{
		DB:          db,
		Permissions: reg.PendingPermissions,
		Validations: func() ([]validation.View, error) {
			rows, err := db.ListLatestValidations()
			if err != nil {
				return nil, err
			}
			out := make([]validation.View, 0, len(rows))
			for _, r := range rows {
				if v, err := vals.View(r.ID); err == nil {
					out = append(out, v)
				}
			}
			return out, nil
		},
		FailedRuns: func() []inbox.FailedRun {
			var out []inbox.FailedRun
			for taskID, f := range reg.TaskRunFacts() {
				if f.State != tasktree.StateError || f.FailureKind == "" || f.FailureRunID == "" {
					continue
				}
				if t, err := db.GetTask(taskID); err != nil || t.ArchivedAt != nil {
					continue
				}
				fr := inbox.FailedRun{RunID: f.FailureRunID, TaskID: taskID, Kind: f.FailureKind, Action: f.FailureAction}
				if f.FailureFinishedAt != nil {
					fr.FinishedAt = f.FailureFinishedAt.UTC()
				}
				out = append(out, fr)
			}
			return out
		},
		Stalls:    stalled.current,
		HostPairs: hostPairs,
		HostLinkIssues: func() []inbox.HostLinkIssue {
			if f := h.linkIssues.Load(); f != nil {
				return (*f)()
			}
			return nil
		},
		TaskTitle: func(id int64) string {
			if t, err := db.GetTask(id); err == nil {
				return t.Title
			}
			return ""
		},
	}
	go h.loop()
	return h
}

// setLinkIssues installs the source of host link issues.
func (h *inboxHost) setLinkIssues(f func() []inbox.HostLinkIssue) {
	h.linkIssues.Store(&f)
}

func (h *inboxHost) stop() {
	if h == nil {
		return
	}
	h.once.Do(func() { close(h.done) })
}

// Poke asks the host to re-check soon (a setter ran).
func (h *inboxHost) Poke() {
	select {
	case h.poke <- struct{}{}:
	default:
	}
}

func (h *inboxHost) loop() {
	t := time.NewTicker(inboxPollPeriod)
	defer t.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-t.C:
			if h.bus.HasSubscribers(TopicInboxChanged) {
				h.check()
			}
		case <-h.poke:
			select {
			case <-h.done:
				return
			case <-time.After(inboxDebounce):
			}
			h.check()
		}
	}
}

// check publishes inbox.changed when the fingerprint moved.
func (h *inboxHost) check() {
	fp, err := h.svc.Fingerprint()
	if err != nil {
		return
	}
	h.mu.Lock()
	changed := fp != h.last
	h.last = fp
	h.mu.Unlock()
	if changed {
		h.bus.Publish(Event{Topic: TopicInboxChanged, Payload: struct{}{}})
	}
}

func inboxActor(rc *requestContext) string {
	if rc != nil && rc.deviceID != "" {
		return "device:" + rc.deviceID
	}
	if rc != nil && !rc.local {
		return "relay"
	}
	return "local"
}

func inboxErr(method string, err error) error {
	if errors.Is(err, inbox.ErrNotAllowed) {
		return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	}
	return err
}

func inboxHandlers(h *inboxHost) map[string]handlerFunc {
	hs := map[string]handlerFunc{}

	hs["inbox.list"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "inbox.list"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			Kinds  []string `json:"kinds"`
			States []string `json:"states"`
			Limit  int      `json:"limit"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		items, err := h.svc.List(inbox.Filter{Kinds: p.Kinds, States: p.States, Limit: p.Limit})
		return map[string]any{"items": items}, err
	}

	hs["inbox.count"] = func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if err := refuseAgent("inbox.count", rc); err != nil {
			return nil, err
		}
		return h.svc.Count()
	}

	setter := func(m string, apply func(p inboxSetParams, actor string) error) handlerFunc {
		return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
			if err := refuseAgent(m, rc); err != nil {
				return nil, err
			}
			var p inboxSetParams
			if err := pluginParams(m, raw, &p); err != nil {
				return nil, err
			}
			if err := apply(p, inboxActor(rc)); err != nil {
				return nil, inboxErr(m, err)
			}
			h.Poke()
			return map[string]any{"ok": true}, nil
		}
	}
	hs["inbox.seen"] = setter("inbox.seen", func(p inboxSetParams, actor string) error { return h.svc.MarkSeen(p.Keys, actor) })
	hs["inbox.snooze"] = setter("inbox.snooze", func(p inboxSetParams, actor string) error {
		until, err := time.Parse(time.RFC3339, p.Until)
		if err != nil {
			return errors.Join(inbox.ErrNotAllowed, errors.New("until must be an RFC 3339 time"))
		}
		return h.svc.Snooze(p.Keys, until, actor)
	})
	hs["inbox.resolve"] = setter("inbox.resolve", func(p inboxSetParams, actor string) error { return h.svc.Resolve(p.Keys, actor) })
	return hs
}

type inboxSetParams struct {
	Keys  []string `json:"keys"`
	Until string   `json:"until"`
}

// refuseAgent: the inbox is the Human's console. No inbox method is in
// RunScopedMethods, and a handler reached with a run-scoped context (a task a
// seat or an automation started) refuses too.
func refuseAgent(method string, rc *requestContext) error {
	if rc == nil || rc.runScoped {
		return forbidden(method + " is not available to agents")
	}
	return nil
}
