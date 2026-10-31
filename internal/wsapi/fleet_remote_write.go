package wsapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// The write half of the remote bridge (ADR-0048 F2b): remote.write carries a
// create, send, stop or wait to a peer host over this daemon's live link,
// mirrors the peer's task locally and opens the watch that delivers the
// run's events to the caller's chat. Every failure that leaves the outcome
// unknown names the idempotency key, so a retry can never silently double
// the work.

// codeLocalWriteFailed: the peer DID act but the local mirror/watch could
// not be recorded -- the run exists and must be found, not recreated blind.
const codeLocalWriteFailed = "local_write_failed"

// maxWaitSeconds caps a wait's long poll at the wire limit.
const maxWaitSeconds = int(wire.MaxRunWait / time.Second)

// RemoteWriteParams is the remote.write ask.
type RemoteWriteParams struct {
	Host           string `json:"host"`
	Op             string `json:"op"`
	WorkspaceID    int64  `json:"workspaceId"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Title          string `json:"title"`
	Prompt         string `json:"prompt"`
	Branch         string `json:"branch"`
	BaseRef        string `json:"baseRef"`
	TaskID         int64  `json:"taskId"`
	RunID          string `json:"runId"`
	Cursor         int64  `json:"cursor"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	IdempotencyKey string `json:"idempotencyKey"`
	Notify         *bool  `json:"notify"`
}

// RemoteWriteResult wraps the wire write result with the host it ran on.
// Events is always an array so a wait can be resumed from nextCursor.
type RemoteWriteResult struct {
	Host              string       `json:"host"`
	HostID            string       `json:"hostId"`
	Op                string       `json:"op"`
	GlobalID          string       `json:"globalId"`
	TaskID            int64        `json:"taskId"`
	RunID             string       `json:"runId"`
	Branch            string       `json:"branch,omitempty"`
	Replayed          bool         `json:"replayed,omitempty"`
	Stopped           bool         `json:"stopped,omitempty"`
	IdempotencyKey    string       `json:"idempotencyKey,omitempty"`
	WatchID           int64        `json:"watchId,omitempty"`
	State             string       `json:"state,omitempty"`
	WaitingPermission bool         `json:"waitingPermission,omitempty"`
	Events            []wire.Event `json:"events"`
	NextCursor        int64        `json:"nextCursor,omitempty"`
	Done              bool         `json:"done,omitempty"`
	StopReason        string       `json:"stopReason,omitempty"`
}

func (r *fleetRemote) handleWrite(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const method = "remote.write"
	var p RemoteWriteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	switch p.Op {
	case "create", "send", "stop", "wait":
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
	switch p.Op {
	case "create":
		return r.writeCreate(ctx, rc, h, p, co)
	case "send":
		return r.writeSend(ctx, rc, h, p, co)
	case "stop":
		return r.writeStop(ctx, h, p, co)
	default:
		return r.writeWait(ctx, h, p, co)
	}
}

// writeCreate makes a task on the peer and records the mirror and the watch
// of the run it started -- together, or a refusal that names the key.
func (r *fleetRemote) writeCreate(ctx context.Context, rc *requestContext, h registry.Host, p RemoteWriteParams, co client.CallOpts) (RemoteWriteResult, error) {
	key := idemKey(p.IdempotencyKey)
	_, res, err := r.client.Create(ctx, p.Host, co, wire.TaskCreateParams{
		IdempotencyKey: key,
		WorkspaceID:    p.WorkspaceID,
		Provider:       p.Provider,
		Model:          p.Model,
		Title:          p.Title,
		Prompt:         p.Prompt,
		Branch:         p.Branch,
		BaseRef:        p.BaseRef,
	})
	if err != nil {
		return RemoteWriteResult{}, writeError(h.Alias, err, key, "create")
	}
	remoteTaskID := strconv.FormatInt(res.TaskID, 10)
	mirror := store.FleetRemoteTask{
		HostID:       h.HostID,
		RemoteTaskID: remoteTaskID,
		LocalTaskID:  rc.ident.taskID,
		Title:        p.Title,
		Branch:       res.Branch,
		State:        "running",
	}
	watchID, err := r.putWatch(mirror, newWatch(rc, p, h, remoteTaskID, res.RunID))
	if err != nil {
		return RemoteWriteResult{}, &rpcCodeError{code: codeLocalWriteFailed, msg: fmt.Sprintf(
			"%s:%d run %s WAS created on the host but the local watch could not be written (%v); inspect it with task_status, or retry create with idempotencyKey %s (safe: the same key returns the same task)",
			h.Alias, res.TaskID, res.RunID, err, key), err: err}
	}
	return RemoteWriteResult{
		Host:           h.Alias,
		HostID:         h.HostID,
		Op:             "create",
		GlobalID:       h.Alias + ":" + remoteTaskID,
		TaskID:         res.TaskID,
		RunID:          res.RunID,
		Branch:         res.Branch,
		Replayed:       res.Replayed,
		IdempotencyKey: key,
		WatchID:        watchID,
		Events:         []wire.Event{},
	}, nil
}

// writeSend continues a task on the peer and watches the new run, keeping
// the title and branch the mirror already had.
func (r *fleetRemote) writeSend(ctx context.Context, rc *requestContext, h registry.Host, p RemoteWriteParams, co client.CallOpts) (RemoteWriteResult, error) {
	key := idemKey(p.IdempotencyKey)
	_, res, err := r.client.Send(ctx, p.Host, co, wire.TaskSendParams{
		IdempotencyKey: key,
		TaskID:         p.TaskID,
		Prompt:         p.Prompt,
	})
	if err != nil {
		return RemoteWriteResult{}, writeError(h.Alias, err, key, "send")
	}
	remoteTaskID := strconv.FormatInt(res.TaskID, 10)
	mirror := store.FleetRemoteTask{
		HostID:       h.HostID,
		RemoteTaskID: remoteTaskID,
		LocalTaskID:  rc.ident.taskID,
		Title:        fmt.Sprintf("task %d", res.TaskID),
		State:        "running",
	}
	if existing, ok := r.existingMirror(h.HostID, strconv.FormatInt(p.TaskID, 10)); ok {
		mirror.Title, mirror.Branch = existing.Title, existing.Branch
		if mirror.Title == "" {
			mirror.Title = fmt.Sprintf("task %d", res.TaskID)
		}
	}
	watchID, err := r.putWatch(mirror, newWatch(rc, p, h, remoteTaskID, res.RunID))
	if err != nil {
		return RemoteWriteResult{}, &rpcCodeError{code: codeLocalWriteFailed, msg: fmt.Sprintf(
			"%s:%d run %s WAS started on the host but the local watch could not be written (%v); inspect it with task_status, or retry send with idempotencyKey %s (safe: the same key returns the same run)",
			h.Alias, res.TaskID, res.RunID, err, key), err: err}
	}
	return RemoteWriteResult{
		Host:           h.Alias,
		HostID:         h.HostID,
		Op:             "send",
		GlobalID:       h.Alias + ":" + remoteTaskID,
		TaskID:         res.TaskID,
		RunID:          res.RunID,
		Replayed:       res.Replayed,
		IdempotencyKey: key,
		WatchID:        watchID,
		Events:         []wire.Event{},
	}, nil
}

// writeStop stops a task (or one run of it) on the peer. The local watch is
// left alone: the watcher sees the stop and closes itself.
func (r *fleetRemote) writeStop(ctx context.Context, h registry.Host, p RemoteWriteParams, co client.CallOpts) (RemoteWriteResult, error) {
	_, res, err := r.client.Stop(ctx, p.Host, co, wire.TaskStopParams{TaskID: p.TaskID, RunID: p.RunID})
	if err != nil {
		return RemoteWriteResult{}, remoteError(h.Alias, err)
	}
	return RemoteWriteResult{
		Host:     h.Alias,
		HostID:   h.HostID,
		Op:       "stop",
		GlobalID: h.Alias + ":" + strconv.FormatInt(p.TaskID, 10),
		TaskID:   p.TaskID,
		RunID:    p.RunID,
		Stopped:  res.Stopped,
		Events:   []wire.Event{},
	}, nil
}

// writeWait long-polls one run on the peer, capped at the wire limit.
func (r *fleetRemote) writeWait(ctx context.Context, h registry.Host, p RemoteWriteParams, co client.CallOpts) (RemoteWriteResult, error) {
	timeout := p.TimeoutSeconds
	if timeout > maxWaitSeconds {
		timeout = maxWaitSeconds
	}
	_, res, err := r.client.Wait(ctx, p.Host, co, wire.RunWaitParams{
		RunID:          p.RunID,
		Cursor:         p.Cursor,
		TimeoutSeconds: timeout,
	})
	if err != nil {
		return RemoteWriteResult{}, remoteError(h.Alias, err)
	}
	events := res.Events
	if events == nil {
		events = []wire.Event{}
	}
	return RemoteWriteResult{
		Host:              h.Alias,
		HostID:            h.HostID,
		Op:                "wait",
		RunID:             res.RunID,
		State:             res.State,
		WaitingPermission: res.WaitingPermission,
		Events:            events,
		NextCursor:        res.NextCursor,
		Done:              res.Done,
		StopReason:        res.StopReason,
	}, nil
}

// idemKey returns the caller's idempotency key or mints one.
func idemKey(k string) string {
	if k != "" {
		return k
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// writeError maps a create/send failure: an ambiguous transport failure
// names the idempotency key (a retry with it is safe), anything else is the
// usual typed refusal or host_unavailable and definitely did not act.
func writeError(alias string, err error, key, verb string) error {
	if client.Ambiguous(err) {
		return &rpcCodeError{code: codeAmbiguous, msg: fmt.Sprintf(
			"%s: the %s may or may not have reached the host; retry with idempotencyKey %s (the same key returns the same task, a different key may create a second one)",
			alias, verb, key), err: err}
	}
	return remoteError(alias, err)
}

// newWatch is the watch a create/send opens on the run it started: nil when
// there is no run-scoped chat to notify, or the caller opted out.
func newWatch(rc *requestContext, p RemoteWriteParams, h registry.Host, remoteTaskID, runID string) *store.FleetWatch {
	if notify := (p.Notify == nil || *p.Notify) && rc.runScoped && rc.ident.chatID != 0; !notify {
		return nil
	}
	return &store.FleetWatch{
		HostID:       h.HostID,
		RemoteTaskID: remoteTaskID,
		RemoteRunID:  runID,
		LocalTaskID:  rc.ident.taskID,
		ChatID:       rc.ident.chatID,
		Notify:       true,
	}
}

// putWatch records the mirror and, when there is one, the watch in a single
// transaction, then hands a new watch to the watcher.
func (r *fleetRemote) putWatch(mirror store.FleetRemoteTask, w *store.FleetWatch) (int64, error) {
	put := r.o.PutWatch
	if put == nil {
		if r.f.opts.Store == nil {
			return 0, errors.New("remote.write: no fleet store for the mirror and watch")
		}
		put = r.f.opts.Store.PutFleetRemoteTaskAndWatch
	}
	id, err := put(mirror, w)
	if err != nil {
		return 0, err
	}
	if r.o.Watch != nil && id != 0 {
		r.o.Watch.Add(id)
	}
	return id, nil
}

// existingMirror reads a task's mirrored title and branch, best effort: a
// send that cannot read the mirror still records a usable one.
func (r *fleetRemote) existingMirror(hostID, remoteTaskID string) (store.FleetRemoteTask, bool) {
	if r.f.opts.Store == nil {
		return store.FleetRemoteTask{}, false
	}
	t, err := r.f.opts.Store.GetFleetRemoteTask(hostID, remoteTaskID)
	return t, err == nil
}
