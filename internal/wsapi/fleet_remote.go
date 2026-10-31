package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

// The local read bridge (ADR-0048 F2b): remote.hosts and remote.read let the
// local CLI and the MCP server read a peer host through this daemon's live
// fleet link. They exist only when fleet is on, are refused over the relay
// and for a run that a peer created (hop limit 1), and expose no grant, key,
// address or label.

// FleetRemoteOptions wires the remote.* bridge to the live links.
type FleetRemoteOptions struct {
	// Conn returns the live connection to a host and the link state; the
	// Caller is nil unless connected.
	Conn func(hostID string) (client.Caller, client.LinkState)
	// RunOrigin returns the host a run or its task arrived from ("" = local).
	// An error means the origin cannot be verified and the call is refused.
	RunOrigin func(runID string, taskID int64) (string, error)
	// SeatMayDelegate reports whether the seat that owns a run may act
	// through other hosts (nil = every local seat may). An error means the
	// seat cannot be verified and the call is refused.
	SeatMayDelegate func(runID string) (bool, error)
	// PutWatch persists a remote-task mirror and its watch (tests; nil uses
	// the fleet store).
	PutWatch func(t store.FleetRemoteTask, w *store.FleetWatch) (int64, error)
	// Watch hands a newly opened watch id to the watcher; nil in tests.
	Watch   interface{ Add(id int64) }
	Now     func() time.Time // tests; default time.Now
	Timeout time.Duration    // tests; 0 = client default
}

// RemoteReadParams is the remote.read ask.
type RemoteReadParams struct {
	Host        string `json:"host"`
	Op          string `json:"op"`
	WorkspaceID int64  `json:"workspaceId"`
	Cursor      int64  `json:"cursor"`
	Limit       int    `json:"limit"`
	ID          int64  `json:"id"`
	TaskID      int64  `json:"taskId"`
	RunID       string `json:"runId"`
}

// RemoteReadResult wraps the wire result with the host it came from.
type RemoteReadResult struct {
	Host          string `json:"host"`
	HostID        string `json:"hostId"`
	Op            string `json:"op"`
	Data          any    `json:"data"`
	StatusUnknown bool   `json:"statusUnknown,omitempty"`
	Failures      int    `json:"failures,omitempty"`
}

// remoteHostView is a host as remote.hosts lists it: names and states only.
type remoteHostView struct {
	Alias  string `json:"alias"`
	HostID string `json:"hostId"`
	State  string `json:"state"`
	Link   string `json:"link"`
}

type remoteHostsResult struct {
	Hosts []remoteHostView `json:"hosts"`
}

// remoteTask is a peer's task with the host it lives on and its global id.
type remoteTask struct {
	wire.Task
	Host     string `json:"host"`
	GlobalID string `json:"globalId"`
}

type remoteTaskList struct {
	Tasks      []remoteTask `json:"tasks"`
	NextCursor int64        `json:"nextCursor,omitempty"`
}

type fleetRemote struct {
	f      *fleetHost
	o      FleetRemoteOptions
	client *client.Client
}

// SetFleetRemote registers remote.hosts and remote.read. SetFleet must have
// run; like it, this is called at most once at startup. It panics otherwise,
// or when Conn or RunOrigin is nil.
func (a *API) SetFleetRemote(o FleetRemoteOptions) {
	if a.fleet == nil {
		panic("wsapi: SetFleetRemote requires SetFleet")
	}
	if _, ok := a.hs["remote.read"]; ok {
		panic("wsapi: SetFleetRemote called twice")
	}
	if o.Conn == nil || o.RunOrigin == nil {
		panic("wsapi: SetFleetRemote requires Conn and RunOrigin")
	}
	f := a.fleet
	r := &fleetRemote{f: f, o: o}
	r.client = client.New(client.Options{
		Self: f.opts.Key.HostID(),
		Resolve: func(ref string) (client.Host, error) {
			h, err := f.resolveHost("remote.read", ref)
			if err != nil {
				return client.Host{}, fmt.Errorf("%w: %s", client.ErrUnknownHost, err)
			}
			return client.Host{HostID: h.HostID, Alias: h.Alias}, nil
		},
		Conn:    o.Conn,
		Now:     o.Now,
		Timeout: o.Timeout,
	})
	a.hs["remote.hosts"] = r.guard("remote.hosts", r.handleHosts)
	a.hs["remote.read"] = r.guard("remote.read", r.handleRead)
	a.hs["remote.write"] = r.guard("remote.write", r.handleWrite)
}

// guard keeps remote.* on the local connection and away from any run a peer
// created, before the handler does anything.
func (r *fleetRemote) guard(method string, h handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if !rc.local {
			return nil, &rpcCodeError{code: codeForbidden, msg: "remote.* is not available over the relay"}
		}
		if rc.runScoped {
			origin, err := r.o.RunOrigin(rc.ident.runID, rc.ident.taskID)
			if err != nil {
				return nil, &rpcCodeError{code: wire.CodeHopLimit, msg: "cannot verify the origin of this run (hop limit 1 fails closed)", err: err}
			}
			if origin != "" {
				return nil, &rpcCodeError{code: wire.CodeHopLimit, msg: "a task created by a peer host cannot call other hosts (hop limit 1)"}
			}
		}
		if rc.runScoped && r.o.SeatMayDelegate != nil {
			may, err := r.o.SeatMayDelegate(rc.ident.runID)
			if err != nil {
				return nil, &rpcCodeError{code: wire.CodeHopLimit, msg: "cannot verify the seat of this run (fails closed)", err: err}
			}
			if !may {
				return nil, &rpcCodeError{code: wire.CodeHopLimit, msg: "this seat cannot use other hosts (hop limit 1)"}
			}
		}
		return h(ctx, rc, raw)
	}
}

func (r *fleetRemote) handleHosts(context.Context, *requestContext, json.RawMessage) (any, error) {
	hosts, err := r.f.opts.Registry.List()
	if err != nil {
		return nil, fmt.Errorf("remote.hosts: %w", err)
	}
	out := remoteHostsResult{Hosts: []remoteHostView{}}
	for _, h := range hosts {
		if h.State == registry.StateRevoked {
			continue
		}
		link, _, _ := r.f.opts.Links.LinkState(h.HostID)
		out.Hosts = append(out.Hosts, remoteHostView{Alias: h.Alias, HostID: h.HostID, State: h.State, Link: link})
	}
	return out, nil
}

func (r *fleetRemote) handleRead(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const method = "remote.read"
	var p RemoteReadParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	switch p.Op {
	case "info", "workspaces", "tasks", "task", "status", "activity":
	default:
		return nil, &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("%s: unknown op %q", method, p.Op)}
	}
	if p.Host == "" {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": host is required"}
	}
	h, err := r.f.resolveHost(method, p.Host)
	if err != nil {
		return nil, err
	}
	var co client.CallOpts
	if rc.runScoped && rc.ident.taskID != 0 {
		co.OriginTask = strconv.FormatInt(rc.ident.taskID, 10)
	}
	res := RemoteReadResult{Host: h.Alias, HostID: h.HostID, Op: p.Op}
	tag := func(t wire.Task) remoteTask {
		return remoteTask{Task: t, Host: h.Alias, GlobalID: h.Alias + ":" + strconv.FormatInt(t.ID, 10)}
	}
	switch p.Op {
	case "info":
		res.Data, err = r.client.Info(ctx, p.Host, co)
	case "workspaces":
		res.Data, err = r.client.Workspaces(ctx, p.Host, co)
	case "tasks":
		var l wire.TaskList
		if l, err = r.client.Tasks(ctx, p.Host, co, p.WorkspaceID, p.Cursor, p.Limit); err == nil {
			tl := remoteTaskList{Tasks: make([]remoteTask, 0, len(l.Tasks)), NextCursor: l.NextCursor}
			for _, t := range l.Tasks {
				tl.Tasks = append(tl.Tasks, tag(t))
			}
			res.Data = tl
		}
	case "task":
		var t wire.Task
		if t, err = r.client.Task(ctx, p.Host, co, p.ID); err == nil {
			res.Data = tag(t)
		}
	case "status":
		var ob client.Observation
		ob, err = r.client.Observe(ctx, p.Host, co, p.RunID, p.TaskID)
		if ob.Unknown {
			res.Data, res.StatusUnknown, res.Failures, err = nil, true, ob.Failures, nil
		} else if err == nil {
			res.Data = ob.Status
		}
	case "activity":
		res.Data, err = r.client.Activity(ctx, p.Host, co, p.TaskID, p.RunID, p.Cursor, p.Limit)
	}
	if err != nil {
		return nil, remoteError(h.Alias, err)
	}
	return res, nil
}

// remoteError maps a client failure to a wire error: a typed refusal keeps
// its code unchanged, anything else is the host being unreachable. It never
// falls back to a local result.
func remoteError(alias string, err error) error {
	var e *client.Error
	if errors.As(err, &e) {
		return &rpcCodeError{code: e.Code, msg: e.Message, err: err}
	}
	return &rpcCodeError{code: client.CodeHostUnavailable, msg: fmt.Sprintf("%s: %v", alias, err), err: err}
}
