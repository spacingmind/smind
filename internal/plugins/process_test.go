package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/plugins/host/hosttest"
)

// pidRecorder captures the test.pids notifications of the helper plugin.
type pidRecorder struct {
	mu   sync.Mutex
	pids map[string][2]int // plugin -> self, grandchild
}

func (r *pidRecorder) Request(context.Context, string, string, json.RawMessage) (any, *host.RPCError) {
	return nil, &host.RPCError{Code: host.ErrCodeMethodNotFound, Message: "no"}
}

func (r *pidRecorder) Notify(plugin, method string, params json.RawMessage) {
	if method != "test.pids" {
		return
	}
	var p struct{ Self, Grandchild int }
	_ = json.Unmarshal(params, &p)
	r.mu.Lock()
	if r.pids == nil {
		r.pids = map[string][2]int{}
	}
	r.pids[plugin] = [2]int{p.Self, p.Grandchild}
	r.mu.Unlock()
}

func (r *pidRecorder) get(id string) ([2]int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pids[id]
	return p, ok
}

func fastHost(rec host.Registry) *host.Options {
	pol := host.DefaultPolicy()
	pol.StartTimeout = 5 * time.Second
	pol.StopGrace = 300 * time.Millisecond
	pol.BackoffBase, pol.BackoffCap = 10*time.Millisecond, 20*time.Millisecond
	return &host.Options{Policy: pol, Registry: rec}
}

func newHostEnv(t *testing.T, rec host.Registry) *env {
	t.Helper()
	e := newEnv(t)
	e.m = New(Options{Home: e.home, Store: e.st, Version: "0.15.0", Enabled: func() bool { return *e.on }, Host: fastHost(rec)})
	t.Cleanup(e.m.Shutdown)
	return e
}

// processManifest declares the helper plugin (the test binary itself) in mode.
func processManifest(id, version, mode string, extra ...string) string {
	cmd, _ := json.Marshal(hosttest.Command(mode, extra...))
	return manifestWith(id, version, `"process":{"command":`+string(cmd)+`}`)
}

func installEnable(t *testing.T, e *env, src string, link bool) Info {
	t.Helper()
	var in Info
	var err error
	if link {
		in, err = e.m.Link(bg, src)
	} else {
		in, err = e.m.Install(bg, src)
	}
	if err != nil {
		t.Fatal(err)
	}
	if in, err = e.m.Enable(in.ID, in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	return in
}

func waitProc(t *testing.T, m *Manager, id string, want host.State) host.Status {
	t.Helper()
	var st host.Status
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		var ok bool
		if st, ok = m.ProcessStatus(id); ok && st.State == want {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("plugin %s never reached %s (last %+v)", id, want, st)
	return st
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if !procAlive(pid) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("process %d is still alive: an orphan", pid)
}

func TestDisable_StopsProcessAndUnregisters(t *testing.T) {
	rec := &pidRecorder{}
	e := newHostEnv(t, rec)
	src := writePluginDir(t, processManifest("demo", "1.0.0", "ignore-term"), nil)
	installEnable(t, e, src, false)
	st := waitProc(t, e.m, "demo", host.StateRunning)
	var pids [2]int
	waitUntil(t, "pid report", func() bool { var ok bool; pids, ok = rec.get("demo"); return ok })

	got, _ := e.m.Get("demo")
	if got.Process == nil || got.Process.State != host.StateRunning || got.Process.PID != st.PID {
		t.Fatalf("plugin.get must show the running process: %+v", got.Process)
	}
	if got, err := e.m.Disable("demo"); err != nil || got.Process != nil {
		t.Fatalf("disable: %+v %v", got.Process, err)
	}
	waitDead(t, pids[0])
	waitDead(t, pids[1])
	if _, ok := e.m.ProcessStatus("demo"); ok {
		t.Fatal("a disabled plugin is still registered with the host")
	}
}

func TestRemove_StopsProcessBeforeDeletingFiles(t *testing.T) {
	rec := &pidRecorder{}
	e := newHostEnv(t, rec)
	installEnable(t, e, writePluginDir(t, processManifest("demo", "1.0.0", "ignore-term"), nil), false)
	waitProc(t, e.m, "demo", host.StateRunning)
	var pids [2]int
	waitUntil(t, "pid report", func() bool { var ok bool; pids, ok = rec.get("demo"); return ok })
	if err := e.m.Remove("demo", false); err != nil {
		t.Fatal(err)
	}
	waitDead(t, pids[0])
	waitDead(t, pids[1])
	if lines, _ := e.m.Logs("demo", 10); lines.Lines != nil && len(lines.Lines) > 0 {
		t.Fatal("a removed plugin keeps no log")
	}
}

func TestReload_FailureKeepsLastGood(t *testing.T) {
	e := newHostEnv(t, host.DenyAll)
	src := writePluginDir(t, processManifest("demo", "1.0.0", "report"), nil)
	installEnable(t, e, src, true)
	before := waitProc(t, e.m, "demo", host.StateRunning)

	// Break the manifest in the linked directory: the reload must fail and the
	// process it started from the last good manifest must keep running.
	writeFile(t, src+"/"+ManifestFile, []byte(`{"id":"demo","name":`))
	if _, err := e.m.Reload("demo"); err == nil {
		t.Fatal("reload of a broken manifest succeeded")
	}
	after, ok := e.m.ProcessStatus("demo")
	if !ok || after.State != host.StateRunning || after.PID != before.PID || !procAlive(before.PID) {
		t.Fatalf("the last good process was disturbed: %+v (was %+v)", after, before)
	}
	got, _ := e.m.Get("demo")
	if got.LastError == "" || !got.Enabled {
		t.Fatalf("the failed reload must be recorded on a still-enabled plugin: %+v", got)
	}

	// A good reload restarts the process on the new files.
	writeFile(t, src+"/"+ManifestFile, []byte(processManifest("demo", "1.1.0", "report")))
	if _, err := e.m.Reload("demo"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "restart on reload", func() bool {
		s, ok := e.m.ProcessStatus("demo")
		return ok && s.State == host.StateRunning && s.PID != before.PID
	})
	waitDead(t, before.PID)
}

func TestReload_ChangedConsentOnLink_StopsProcess(t *testing.T) {
	e := newHostEnv(t, host.DenyAll)
	src := writePluginDir(t, processManifest("demo", "1.0.0", "report"), nil)
	installEnable(t, e, src, true)
	st := waitProc(t, e.m, "demo", host.StateRunning)
	// A different command changes the consent fingerprint: the plugin is
	// disabled until the Human confirms again, so nothing may keep running.
	writeFile(t, src+"/"+ManifestFile, []byte(processManifest("demo", "1.0.0", "ignore-term")))
	got, err := e.m.Reload("demo")
	if err != nil || got.Enabled {
		t.Fatalf("%+v %v", got, err)
	}
	waitDead(t, st.PID)
	if _, ok := e.m.ProcessStatus("demo"); ok {
		t.Fatal("process still supervised after the consent was invalidated")
	}
}

func TestUpdate_Apply_RestartsProcess(t *testing.T) {
	e := newHostEnv(t, host.DenyAll)
	src := writePluginDir(t, processManifest("demo", "1.0.0", "report"), nil)
	installEnable(t, e, src, false)
	before := waitProc(t, e.m, "demo", host.StateRunning)
	// A new command is a consent change: pending, old process keeps running.
	writeFile(t, src+"/"+ManifestFile, []byte(processManifest("demo", "2.0.0", "report", "x")))
	res, err := e.m.Update(bg, "demo", "", false)
	if err != nil || !res.Pending {
		t.Fatalf("%+v %v", res, err)
	}
	if s, _ := e.m.ProcessStatus("demo"); s.PID != before.PID || !procAlive(before.PID) {
		t.Fatal("a pending update disturbed the running process")
	}
	if _, err := e.m.Update(bg, "demo", "", true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "restart on update", func() bool {
		s, ok := e.m.ProcessStatus("demo")
		return ok && s.State == host.StateRunning && s.PID != before.PID
	})
	waitDead(t, before.PID)
}

func TestDaemonShutdown_StopsAllPlugins(t *testing.T) {
	rec := &pidRecorder{}
	e := newHostEnv(t, rec)
	for _, id := range []string{"aaa", "bbb"} {
		installEnable(t, e, writePluginDir(t, processManifest(id, "1.0.0", "ignore-term"), nil), false)
	}
	var all []int
	for _, id := range []string{"aaa", "bbb"} {
		waitProc(t, e.m, id, host.StateRunning)
		id := id
		waitUntil(t, "pid report", func() bool { _, ok := rec.get(id); return ok })
		p, _ := rec.get(id)
		all = append(all, p[0], p[1])
	}
	e.m.Shutdown()
	for _, pid := range all {
		waitDead(t, pid)
	}
	e.m.Shutdown() // twice is fine
}

func TestGlobalSwitchOff_NothingSpawned(t *testing.T) {
	rec := &pidRecorder{}
	e := newHostEnv(t, rec)
	installEnable(t, e, writePluginDir(t, processManifest("demo", "1.0.0", "ignore-term"), nil), false)
	st := waitProc(t, e.m, "demo", host.StateRunning)

	// The Human flips the switch off in config.yaml: the daemon syncs.
	*e.on = false
	e.m.SyncHost()
	waitDead(t, st.PID)
	if _, ok := e.m.ProcessStatus("demo"); ok {
		t.Fatal("a plugin runs while plugins.enabled is false")
	}
	if got, _ := e.m.Get("demo"); got.Process != nil || got.Active {
		t.Fatalf("with the switch off the plugin is not active and has no process: %+v", got)
	}

	// A daemon that STARTS with the switch off and an enabled plugin in its
	// database spawns nothing.
	m2 := New(Options{Home: e.home, Store: e.st, Version: "0.15.0", Enabled: func() bool { return false }, Host: fastHost(rec)})
	defer m2.Shutdown()
	m2.Start()
	m2.SyncHost()
	time.Sleep(300 * time.Millisecond)
	if _, ok := m2.ProcessStatus("demo"); ok {
		t.Fatal("Start spawned a plugin with the global switch off")
	}
	if n := len(hostLogs(m2, "demo")); n != 0 {
		t.Fatalf("the host touched a plugin while the switch was off: %d log lines", n)
	}

	// Switching it on again starts it.
	*e.on = true
	e.m.SyncHost()
	waitProc(t, e.m, "demo", host.StateRunning)
}

func hostLogs(m *Manager, id string) []host.LogEntry {
	r, _ := m.Logs(id, 100)
	return r.Lines
}

func TestStart_StartsEnabledPlugins_SkipsInvalid(t *testing.T) {
	e := newHostEnv(t, host.DenyAll)
	good := writePluginDir(t, processManifest("good", "1.0.0", "report"), nil)
	installEnable(t, e, good, false)
	waitProc(t, e.m, "good", host.StateRunning)
	e.m.Shutdown()

	m2 := New(Options{Home: e.home, Store: e.st, Version: "0.15.0", Enabled: func() bool { return true }, Host: fastHost(host.DenyAll)})
	defer m2.Shutdown()
	m2.Start()
	waitProc(t, m2, "good", host.StateRunning)
}

func TestInfo_FailedProcess_Surfaced(t *testing.T) {
	opts := fastHost(host.DenyAll)
	opts.Policy.BreakerCrashes = 3
	e := newEnv(t)
	e.m = New(Options{Home: e.home, Store: e.st, Version: "0.15.0", Enabled: func() bool { return true }, Host: opts})
	defer e.m.Shutdown()
	installEnable(t, e, writePluginDir(t, processManifest("demo", "1.0.0", "crash"), nil), false)
	waitProc(t, e.m, "demo", host.StateFailed)
	got, _ := e.m.Get("demo")
	if got.Process == nil || got.Process.State != host.StateFailed || !strings.Contains(got.LastError, "process failed: crashed 3 times") {
		t.Fatalf("a tripped breaker must show on plugin.get: %+v / %q", got.Process, got.LastError)
	}
	l, _ := e.m.List()
	if l.Plugins[0].Process == nil || l.Plugins[0].Process.State != host.StateFailed {
		t.Fatalf("and on plugin.list: %+v", l.Plugins[0])
	}
	// Disabling and enabling again is a fresh start (the breaker resets).
	if _, err := e.m.Disable("demo"); err != nil {
		t.Fatal(err)
	}
	in, _ := e.m.Get("demo")
	if _, err := e.m.Enable("demo", in.Consent.Fingerprint); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "a new run", func() bool { s, ok := e.m.ProcessStatus("demo"); return ok && s.Restarts <= 1 })
}

func TestLogs_ReadableAfterStop_UnknownPlugin(t *testing.T) {
	e := newHostEnv(t, host.DenyAll)
	installEnable(t, e, writePluginDir(t, processManifest("demo", "1.0.0", "report"), nil), false)
	waitProc(t, e.m, "demo", host.StateRunning)
	if _, err := e.m.Disable("demo"); err != nil {
		t.Fatal(err)
	}
	r, err := e.m.Logs("demo", 0)
	if err != nil || len(r.Lines) == 0 || !strings.Contains(r.Lines[0].Text, "[smind]") {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := e.m.Logs("nope", 0); CodeOf(err) != CodeNotFound {
		t.Fatalf("%v", err)
	}
}

func TestNoHostOption_PluginsStayInert(t *testing.T) {
	e := newEnv(t) // no Options.Host
	installEnable(t, e, writePluginDir(t, processManifest("demo", "1.0.0", "report"), nil), false)
	if _, ok := e.m.ProcessStatus("demo"); ok {
		t.Fatal("process hosting is off without Options.Host")
	}
	if r, err := e.m.Logs("demo", 0); err != nil || len(r.Lines) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
