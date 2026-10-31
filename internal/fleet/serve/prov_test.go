package serve

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_800_000_000, 0)} }

type fakeAuthorizer struct {
	grant registry.Grant
	err   error
}

func (a fakeAuthorizer) Authorize(string, string, int64) (registry.Grant, error) {
	return a.grant, a.err
}

type memAudit struct{ rows []store.FleetAuditRow }

func (m *memAudit) AppendFleetAudit(r store.FleetAuditRow) (int64, error) {
	m.rows = append(m.rows, r)
	return int64(len(m.rows)), nil
}

type fakeReader struct {
	calls int
	err   error
}

func (f *fakeReader) Info(registry.Grant) (wire.Info, error) {
	f.calls++
	return wire.Info{HostID: "x"}, f.err
}
func (f *fakeReader) Workspaces(registry.Grant) (wire.WorkspaceList, error) {
	f.calls++
	return wire.WorkspaceList{}, f.err
}
func (f *fakeReader) TaskList(registry.Grant, wire.TaskListParams) (wire.TaskList, error) {
	f.calls++
	return wire.TaskList{}, f.err
}
func (f *fakeReader) TaskGet(_ registry.Grant, p wire.TaskGetParams) (wire.Task, error) {
	f.calls++
	return wire.Task{ID: p.ID, Title: "t"}, f.err
}
func (f *fakeReader) RunStatus(registry.Grant, wire.RunStatusParams) (wire.RunStatus, error) {
	f.calls++
	return wire.RunStatus{}, f.err
}
func (f *fakeReader) Activity(registry.Grant, wire.TaskActivityParams) (wire.Activity, error) {
	f.calls++
	return wire.Activity{}, f.err
}

func newTestServer(self string, clk *fakeClock) (*Server, *memAudit, *fakeReader) {
	au := &memAudit{}
	rd := &fakeReader{}
	s := New(Options{
		HostID:     self,
		Authorizer: fakeAuthorizer{grant: registry.DefaultForwardGrant()},
		Audit:      au,
		Reader:     rd,
		Now:        clk.now,
	})
	return s, au, rd
}

func goodProv(clk *fakeClock, principal, req string) wire.Prov {
	return wire.Prov{Origin: principal, Trace: "tr", Hops: []string{principal}, ReqID: req, TS: clk.now().Unix()}
}

func code(r *Refusal) string {
	if r == nil {
		return ""
	}
	return r.Code
}

func TestFleetPrincipalMismatchRefused(t *testing.T) {
	clk := newClock()
	s, _, _ := newTestServer("A", clk)
	p := goodProv(clk, "B", "r1")
	p.Origin = "C"
	if got := code(s.checkProv("B", p, clk.now())); got != wire.CodePrincipalMismatch {
		t.Fatalf("origin mismatch: %q", got)
	}
	p = goodProv(clk, "B", "r2")
	p.Hops = []string{"C"}
	if got := code(s.checkProv("B", p, clk.now())); got != wire.CodePrincipalMismatch {
		t.Fatalf("hops[0] mismatch: %q", got)
	}
}

func TestFleetCycleRefused_AtoBtoA(t *testing.T) {
	clk := newClock()
	s, _, _ := newTestServer("A", clk)
	p := wire.Prov{Origin: "B", Trace: "t", Hops: []string{"B", "A"}, ReqID: "r", TS: clk.now().Unix()}
	if got := code(s.checkProv("B", p, clk.now())); got != wire.CodeCycle {
		t.Fatalf("got %q, want cycle", got)
	}
	// A forged origin "A" from principal "B": the mismatch check fires first,
	// so the cycle code is never reached.
	p = wire.Prov{Origin: "A", Trace: "t", Hops: []string{"A"}, ReqID: "r2", TS: clk.now().Unix()}
	if got := code(s.checkProv("B", p, clk.now())); got != wire.CodePrincipalMismatch {
		t.Fatalf("got %q, want principal_mismatch", got)
	}
}

func TestFleetHopTwoRefused(t *testing.T) {
	clk := newClock()
	s, _, _ := newTestServer("C", clk)
	p := wire.Prov{Origin: "A", Trace: "t", Hops: []string{"A", "B"}, ReqID: "r", TS: clk.now().Unix()}
	if got := code(s.checkProv("A", p, clk.now())); got != wire.CodeHopLimit {
		t.Fatalf("two hops: %q", got)
	}
	p.Hops = nil
	if got := code(s.checkProv("A", p, clk.now())); got != wire.CodeHopLimit {
		t.Fatalf("empty hops: %q", got)
	}
}

func TestFleetReplayAndStaleRefused(t *testing.T) {
	clk := newClock()
	s, _, _ := newTestServer("A", clk)
	at := func(req string, off time.Duration) *Refusal {
		p := goodProv(clk, "B", req)
		p.TS = clk.now().Add(off).Unix()
		return s.checkProv("B", p, clk.now())
	}
	if got := code(at("old", -121*time.Second)); got != wire.CodeStale {
		t.Fatalf("121s old: %q", got)
	}
	if got := code(at("ahead", 121*time.Second)); got != wire.CodeStale {
		t.Fatalf("121s ahead: %q", got)
	}
	if got := code(at("ok", -119*time.Second)); got != "" {
		t.Fatalf("119s old: %q", got)
	}
	if got := code(at("ok", 0)); got != wire.CodeReplay {
		t.Fatalf("same reqId: %q", got)
	}
	clk.advance(11 * time.Minute)
	if got := code(at("ok", 0)); got != "" {
		t.Fatalf("after window: %q", got)
	}
	// A stale request must not occupy a slot.
	if got := code(at("later", -200*time.Second)); got != wire.CodeStale {
		t.Fatalf("stale: %q", got)
	}
	if got := code(at("later", 0)); got != "" {
		t.Fatalf("fresh after stale: %q", got)
	}
}

func TestFleetReplayCacheFull(t *testing.T) {
	c := newReplayCache()
	now := newClock().now()
	for i := range replayCap {
		if r, f := c.check("p", string(rune('a'))+time.Duration(i).String(), now); r || f {
			t.Fatalf("slot %d: replay=%v full=%v", i, r, f)
		}
	}
	if _, full := c.check("p", "extra", now); !full {
		t.Fatal("want full")
	}
}

func TestFleetProvenanceOrderOfChecks(t *testing.T) {
	clk := newClock()
	now := clk.now().Unix()
	stale := now - 1000
	tests := []struct {
		name string
		p    wire.Prov
		want string
	}{
		{"mismatch beats cycle", wire.Prov{Origin: "A", Trace: "", Hops: []string{"A", "B"}, TS: stale}, wire.CodePrincipalMismatch},
		{"cycle beats hop_limit", wire.Prov{Origin: "B", Hops: []string{"B", "A"}, TS: stale}, wire.CodeCycle},
		{"hop_limit beats invalid", wire.Prov{Origin: "B", Hops: []string{"B", "X"}, TS: stale}, wire.CodeHopLimit},
		{"invalid beats stale", wire.Prov{Origin: "B", Hops: []string{"B"}, TS: stale}, wire.CodeInvalid},
		{"stale beats replay", wire.Prov{Origin: "B", Trace: "t", Hops: []string{"B"}, ReqID: "dup", TS: stale}, wire.CodeStale},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := newTestServer("A", clk)
			// Pre-record the reqId so replay would also apply.
			s.replay.check("B", "dup", clk.now())
			if got := code(s.checkProv("B", tt.p, clk.now())); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
	// Replay is reached only after everything else passed.
	s, _, _ := newTestServer("A", clk)
	s.replay.check("B", "dup", clk.now())
	p := wire.Prov{Origin: "B", Trace: "t", Hops: []string{"B"}, ReqID: "dup", TS: now}
	if got := code(s.checkProv("B", p, clk.now())); got != wire.CodeReplay {
		t.Fatalf("got %q, want replay", got)
	}
}
