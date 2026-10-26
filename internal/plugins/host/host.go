package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Options configures a Host. Every field has a safe zero value.
type Options struct {
	Policy Policy // zero: DefaultPolicy()
	Clock  Clock  // zero: the real clock
	// Registry answers plugin-to-host calls (nil: DenyAll).
	Registry Registry
	// Redact scrubs a log line before it is stored (nil: none).
	Redact func(string) string
	// Gate is asked before every (re)start; false means "do not spawn" (the
	// global plugins.enabled switch). nil: always allowed.
	Gate func() bool
	// OnChange is told, asynchronously, that a plugin's process status moved.
	OnChange func(id string)
	// Environ is the daemon environment the child's is built from (nil:
	// os.Environ).
	Environ func() []string
}

// Host supervises the processes of all plugins.
type Host struct {
	pol    Policy
	clock  Clock
	reg    Registry
	redact func(string) string
	gate   func() bool
	env    func() []string
	note   *notifier

	opMu sync.Mutex // serializes Ensure/Restart/Stop/StopAll
	mu   sync.Mutex
	sups map[string]*supervisor
	logs map[string]*Log
}

// New returns a Host. It starts nothing until Ensure.
func New(o Options) *Host {
	h := &Host{pol: o.Policy, clock: o.Clock, reg: o.Registry, redact: o.Redact, gate: o.Gate, env: o.Environ,
		sups: map[string]*supervisor{}, logs: map[string]*Log{}}
	if h.pol == (Policy{}) {
		h.pol = DefaultPolicy()
	}
	if h.clock == nil {
		h.clock = realClock{}
	}
	if h.reg == nil {
		h.reg = DenyAll
	}
	if h.env == nil {
		h.env = os.Environ
	}
	if o.OnChange != nil {
		h.note = newNotifier(o.OnChange)
	}
	return h
}

func (h *Host) allowed() bool { return h.gate == nil || h.gate() }

func (h *Host) changed(id string) {
	if h.note != nil {
		h.note.add(id)
	}
}

func (h *Host) logFor(id string) *Log {
	h.mu.Lock()
	defer h.mu.Unlock()
	l := h.logs[id]
	if l == nil {
		l = newLog(h.pol.Log, h.redact, h.clock.Now)
		h.logs[id] = l
	}
	return l
}

// Ensure makes sure a supervisor runs spec: nothing happens when one already
// runs (or has failed on) the same spec, and a different spec replaces it. A
// failed supervisor stays failed until Restart, Stop or a different spec.
func (h *Host) Ensure(spec Spec) { h.start(spec, false) }

// Restart replaces the supervisor with a fresh one (reload, update, and the
// way out of the failed state).
func (h *Host) Restart(spec Spec) { h.start(spec, true) }

func (h *Host) start(spec Spec, force bool) {
	h.opMu.Lock()
	defer h.opMu.Unlock()
	if !h.allowed() {
		h.stopLocked(spec.ID)
		return
	}
	h.mu.Lock()
	old := h.sups[spec.ID]
	h.mu.Unlock()
	if old != nil {
		if !force && old.spec.equal(spec) {
			return
		}
		h.stopLocked(spec.ID)
	}
	s := newSupervisor(h, spec)
	h.mu.Lock()
	h.sups[spec.ID] = s
	h.mu.Unlock()
	go s.run()
}

// Stop ends a plugin's process and returns once it and everything it spawned
// are gone.
func (h *Host) Stop(id string) {
	h.opMu.Lock()
	defer h.opMu.Unlock()
	h.stopLocked(id)
}

func (h *Host) stopLocked(id string) {
	h.mu.Lock()
	s := h.sups[id]
	delete(h.sups, id)
	h.mu.Unlock()
	if s != nil {
		s.stop()
		h.changed(id)
	}
}

// StopAll stops every plugin (daemon shutdown), in parallel.
func (h *Host) StopAll() {
	h.opMu.Lock()
	defer h.opMu.Unlock()
	h.mu.Lock()
	all := h.sups
	h.sups = map[string]*supervisor{}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func() { defer wg.Done(); s.stop() }()
	}
	wg.Wait()
}

// Close stops everything and the change notifier.
func (h *Host) Close() {
	h.StopAll()
	if h.note != nil {
		h.note.close()
	}
}

// Forget drops a plugin's log (it was removed).
func (h *Host) Forget(id string) {
	h.mu.Lock()
	delete(h.logs, id)
	h.mu.Unlock()
}

// Status reports a plugin's process, false when none is supervised.
func (h *Host) Status(id string) (Status, bool) {
	h.mu.Lock()
	s := h.sups[id]
	h.mu.Unlock()
	if s == nil {
		return Status{}, false
	}
	return s.status(), true
}

// Logs returns up to limit of the newest log lines (limit <= 0: all held).
func (h *Host) Logs(id string, limit int) []LogEntry {
	h.mu.Lock()
	l := h.logs[id]
	h.mu.Unlock()
	if l == nil {
		return nil
	}
	return l.Lines(limit)
}

// ErrNotRunning is returned by Call when the plugin has no running process.
var ErrNotRunning = errors.New("the plugin process is not running")

// Call sends a host-to-plugin request to a running plugin.
func (h *Host) Call(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	h.mu.Lock()
	s := h.sups[id]
	h.mu.Unlock()
	if s == nil {
		return nil, ErrNotRunning
	}
	p := s.current()
	if p == nil {
		return nil, ErrNotRunning
	}
	return p.Call(ctx, method, params)
}

// Logf adds a `[smind]` line to a plugin's log (redacted like every line), for
// what the host did on the plugin's behalf: a hook result, a refused send.
func (h *Host) Logf(id, format string, a ...any) { h.logFor(id).Hostf(format, a...) }

// Notify sends a host-to-plugin notification to a running plugin.
func (h *Host) Notify(id, method string, params any) error {
	h.mu.Lock()
	s := h.sups[id]
	h.mu.Unlock()
	if s == nil {
		return ErrNotRunning
	}
	p := s.current()
	if p == nil {
		return ErrNotRunning
	}
	return p.Notify(method, params)
}

type supervisor struct {
	h    *Host
	spec Spec
	log  *Log

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu  sync.Mutex
	st  Status
	cur *Process
}

func newSupervisor(h *Host, spec Spec) *supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &supervisor{h: h, spec: spec, log: h.logFor(spec.ID), ctx: ctx, cancel: cancel, done: make(chan struct{}),
		st: Status{State: StateStarting, Since: h.clock.Now()}}
}

func (s *supervisor) status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

func (s *supervisor) current() *Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.State != StateRunning {
		return nil
	}
	return s.cur
}

func (s *supervisor) set(f func(*Status)) {
	s.mu.Lock()
	f(&s.st)
	s.mu.Unlock()
	s.h.changed(s.spec.ID)
}

// stop ends the supervisor and its process, and waits for both.
func (s *supervisor) stop() {
	s.cancel()
	<-s.done
}

// dropSelf removes a supervisor that gave up on its own (the gate closed).
func (s *supervisor) dropSelf() {
	s.h.mu.Lock()
	if s.h.sups[s.spec.ID] == s {
		delete(s.h.sups, s.spec.ID)
	}
	s.h.mu.Unlock()
	s.h.changed(s.spec.ID)
}

func (s *supervisor) run() {
	defer close(s.done)
	pol, clk := s.h.pol, s.h.clock
	var crashes []time.Time
	consecutive := 0
	for {
		if s.ctx.Err() != nil {
			return
		}
		if !s.h.allowed() {
			s.log.Hostf("plugins are turned off: not starting")
			s.dropSelf()
			return
		}
		s.set(func(st *Status) { st.State, st.PID, st.Since, st.RetryAt = StateStarting, 0, clk.Now(), time.Time{} })
		started := clk.Now()
		reason := s.runOnce()
		if s.ctx.Err() != nil {
			return
		}
		now := clk.Now()
		if now.Sub(started) >= pol.StableAfter {
			crashes, consecutive = nil, 0
		}
		kept := crashes[:0]
		for _, t := range crashes {
			if now.Sub(t) < pol.BreakerWindow {
				kept = append(kept, t)
			}
		}
		crashes = append(kept, now)
		consecutive++
		s.log.Hostf("crashed: %s", reason)
		if len(crashes) >= pol.BreakerCrashes {
			msg := fmt.Sprintf("crashed %d times within %s; last: %s", len(crashes), pol.BreakerWindow, reason)
			s.log.Hostf("giving up: %s", msg)
			s.set(func(st *Status) {
				st.State, st.PID, st.Since, st.RetryAt, st.LastError = StateFailed, 0, now, time.Time{}, msg
				st.Restarts++
			})
			return
		}
		delay := pol.BackoffBase
		for i := 1; i < consecutive && delay < pol.BackoffCap; i++ {
			delay *= 2
		}
		if delay > pol.BackoffCap {
			delay = pol.BackoffCap
		}
		s.log.Hostf("restarting in %s", delay)
		s.set(func(st *Status) {
			st.State, st.PID, st.Since, st.RetryAt, st.LastError = StateRestarting, 0, now, now.Add(delay), reason
			st.Restarts++
		})
		select {
		case <-clk.After(delay):
		case <-s.ctx.Done():
			return
		}
	}
}

// runOnce starts the process, waits for the handshake and then for the process
// to end or the supervisor to be stopped. It returns why the run ended ("" when
// it was stopped). The process group is swept on every path.
func (s *supervisor) runOnce() string {
	p, err := startProcess(s.spec, s.h.env(), s.h.pol, s.h.reg, s.log)
	if err != nil {
		s.log.Hostf("start failed: %v", err)
		return "start failed: " + err.Error()
	}
	defer p.Stop()
	if err := p.initialize(s.ctx); err != nil {
		if s.ctx.Err() != nil {
			return ""
		}
		return "start failed: " + err.Error()
	}
	s.mu.Lock()
	s.cur = p
	s.mu.Unlock()
	s.set(func(st *Status) {
		st.State, st.PID, st.Since, st.LastError = StateRunning, p.PID(), s.h.clock.Now(), ""
	})
	defer func() {
		s.mu.Lock()
		s.cur = nil
		s.mu.Unlock()
	}()
	select {
	case <-p.Done():
		return p.exitReason()
	case <-s.ctx.Done():
		return ""
	}
}

// notifier delivers change ids to one callback on its own goroutine, so a
// callback that takes the manager's lock can never deadlock a supervisor that
// the manager is waiting for.
type notifier struct {
	fn      func(string)
	mu      sync.Mutex
	pending map[string]struct{}
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}

	closeOnce sync.Once
}

func newNotifier(fn func(string)) *notifier {
	n := &notifier{fn: fn, pending: map[string]struct{}{}, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go n.loop()
	return n
}

func (n *notifier) add(id string) {
	n.mu.Lock()
	n.pending[id] = struct{}{}
	n.mu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *notifier) loop() {
	defer close(n.done)
	for {
		select {
		case <-n.stop:
			return
		case <-n.wake:
		}
		n.mu.Lock()
		ids := n.pending
		n.pending = map[string]struct{}{}
		n.mu.Unlock()
		for id := range ids {
			n.fn(id)
		}
	}
}

func (n *notifier) close() {
	n.closeOnce.Do(func() { close(n.stop) })
	<-n.done
}
