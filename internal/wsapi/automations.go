package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/automation"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
)

// The automation.* RPCs (ADR-0044). Reads (list, get, audit, memory.get) are
// open to every connection that may call them, relay sessions included. A
// relay session may also DISABLE (a brake, never an accelerator). Everything
// else that creates, changes, arms or fires is HUMAN + LOCAL only: refused as
// "forbidden" for a run-scoped (agent) caller -- which covers a task an
// automation started, since its connection is run-scoped too -- and for a
// relay session; a non-loopback direct connection needs
// automations.allowRemoteManage. None of these methods is in RunScopedMethods,
// and the MCP server exposes no automation tool.
//
// The global switches (automations.enabled / paused) have no RPC at all.

// automationMutating lists the methods the human-local guard wraps; a test
// walks it.
var automationMutating = []string{
	"automation.create", "automation.update", "automation.delete", "automation.enable",
	"automation.resume", "automation.fire", "automation.memory.clear",
}

// automationBrake lists the methods a relay session may call (not an agent).
var automationBrake = []string{"automation.disable"}

// automationReads lists the read-only methods.
var automationReads = []string{"automation.list", "automation.get", "automation.audit", "automation.memory.get"}

// Topics (ADR-0044 section 9). Payloads carry ids and short reasons only,
// never signal text.
const (
	TopicAutomationFired   = "automation.fired"
	TopicAutomationPaused  = "automation.paused"
	TopicAutomationUpdated = "automation.updated"
)

// configAutomations reads config.yaml on every call, like the plugin switch;
// an unreadable or invalid file is OFF. Tests replace it.
var configAutomations = func() config.AutomationsConfig {
	cfg, err := config.Load()
	if err != nil {
		return config.AutomationsConfig{MaxBudgetUSD: config.DefaultMaxAutomationBudgetUSD}
	}
	return cfg.Automations
}

func automationLimits() automation.Limits {
	c := configAutomations()
	return automation.Limits{MaxBudgetUSD: c.MaxBudgetUSD, AllowUnattendedModes: c.AllowUnattendedModes}
}

func automationHumanLocal(method string, h handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		switch {
		case rc == nil || rc.runScoped:
			return nil, forbidden(method + " is not available to agents")
		case !rc.local:
			return nil, forbidden(method + " is only available on a direct connection, not over the relay")
		case rc.channel != ChannelLoopback && !configAutomations().AllowRemoteManage:
			return nil, forbidden(method + " needs a local connection: set automations.allowRemoteManage: true in config.yaml to manage automations from a remote connection")
		}
		return h(ctx, rc, raw)
	}
}

func automationNoAgents(method string, h handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc == nil || rc.runScoped {
			return nil, forbidden(method + " is not available to agents")
		}
		return h(ctx, rc, raw)
	}
}

// automationHost holds what the handlers need; the engine is bound late
// (cmd/smind builds it from the runner and registry the API owns).
type automationHost struct {
	db  *store.Store
	bus *eventBus
	mem automation.Memory

	mu  sync.RWMutex
	eng *automation.Engine
}

func newAutomationHost(db *store.Store, bus *eventBus, home string) *automationHost {
	return &automationHost{db: db, bus: bus, mem: automation.Memory{Home: home}}
}

func (h *automationHost) engine() *automation.Engine {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.eng
}

// SetAutomations binds the engine so automation.fire can use it.
func (a *API) SetAutomations(e *automation.Engine) {
	a.automations.mu.Lock()
	a.automations.eng = e
	a.automations.mu.Unlock()
}

// AutomationSink adapts the event bus to the engine's Sink.
func (a *API) AutomationSink() automation.Sink { return automationSink{h: a.automations} }

type automationSink struct{ h *automationHost }

type automationFiredPayload struct {
	AutomationID int64  `json:"automationId"`
	AuditID      int64  `json:"auditId"`
	TaskID       *int64 `json:"taskId,omitempty"`
	Decision     string `json:"decision"`
	Fingerprint  string `json:"fingerprint"`
}

type automationPausedPayload struct {
	AutomationID int64  `json:"automationId"`
	Reason       string `json:"reason"`
	// TaskID is the failed run's task that tripped the pause (0 = none); the
	// push uses it so the phone can open that task.
	TaskID int64 `json:"taskId,omitempty"`
}

type automationUpdatedPayload struct {
	AutomationID int64 `json:"automationId"`
}

func (s automationSink) Fired(a store.Automation, r store.AutomationAudit) {
	s.h.bus.Publish(Event{Topic: TopicAutomationFired, Payload: automationFiredPayload{
		AutomationID: a.ID, AuditID: r.ID, TaskID: r.TaskID, Decision: r.Decision, Fingerprint: r.Fingerprint}})
}

func (s automationSink) Paused(a store.Automation, reason string, taskID int64) {
	s.h.bus.Publish(Event{Topic: TopicAutomationPaused, Payload: automationPausedPayload{AutomationID: a.ID, Reason: reason, TaskID: taskID}})
}

func (s automationSink) Updated(a store.Automation) {
	s.h.bus.Publish(Event{Topic: TopicAutomationUpdated, Payload: automationUpdatedPayload{AutomationID: a.ID}})
}

func (h *automationHost) updated(id int64) {
	h.bus.Publish(Event{Topic: TopicAutomationUpdated, Payload: automationUpdatedPayload{AutomationID: id}})
}

// automationView is the wire shape of one automation.
type automationView struct {
	ID                  int64                 `json:"id"`
	Name                string                `json:"name"`
	Enabled             bool                  `json:"enabled"`
	WorkspaceID         int64                 `json:"workspaceId"`
	Signal              automation.SignalSpec `json:"signal"`
	Template            automation.Template   `json:"template"`
	Rails               automation.Rails      `json:"rails"`
	Status              string                `json:"status"`
	PausedReason        string                `json:"pausedReason"`
	PausedAt            *string               `json:"pausedAt"`
	ConsecutiveFailures int                   `json:"consecutiveFailures"`
	BudgetBlocked       bool                  `json:"budgetBlocked"`
	LastFiredAt         *string               `json:"lastFiredAt"`
	NextRunAt           *string               `json:"nextRunAt"`
	NextRuns            []string              `json:"nextRuns,omitempty"`
	MemoryEnabled       bool                  `json:"memoryEnabled"`
	CreatedAt           string                `json:"createdAt"`
	UpdatedAt           string                `json:"updatedAt"`
}

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func viewAutomation(a store.Automation) (automationView, error) {
	d, err := automation.DefOf(a)
	if err != nil {
		return automationView{}, err
	}
	d = d.WithDefaults()
	v := automationView{
		ID: a.ID, Name: a.Name, Enabled: a.Enabled, WorkspaceID: a.WorkspaceID, Signal: d.Signal, Template: d.Template, Rails: d.Rails,
		Status: a.Status, PausedReason: a.PausedReason, PausedAt: tsPtr(a.PausedAt), ConsecutiveFailures: a.ConsecutiveFailures,
		BudgetBlocked: a.BudgetBlocked, LastFiredAt: tsPtr(a.LastFiredAt), NextRunAt: tsPtr(a.NextRunAt), MemoryEnabled: d.Rails.MemoryEnabled,
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: a.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if d.Signal.Kind == automation.KindCron {
		if c, err := automation.ParseCron(d.Signal.Cron); err == nil {
			t := time.Now().UTC()
			for i := 0; i < 3; i++ {
				next, ok := c.Next(t)
				if !ok {
					break
				}
				v.NextRuns = append(v.NextRuns, next.Format(time.RFC3339))
				t = next
			}
		}
	}
	return v, nil
}

type automationAuditView struct {
	ID          int64   `json:"id"`
	SignalKind  string  `json:"signalKind"`
	Fingerprint string  `json:"fingerprint"`
	Decision    string  `json:"decision"`
	Reason      string  `json:"reason"`
	Manual      bool    `json:"manual"`
	Catchup     bool    `json:"catchup"`
	TaskID      *int64  `json:"taskId"`
	RunID       string  `json:"runId"`
	Outcome     string  `json:"outcome"`
	Note        string  `json:"note"`
	CreatedAt   string  `json:"createdAt"`
	FinishedAt  *string `json:"finishedAt"`
}

func viewAudit(r store.AutomationAudit) automationAuditView {
	return automationAuditView{ID: r.ID, SignalKind: r.SignalKind, Fingerprint: r.Fingerprint, Decision: r.Decision, Reason: r.Reason,
		Manual: r.Manual, Catchup: r.Catchup, TaskID: r.TaskID, RunID: r.RunID, Outcome: r.Outcome, Note: r.Note,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339), FinishedAt: tsPtr(r.FinishedAt)}
}

func automationErr(method string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrAutomationNotFound):
		return &rpcCodeError{code: codeNotFound, msg: method + ": automation not found", err: err}
	case errors.Is(err, store.ErrAutomationExists):
		return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	}
	return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
}

type automationIDParams struct {
	ID int64 `json:"id"`
}

func parseID(method string, raw json.RawMessage) (int64, error) {
	var p automationIDParams
	if err := json.Unmarshal(raw, &p); err != nil || p.ID <= 0 {
		return 0, &rpcCodeError{code: codeInvalid, msg: method + ": id is required"}
	}
	return p.ID, nil
}

func automationHandlers(h *automationHost) map[string]handlerFunc {
	hs := map[string]handlerFunc{}
	hs["automation.list"] = func(context.Context, *requestContext, json.RawMessage) (any, error) {
		rows, err := h.db.ListAutomations()
		if err != nil {
			return nil, fmt.Errorf("automation.list: %w", err)
		}
		cfg := configAutomations()
		out := struct {
			Enabled     bool             `json:"enabled"`
			Paused      bool             `json:"paused"`
			Automations []automationView `json:"automations"`
		}{Enabled: cfg.Enabled, Paused: cfg.Paused, Automations: []automationView{}}
		for _, a := range rows {
			v, err := viewAutomation(a)
			if err != nil {
				continue
			}
			out.Automations = append(out.Automations, v)
		}
		return out, nil
	}
	hs["automation.get"] = func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		id, err := parseID("automation.get", raw)
		if err != nil {
			return nil, err
		}
		a, err := h.db.GetAutomation(id)
		if err != nil {
			return nil, automationErr("automation.get", err)
		}
		return viewAutomation(a)
	}
	hs["automation.audit"] = func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID     int64 `json:"id"`
			Limit  int   `json:"limit"`
			Before int64 `json:"before"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.ID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: "automation.audit: id is required"}
		}
		if _, err := h.db.GetAutomation(p.ID); err != nil {
			return nil, automationErr("automation.audit", err)
		}
		rows, err := h.db.ListAutomationAudit(p.ID, p.Limit, p.Before)
		if err != nil {
			return nil, fmt.Errorf("automation.audit: %w", err)
		}
		out := make([]automationAuditView, len(rows))
		for i, r := range rows {
			out[i] = viewAudit(r)
		}
		return map[string]any{"rows": out}, nil
	}
	hs["automation.memory.get"] = func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		id, err := parseID("automation.memory.get", raw)
		if err != nil {
			return nil, err
		}
		if _, err := h.db.GetAutomation(id); err != nil {
			return nil, automationErr("automation.memory.get", err)
		}
		text, err := h.mem.Read(id, 64<<10)
		if err != nil {
			return nil, fmt.Errorf("automation.memory.get: %w", err)
		}
		return map[string]any{"memory": text}, nil
	}

	hs["automation.create"] = automationHumanLocal("automation.create", func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var d automation.Def
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, &rpcCodeError{code: codeInvalid, msg: "automation.create: invalid params: " + err.Error()}
		}
		a, err := automation.Create(h.db, d, automationLimits())
		if err != nil {
			return nil, automationErr("automation.create", err)
		}
		h.updated(a.ID)
		return viewAutomation(a)
	})
	hs["automation.update"] = automationHumanLocal("automation.update", func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
			automation.Def
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.ID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: "automation.update: id and the definition are required"}
		}
		a, err := automation.Update(h.db, p.ID, p.Def, automationLimits())
		if err != nil {
			return nil, automationErr("automation.update", err)
		}
		h.updated(a.ID)
		return viewAutomation(a)
	})
	hs["automation.delete"] = automationHumanLocal("automation.delete", func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		id, err := parseID("automation.delete", raw)
		if err != nil {
			return nil, err
		}
		if err := h.db.DeleteAutomation(id); err != nil {
			return nil, automationErr("automation.delete", err)
		}
		h.updated(id)
		return map[string]any{}, nil
	})
	setEnabled := func(method string, on bool) handlerFunc {
		return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
			id, err := parseID(method, raw)
			if err != nil {
				return nil, err
			}
			if err := h.db.SetAutomationEnabled(id, on); err != nil {
				return nil, automationErr(method, err)
			}
			h.updated(id)
			a, err := h.db.GetAutomation(id)
			if err != nil {
				return nil, automationErr(method, err)
			}
			return viewAutomation(a)
		}
	}
	hs["automation.enable"] = automationHumanLocal("automation.enable", setEnabled("automation.enable", true))
	hs["automation.disable"] = automationNoAgents("automation.disable", setEnabled("automation.disable", false))
	hs["automation.resume"] = automationHumanLocal("automation.resume", func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		id, err := parseID("automation.resume", raw)
		if err != nil {
			return nil, err
		}
		if err := h.db.ResumeAutomation(id, time.Now()); err != nil {
			return nil, automationErr("automation.resume", err)
		}
		h.updated(id)
		a, _ := h.db.GetAutomation(id)
		return viewAutomation(a)
	})
	hs["automation.fire"] = automationHumanLocal("automation.fire", func(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		id, err := parseID("automation.fire", raw)
		if err != nil {
			return nil, err
		}
		eng := h.engine()
		if eng == nil {
			return nil, &rpcCodeError{code: codeUnavailable, msg: "automation.fire: the automation engine is not running"}
		}
		row, err := eng.Fire(ctx, id)
		if err != nil {
			return nil, automationErr("automation.fire", err)
		}
		return viewAudit(row), nil
	})
	hs["automation.memory.clear"] = automationHumanLocal("automation.memory.clear", func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		id, err := parseID("automation.memory.clear", raw)
		if err != nil {
			return nil, err
		}
		if _, err := h.db.GetAutomation(id); err != nil {
			return nil, automationErr("automation.memory.clear", err)
		}
		if err := h.mem.Clear(id); err != nil {
			return nil, fmt.Errorf("automation.memory.clear: %w", err)
		}
		return map[string]any{}, nil
	})
	return hs
}
