package link

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/fleet/hello"
	"github.com/spacingmind/smind/internal/fleet/identity"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsclient"
)

const tinyMax = 20 * time.Millisecond

type mgrEnv struct {
	reg    *registry.Registry
	self   *identity.Key
	other  *identity.Key
	hostID string

	mu      sync.Mutex
	dials   int
	callees []*fakeCallee
	// script customises the callee for the n-th (0-based) dial.
	script func(n int, f *fakeCallee)
}

func newMgrEnv(t *testing.T) *mgrEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &mgrEnv{reg: registry.New(st, 10), self: newKey(t), other: newKey(t)}
	tr := Transport{Kind: KindDirect, URL: "ws://127.0.0.1:1"}
	h, err := e.reg.PairAsInitiator(e.other.Public(), "box", "", tr.JSON())
	if err != nil {
		t.Fatal(err)
	}
	e.hostID = h.HostID
	return e
}

func (e *mgrEnv) dial(_ context.Context, _ Transport, _ string) (Conn, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f := newCallee(e.other)
	if e.script != nil {
		e.script(e.dials, f)
	}
	e.dials++
	e.callees = append(e.callees, f)
	return f, nil
}

func (e *mgrEnv) dialCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dials
}

func (e *mgrEnv) callee(i int) *fakeCallee {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.callees[i]
}

func (e *mgrEnv) manager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(ManagerOptions{
		Key: e.self, Registry: e.reg, Dial: e.dial, Version: "test",
		BackoffMin: time.Millisecond, BackoffMax: tinyMax, ApprovalPoll: 5 * time.Millisecond,
	})
	t.Cleanup(m.Close)
	return m
}

func waitStatus(t *testing.T, m *Manager, id string, want State) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Status(id); s.State == want {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("status never reached %s, last %+v", want, m.Status(id))
	return Status{}
}

func TestManagerReconnectsAndChecksVersionEachTime(t *testing.T) {
	e := newMgrEnv(t)
	e.script = func(n int, f *fakeCallee) {
		if n == 1 {
			f.fleetV = 2
		}
	}
	m := e.manager(t)
	m.Ensure(e.hostID)
	waitStatus(t, m, e.hostID, StateConnected)
	e.callee(0).Close()
	s := waitStatus(t, m, e.hostID, StateRefused)
	if s.Code != hello.CodeVersionUnsupported || s.Detail == "" {
		t.Fatalf("status %+v", s)
	}
	if calls := e.callee(1).calls; len(calls) != 1 || calls[0] != hello.MethodChallenge {
		t.Fatalf("calls on the skewed peer: %v", calls)
	}
	waitStatus(t, m, e.hostID, StateConnected)
}

func TestManagerAwaitingApprovalThenApproved(t *testing.T) {
	e := newMgrEnv(t)
	e.script = func(n int, f *fakeCallee) {
		if n == 0 {
			f.helloErr = &wsclient.RPCError{Message: "pending", Code: hello.CodeAwaitingApproval}
		}
	}
	m := e.manager(t)
	m.Ensure(e.hostID)
	waitStatus(t, m, e.hostID, StateAwaitingApproval)
	waitStatus(t, m, e.hostID, StateConnected)
	h, err := e.reg.Get(e.hostID)
	if err != nil || h.State != registry.StateApproved {
		t.Fatalf("host %+v err %v", h, err)
	}
}

func TestManagerDropClosesConn(t *testing.T) {
	e := newMgrEnv(t)
	m := e.manager(t)
	m.Ensure(e.hostID)
	waitStatus(t, m, e.hostID, StateConnected)
	if _, ok := m.Conn(e.hostID); !ok {
		t.Fatal("no conn while connected")
	}
	m.Drop(e.hostID)
	select {
	case <-e.callee(0).Done():
	case <-time.After(2 * time.Second):
		t.Fatal("conn not closed by Drop")
	}
	if s := m.Status(e.hostID); s.State != StateDown || s.Detail != "not linked" {
		t.Fatalf("status %+v", s)
	}
	if _, ok := m.Conn(e.hostID); ok {
		t.Fatal("conn after Drop")
	}
}

func TestManagerIdentityChangedStops(t *testing.T) {
	e := newMgrEnv(t)
	impostor := newKey(t)
	e.script = func(_ int, f *fakeCallee) { f.key = impostor }
	m := e.manager(t)
	m.Ensure(e.hostID)
	s := waitStatus(t, m, e.hostID, StateRefused)
	if s.Code != hello.CodePeerIdentityChanged {
		t.Fatalf("status %+v", s)
	}
	time.Sleep(5 * tinyMax)
	if n := e.dialCount(); n != 1 {
		t.Fatalf("dials = %d, want 1", n)
	}
}

func TestManagerOutboundDisabledDoesNotDial(t *testing.T) {
	e := newMgrEnv(t)
	if err := e.reg.SetOutboundEnabled(e.hostID, false); err != nil {
		t.Fatal(err)
	}
	m := e.manager(t)
	m.Ensure(e.hostID)
	deadline := time.Now().Add(2 * time.Second)
	for m.Status(e.hostID).Detail != "outbound disabled" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s := m.Status(e.hostID); s.State != StateDown || s.Detail != "outbound disabled" {
		t.Fatalf("status %+v", s)
	}
	time.Sleep(3 * tinyMax)
	if n := e.dialCount(); n != 0 {
		t.Fatalf("dials = %d, want 0", n)
	}
}

func TestManagerTestIsOneShot(t *testing.T) {
	e := newMgrEnv(t)
	m := e.manager(t)
	s, _, err := m.Test(context.Background(), e.hostID)
	if err != nil || s.State != StateConnected {
		t.Fatalf("status %+v err %v", s, err)
	}
	select {
	case <-e.callee(0).Done():
	default:
		t.Fatal("test connection left open")
	}
	if st := m.Status(e.hostID); st.Detail != "not linked" {
		t.Fatalf("Test touched the loop state: %+v", st)
	}
	time.Sleep(3 * tinyMax)
	if n := e.dialCount(); n != 1 {
		t.Fatalf("dials = %d, want 1", n)
	}
}
