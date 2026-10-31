package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/wsclient"
)

const self = "host-self"

type fakeCaller struct {
	methods []string
	params  []json.RawMessage
	result  string
	err     error
	block   bool
	calls   int
}

func (f *fakeCaller) Call(ctx context.Context, method string, params, out any) error {
	f.calls++
	b, _ := json.Marshal(params)
	f.methods = append(f.methods, method)
	f.params = append(f.params, b)
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.err != nil {
		return f.err
	}
	if f.result != "" && out != nil {
		return json.Unmarshal([]byte(f.result), out)
	}
	return nil
}

type env struct {
	fc    *fakeCaller
	state LinkState
	now   time.Time
	n     int
}

func newEnv(t *testing.T) (*env, *Client) {
	t.Helper()
	e := &env{fc: &fakeCaller{}, state: LinkConnected, now: time.Unix(1_700_000_000, 0)}
	c := New(Options{
		Self: self,
		Resolve: func(ref string) (Host, error) {
			if ref == "gone" {
				return Host{}, fmt.Errorf("resolve %q: %w", ref, ErrUnknownHost)
			}
			return Host{HostID: "h1", Alias: "box"}, nil
		},
		Conn: func(string) (Caller, LinkState) {
			if e.state != LinkConnected {
				return nil, e.state
			}
			return e.fc, e.state
		},
		Now:     func() time.Time { return e.now },
		NewID:   func() string { e.n++; return fmt.Sprintf("id-%d", e.n) },
		Timeout: 20 * time.Millisecond,
	})
	return e, c
}

func runAll(c *Client, o CallOpts) {
	ctx := context.Background()
	_, _ = c.Info(ctx, "box", o)
	_, _ = c.Workspaces(ctx, "box", o)
	_, _ = c.Tasks(ctx, "box", o, 1, 2, 3)
	_, _ = c.Task(ctx, "box", o, 4)
	_, _ = c.RunStatus(ctx, "box", o, "r1", 5)
	_, _ = c.Activity(ctx, "box", o, 6, "r2", 7, 8)
}

func provOf(t *testing.T, raw json.RawMessage) wire.Prov {
	t.Helper()
	var p struct {
		Prov wire.Prov `json:"prov"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p.Prov
}

func TestClientAttachesProvenance(t *testing.T) {
	e, c := newEnv(t)
	runAll(c, CallOpts{})
	if len(e.fc.params) != 6 {
		t.Fatalf("calls = %d", len(e.fc.params))
	}
	seen := map[string]bool{}
	for i, raw := range e.fc.params {
		p := provOf(t, raw)
		if p.Origin != self || len(p.Hops) != 1 || p.Hops[0] != self {
			t.Errorf("%s: origin/hops = %q %v", e.fc.methods[i], p.Origin, p.Hops)
		}
		if p.Trace == "" || p.ReqID == "" {
			t.Errorf("%s: empty trace/reqId", e.fc.methods[i])
		}
		if d := p.TS - e.now.Unix(); d < -2 || d > 2 {
			t.Errorf("%s: ts off by %d", e.fc.methods[i], d)
		}
		if seen[p.ReqID] {
			t.Errorf("duplicate reqId %s", p.ReqID)
		}
		seen[p.ReqID] = true
	}

	e.fc.params = nil
	runAll(c, CallOpts{Trace: "my-trace", OriginTask: "task-9"})
	for _, raw := range e.fc.params {
		p := provOf(t, raw)
		if p.Trace != "my-trace" || p.OriginTask != "task-9" {
			t.Errorf("opts not carried: %+v", p)
		}
	}
}

func TestClientParamsDecodeToWireTypes(t *testing.T) {
	e, c := newEnv(t)
	runAll(c, CallOpts{})
	var tl wire.TaskListParams
	var tg wire.TaskGetParams
	var rs wire.RunStatusParams
	var ac wire.TaskActivityParams
	for i, dst := range []any{nil, nil, &tl, &tg, &rs, &ac} {
		if dst == nil {
			continue
		}
		if err := json.Unmarshal(e.fc.params[i], dst); err != nil {
			t.Fatal(err)
		}
	}
	if tl.WorkspaceID != 1 || tl.Cursor != 2 || tl.Limit != 3 {
		t.Errorf("tasks: %+v", tl)
	}
	if tg.ID != 4 {
		t.Errorf("task: %+v", tg)
	}
	if rs.RunID != "r1" || rs.TaskID != 5 {
		t.Errorf("runstatus: %+v", rs)
	}
	if ac.TaskID != 6 || ac.RunID != "r2" || ac.Cursor != 7 || ac.Limit != 8 {
		t.Errorf("activity: %+v", ac)
	}
	want := []string{wire.MethodInfo, wire.MethodWorkspaceList, wire.MethodTaskList, wire.MethodTaskGet, wire.MethodRunStatus, wire.MethodTaskActivity}
	for i, m := range want {
		if e.fc.methods[i] != m {
			t.Errorf("method %d = %s, want %s", i, e.fc.methods[i], m)
		}
	}
}

func TestClientRefusalCodesTyped(t *testing.T) {
	e, c := newEnv(t)
	for _, code := range []string{wire.CodeHopLimit, wire.CodeCycle, wire.CodeNotGranted, CodeVersionUnsupported} {
		e.fc.err = &wsclient.RPCError{Code: code, Message: "x"}
		_, err := c.Info(context.Background(), "box", CallOpts{})
		if !IsCode(err, code) {
			t.Errorf("%s: err = %v", code, err)
		}
		var te *Error
		if !errors.As(err, &te) || te.Host != "box" {
			t.Errorf("%s: host = %+v", code, te)
		}
		if IsTransport(err) {
			t.Errorf("%s: counted as transport", code)
		}
	}
}

func TestClientTimeout(t *testing.T) {
	e, c := newEnv(t)
	e.fc.block = true
	_, err := c.Info(context.Background(), "box", CallOpts{})
	if !IsCode(err, CodeTimeout) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientNoConnIsHostUnavailable(t *testing.T) {
	e, c := newEnv(t)
	msgs := map[LinkState]string{}
	for _, st := range []LinkState{LinkOther, LinkConnecting} {
		e.state = st
		_, err := c.Info(context.Background(), "box", CallOpts{})
		if !IsCode(err, CodeHostUnavailable) {
			t.Fatalf("%s: err = %v", st, err)
		}
		msgs[st] = err.Error()
	}
	if msgs[LinkOther] == msgs[LinkConnecting] {
		t.Errorf("messages equal: %q", msgs[LinkOther])
	}
	if e.fc.calls != 0 {
		t.Errorf("caller called %d times", e.fc.calls)
	}
}

func TestClientUnknownHost(t *testing.T) {
	_, c := newEnv(t)
	_, err := c.Info(context.Background(), "gone", CallOpts{})
	if !IsCode(err, CodeUnknownHost) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientTransportErrorIsNotTyped(t *testing.T) {
	e, c := newEnv(t)
	e.fc.err = errors.New("boom")
	_, err := c.Info(context.Background(), "box", CallOpts{})
	if !IsTransport(err) {
		t.Fatalf("err = %v", err)
	}
	for _, code := range []string{CodeTimeout, CodeHostUnavailable, wire.CodeNotGranted, "anything"} {
		if IsCode(err, code) {
			t.Errorf("IsCode(%s) true", code)
		}
	}
}
