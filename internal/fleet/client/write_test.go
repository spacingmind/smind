package client

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/wsclient"
)

func validCreate() wire.TaskCreateParams {
	return wire.TaskCreateParams{
		IdempotencyKey: "key-1",
		WorkspaceID:    7,
		Provider:       "codex",
		Model:          "gpt-5",
		Title:          "fix",
		Prompt:         "fix the bug",
	}
}

func TestClientWriteAttachesProvenanceAndKey(t *testing.T) {
	e, c := newEnv(t)
	p := validCreate()
	h, _, err := c.Create(context.Background(), "box", CallOpts{}, p)
	if err != nil {
		t.Fatal(err)
	}
	if h != (Host{HostID: "h1", Alias: "box"}) {
		t.Errorf("host = %+v", h)
	}
	if len(e.fc.methods) != 1 || e.fc.methods[0] != wire.MethodTaskCreate {
		t.Fatalf("methods = %v", e.fc.methods)
	}
	var got wire.TaskCreateParams
	if err := json.Unmarshal(e.fc.params[0], &got); err != nil {
		t.Fatal(err)
	}
	prov := got.Prov
	if prov.Origin != self || len(prov.Hops) != 1 || prov.Hops[0] != self {
		t.Errorf("prov = %+v", prov)
	}
	if prov.Trace == "" || prov.ReqID == "" {
		t.Errorf("empty trace/reqId: %+v", prov)
	}
	if got.IdempotencyKey != p.IdempotencyKey {
		t.Errorf("idempotencyKey = %q, want %q", got.IdempotencyKey, p.IdempotencyKey)
	}
	if p.Prov.Origin != "" || p.Prov.ReqID != "" {
		t.Errorf("caller's params mutated: %+v", p.Prov)
	}
}

func TestClientWriteRefusalsTyped(t *testing.T) {
	e, c := newEnv(t)
	for _, code := range []string{"not_granted", wire.CodeBudgetExceeded} {
		e.fc.err = &wsclient.RPCError{Code: code, Message: "no"}
		_, _, err := c.Send(context.Background(), "box", CallOpts{}, wire.TaskSendParams{
			IdempotencyKey: "k", TaskID: 3, Prompt: "go",
		})
		if !IsCode(err, code) {
			t.Errorf("%s: err = %v", code, err)
		}
		if Ambiguous(err) {
			t.Errorf("%s: ambiguous", code)
		}
	}
}

func TestClientAmbiguousClassification(t *testing.T) {
	e, c := newEnv(t)
	e.state = LinkConnecting
	_, _, connErr := c.Stop(context.Background(), "box", CallOpts{}, wire.TaskStopParams{TaskID: 1})
	if !IsConnecting(connErr) {
		t.Fatalf("setup: connErr = %v", connErr)
	}
	cases := []struct {
		name                           string
		err                            error
		ambiguous, failure, connecting bool
	}{
		{"nil", nil, false, false, false},
		{"transport", errors.New("conn reset"), true, true, false},
		{"timeout", &Error{Code: CodeTimeout}, true, true, false},
		{"host down", &Error{Code: CodeHostUnavailable}, false, true, false},
		{"connecting", connErr, false, false, true},
		{"refusal", &Error{Code: wire.CodeNotGranted}, false, false, false},
		{"unknown host", &Error{Code: CodeUnknownHost}, false, false, false},
	}
	for _, tc := range cases {
		if got := Ambiguous(tc.err); got != tc.ambiguous {
			t.Errorf("%s: Ambiguous = %v", tc.name, got)
		}
		if got := CountsAsFailure(tc.err); got != tc.failure {
			t.Errorf("%s: CountsAsFailure = %v", tc.name, got)
		}
		if got := IsConnecting(tc.err); got != tc.connecting {
			t.Errorf("%s: IsConnecting = %v", tc.name, got)
		}
	}
}

// deadlineCaller records the deadline of the context it is called with, since
// fakeCaller ignores it.
type deadlineCaller struct {
	fakeCaller
	deadline time.Time
	has      bool
}

func (d *deadlineCaller) Call(ctx context.Context, method string, params, out any) error {
	if dl, ok := ctx.Deadline(); ok {
		d.deadline, d.has = dl, true
	}
	return d.fakeCaller.Call(ctx, method, params, out)
}

// newWaitClient builds a client with the default Options.Timeout, since
// newEnv pins a 20 ms one.
func newWaitClient(t *testing.T, d *deadlineCaller) *Client {
	t.Helper()
	return New(Options{
		Self:    self,
		Resolve: func(ref string) (Host, error) { return Host{HostID: "h1", Alias: "box"}, nil },
		Conn:    func(string) (Caller, LinkState) { return d, LinkConnected },
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0) },
		NewID:   func() string { return "id-w" },
	})
}

func TestClientWaitUsesLongTimeout(t *testing.T) {
	d := &deadlineCaller{}
	c := newWaitClient(t, d)
	p := wire.RunWaitParams{RunID: "r1"}
	if _, _, err := c.Wait(context.Background(), "box", CallOpts{}, p); err != nil {
		t.Fatal(err)
	}
	if !d.has {
		t.Fatal("no deadline on wait context")
	}
	if got := time.Until(d.deadline); got <= 30*time.Second {
		t.Errorf("default deadline %v away, want > 30s", got)
	}
	if _, _, err := c.Wait(context.Background(), "box", CallOpts{Timeout: 2 * time.Second}, p); err != nil {
		t.Fatal(err)
	}
	if got := time.Until(d.deadline); got > 2200*time.Millisecond {
		t.Errorf("2s override deadline %v away", got)
	}
}

func TestClientCreateInvalidParamsNeverCalls(t *testing.T) {
	e, c := newEnv(t)
	_, _, err := c.Create(context.Background(), "box", CallOpts{}, wire.TaskCreateParams{})
	if !IsCode(err, wire.CodeInvalid) {
		t.Fatalf("err = %v", err)
	}
	if e.fc.calls != 0 {
		t.Errorf("caller called %d times", e.fc.calls)
	}
}
