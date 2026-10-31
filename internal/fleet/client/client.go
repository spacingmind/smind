// Package client is the caller side of the fleet read methods (ADR-0048): typed
// calls over one live peer connection, with provenance attached and every
// failure mapped to either a typed *Error or a plain transport error.
package client

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/fleet/wire"
)

// Caller is one live connection to a peer (satisfied by link.Conn).
type Caller interface {
	Call(ctx context.Context, method string, params, out any) error
}

// LinkState is the state of the link to a peer as the client sees it.
type LinkState string

const (
	LinkConnected  LinkState = "connected"
	LinkConnecting LinkState = "connecting"
	// LinkOther covers down, refused, peer_disabled, awaiting_approval and no link.
	LinkOther LinkState = "other"
)

// Error codes of the client itself, besides the wire.Code* ones.
const (
	CodeHostUnavailable    = "host_unavailable"
	CodeTimeout            = "timeout"
	CodeStatusUnknown      = "status_unknown"
	CodeUnknownHost        = "unknown_host"
	CodeVersionUnsupported = "fleet_version_unsupported"
)

// ErrUnknownHost is wrapped by Options.Resolve when a host is removed or unknown.
var ErrUnknownHost = errors.New("unknown host")

// Host is a resolved peer.
type Host struct{ HostID, Alias string }

// Options configures a Client.
type Options struct {
	Self         string                                  // this daemon's hostId (provenance origin)
	Resolve      func(ref string) (Host, error)          // alias or id -> Host
	Conn         func(hostID string) (Caller, LinkState) // Caller nil unless LinkConnected
	Now          func() time.Time                        // default time.Now
	NewID        func() string                           // default random UUIDv4
	Timeout      time.Duration                           // per call, default 15s
	FailureLimit int                                     // default 3
}

// CallOpts are the per-call provenance inputs. An empty Trace mints a new id.
type CallOpts struct {
	Trace, OriginTask string

	// Timeout overrides Options.Timeout for this call when > 0.
	Timeout time.Duration
}

// Error is a typed refusal or client-side failure.
type Error struct {
	Code, Message, Host string

	connecting bool
}

func (e *Error) Error() string {
	s := fmt.Sprintf("%s: %s", e.Code, e.Message)
	if e.Host != "" {
		s += " (host " + e.Host + ")"
	}
	return s
}

// IsCode reports whether err is an *Error with the given code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// IsTransport reports whether err is a failure that is not a typed *Error.
func IsTransport(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	return !errors.As(err, &e)
}

// IsConnecting reports whether err is a host_unavailable because the link is
// still connecting: nothing was sent, so the peer is not at fault.
func IsConnecting(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == CodeHostUnavailable && e.connecting
}

// CountsAsFailure reports whether err says the peer failed to answer:
// transport errors, timeouts and a host that is down do; typed refusals and
// a link still connecting do not.
func CountsAsFailure(err error) bool {
	if err == nil {
		return false
	}
	if IsTransport(err) || IsCode(err, CodeTimeout) {
		return true
	}
	return IsCode(err, CodeHostUnavailable) && !IsConnecting(err)
}

// Ambiguous reports whether the callee may have acted before the error, so a
// blind retry is unsafe. Only transport errors and timeouts are ambiguous;
// host_unavailable, unknown_host and every typed refusal mean the call was
// definitely not carried out.
func Ambiguous(err error) bool {
	return IsTransport(err) || IsCode(err, CodeTimeout)
}

// Client makes typed fleet read calls to peers.
type Client struct {
	o  Options
	ob observer
}

// New returns a Client with defaults applied.
func New(o Options) *Client {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.NewID == nil {
		o.NewID = newUUID
	}
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	if o.FailureLimit <= 0 {
		o.FailureLimit = 3
	}
	return &Client{o: o}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Info calls fleet.info.
func (c *Client) Info(ctx context.Context, ref string, o CallOpts) (wire.Info, error) {
	var out wire.Info
	_, err := c.call(ctx, ref, o, wire.MethodInfo, func(p wire.Prov) any {
		return wire.InfoParams{Prov: p}
	}, &out)
	return out, err
}

// Workspaces calls fleet.workspace.list.
func (c *Client) Workspaces(ctx context.Context, ref string, o CallOpts) (wire.WorkspaceList, error) {
	var out wire.WorkspaceList
	_, err := c.call(ctx, ref, o, wire.MethodWorkspaceList, func(p wire.Prov) any {
		return wire.WorkspaceListParams{Prov: p}
	}, &out)
	return out, err
}

// Tasks calls fleet.task.list.
func (c *Client) Tasks(ctx context.Context, ref string, o CallOpts, workspaceID, cursor int64, limit int) (wire.TaskList, error) {
	var out wire.TaskList
	_, err := c.call(ctx, ref, o, wire.MethodTaskList, func(p wire.Prov) any {
		return wire.TaskListParams{Prov: p, WorkspaceID: workspaceID, Cursor: cursor, Limit: limit}
	}, &out)
	return out, err
}

// Task calls fleet.task.get.
func (c *Client) Task(ctx context.Context, ref string, o CallOpts, id int64) (wire.Task, error) {
	var out wire.Task
	_, err := c.call(ctx, ref, o, wire.MethodTaskGet, func(p wire.Prov) any {
		return wire.TaskGetParams{Prov: p, ID: id}
	}, &out)
	return out, err
}

// RunStatus calls fleet.run.status.
func (c *Client) RunStatus(ctx context.Context, ref string, o CallOpts, runID string, taskID int64) (wire.RunStatus, error) {
	var out wire.RunStatus
	_, err := c.call(ctx, ref, o, wire.MethodRunStatus, func(p wire.Prov) any {
		return wire.RunStatusParams{Prov: p, RunID: runID, TaskID: taskID}
	}, &out)
	return out, err
}

// Activity calls fleet.task.activity.
func (c *Client) Activity(ctx context.Context, ref string, o CallOpts, taskID int64, runID string, cursor int64, limit int) (wire.Activity, error) {
	var out wire.Activity
	_, err := c.call(ctx, ref, o, wire.MethodTaskActivity, func(p wire.Prov) any {
		return wire.TaskActivityParams{Prov: p, TaskID: taskID, RunID: runID, Cursor: cursor, Limit: limit}
	}, &out)
	return out, err
}

func (c *Client) call(ctx context.Context, ref string, o CallOpts, method string, build func(p wire.Prov) any, out any) (Host, error) {
	h, err := c.o.Resolve(ref)
	if err != nil {
		return Host{}, &Error{Code: CodeUnknownHost, Message: err.Error(), Host: ref}
	}
	conn, state := c.o.Conn(h.HostID)
	switch {
	case state == LinkConnecting:
		return h, &Error{Code: CodeHostUnavailable, Message: fmt.Sprintf("link to %s is still connecting", h.Alias), Host: h.Alias, connecting: true}
	case state != LinkConnected || conn == nil:
		return h, &Error{Code: CodeHostUnavailable, Message: fmt.Sprintf("%s is not connected", h.Alias), Host: h.Alias}
	}
	trace := o.Trace
	if trace == "" {
		trace = c.o.NewID()
	}
	prov := wire.Prov{
		Origin:     c.o.Self,
		Trace:      trace,
		Hops:       []string{c.o.Self},
		OriginTask: o.OriginTask,
		ReqID:      c.o.NewID(),
		TS:         c.o.Now().Unix(),
	}
	timeout := c.o.Timeout
	if o.Timeout > 0 {
		timeout = o.Timeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = conn.Call(cctx, method, build(prov), out)
	if err == nil {
		return h, nil
	}
	var re rpcError
	switch {
	case errors.As(err, &re) && re.RPCCode() != "":
		return h, &Error{Code: re.RPCCode(), Message: re.RPCMessage(), Host: h.Alias}
	case errors.Is(err, context.DeadlineExceeded):
		return h, &Error{Code: CodeTimeout, Message: fmt.Sprintf("call to %s timed out", h.Alias), Host: h.Alias}
	}
	return h, fmt.Errorf("fleet client: %s: %w", h.Alias, err)
}

// rpcError is a coded error from the wire (wsclient.RPCError); matched by
// interface so this package does not import wsclient.
type rpcError interface {
	error
	RPCCode() string
	RPCMessage() string
}
