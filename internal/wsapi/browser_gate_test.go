package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/browser/runsock"
	"github.com/spacingmind/smind/internal/listeners"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// recordingPermit is a permission gate stand-in that records what the host asked.
type recordingPermit struct {
	mu    sync.Mutex
	calls []runs.BrowserCall
	err   error
}

func (p *recordingPermit) permit(_ context.Context, c runs.BrowserCall) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, c)
	return p.err
}

func gateHost(t *testing.T) (*identEnv, *browserHost, *recordingPermit) {
	t.Helper()
	e := newIdentEnv(t, "hang")
	h := e.api.browser
	rp := &recordingPermit{}
	h.permit = rp.permit
	h.gate = func(int64) bool { return true }
	h.killSwitch = func() bool { return false }
	h.launcher = func() (browser.Launcher, error) { return nil, &browser.UnavailableError{InstallStep: "x"} }
	h.deny = func() ([]browser.Endpoint, error) { return nil, nil }
	h.liveDeny = nil
	return e, h, rp
}

// Every tool call goes through the gate before the broker; the gate is told the
// run and the tool; an open carries its URL and the host's status.
func TestGate_EveryCallPassesThePermissionGateFirst(t *testing.T) {
	t.Parallel()
	e, h, rp := gateHost(t)
	h.allow = func(int64) []string { return []string{"Intranet.Test"} }
	for _, tool := range []string{"browser_tabs", "browser_snapshot", "browser_close"} {
		if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, tool, `{}`); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_open", `{"url":"http://localhost:3000/a?token=S"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_open", `{"url":"http://intranet.test/x"}`); err != nil {
		t.Fatal(err)
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if len(rp.calls) != 5 {
		t.Fatalf("5 calls, 5 gate questions: %+v", rp.calls)
	}
	for _, c := range rp.calls {
		if c.RunID != e.runID {
			t.Fatalf("the gate is asked about the run the socket belongs to: %+v", c)
		}
	}
	if rp.calls[3].HostStatus != "localhost" || rp.calls[3].URL != "http://localhost:3000/a?token=S" {
		t.Fatalf("open on localhost: %+v", rp.calls[3])
	}
	if rp.calls[4].HostStatus != "added" || len(rp.calls[4].Allowlist) != 1 {
		t.Fatalf("open on an added host: %+v", rp.calls[4])
	}
}

// A refusal by the gate is the call's refusal: the broker never runs.
func TestGate_ARefusalNeverReachesTheBroker(t *testing.T) {
	t.Parallel()
	e, h, rp := gateHost(t)
	rp.err = errors.New("browser permission refused: the person said no")
	rb := &recordingBroker{}
	h.broker = rb
	_, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`)
	if err == nil || !strings.Contains(err.Error(), "the person said no") || strings.Contains(err.Error(), "permission refused: ") {
		t.Fatalf("refusal: %v", err)
	}
	if rb.calls != 0 {
		t.Fatal("the broker ran a refused call")
	}
}

// A URL the workspace policy refuses is refused with the policy's reason BEFORE
// any card: the person is never asked about a navigation that cannot happen.
func TestGate_ARefusedURLRaisesNoCard(t *testing.T) {
	t.Parallel()
	e, h, rp := gateHost(t)
	h.allow = func(int64) []string { return nil }
	for _, u := range []string{"http://169.254.169.254/latest/meta-data/", "http://example.com/", "file:///etc/passwd", "http://user:pw@localhost/"} {
		if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_open", `{"url":"`+u+`"}`); err == nil {
			t.Fatalf("%s must be refused", u)
		}
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if len(rp.calls) != 0 {
		t.Fatalf("a refused URL raised %d gate questions", len(rp.calls))
	}
}

// The live deny list (other listeners, linked daemons) refuses at once, even for
// a port the human allowlisted by IP, and before a card.
func TestGate_LiveDenyRefusesBeforeACardAndBeatsTheAllowlist(t *testing.T) {
	t.Parallel()
	e, h, rp := gateHost(t)
	h.allow = func(int64) []string { return []string{"10.1.2.3"} }
	live := []browser.Endpoint{{Host: "127.0.0.1", Port: 41234}, {Host: "10.1.2.3", Port: 8088}}
	h.liveDeny = func(context.Context) ([]browser.Endpoint, error) { return live, nil }
	for _, u := range []string{"http://localhost:41234/", "http://127.0.0.1:41234/x", "http://10.1.2.3:8088/"} {
		if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_open", `{"url":"`+u+`"}`); err == nil {
			t.Fatalf("%s is on the live deny list", u)
		}
	}
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_open", `{"url":"http://localhost:3000/"}`); err != nil {
		t.Fatalf("another loopback port stays allowed: %v", err)
	}
	// A listener that appears AFTER the first call is refused at once.
	live = append(live, browser.Endpoint{Host: unspecifiedV4, Port: 3000})
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_open", `{"url":"http://localhost:3000/"}`); err == nil {
		t.Fatal("a listener registered after the first call is refused at the next one")
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if len(rp.calls) != 1 {
		t.Fatalf("only the one allowed open reached the gate: %d", len(rp.calls))
	}
}

// The kill switch and a disabled workspace drop the run's grants; the
// settings change to OFF does too.
func TestGate_SwitchesDropGrants(t *testing.T) {
	t.Parallel()
	e, h, _ := gateHost(t)
	var revokedWS []int64
	allRevoked := 0
	h.revokeGrants = func(ws int64) { revokedWS = append(revokedWS, ws) }
	h.revokeAll = func() { allRevoked++ }
	h.killSwitch = func() bool { return true }
	_, _ = callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`)
	if allRevoked != 1 {
		t.Fatalf("the kill switch revokes every grant: %d", allRevoked)
	}
	h.killSwitch = func() bool { return false }
	h.gate = func(int64) bool { return false }
	_, _ = callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`)
	if len(revokedWS) != 1 {
		t.Fatalf("a workspace whose switch is off loses its grants: %v", revokedWS)
	}
	// settings.set enabled=false revokes that workspace's grants.
	hh, _, _ := settingsHost(t)
	var got []int64
	hh.revokeGrants = func(ws int64) { got = append(got, ws) }
	if _, err := setCall(hh, settingsHumanRC, `{"workspaceId":1,"enabled":true}`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("turning the switch ON revokes nothing")
	}
	if _, err := setCall(hh, settingsHumanRC, `{"workspaceId":1,"enabled":false}`); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("turning it OFF drops that workspace's grants: %v %v", got, err)
	}
}

// A finished run's grants and limiter are dropped.
func TestGate_AFinishedRunForgetsItsGrants(t *testing.T) {
	t.Parallel()
	e, h, _ := gateHost(t)
	var forgotten []string
	h.forgetRun = func(id string) { forgotten = append(forgotten, id) }
	h.socks = &fakeHub{}
	h.OnRunFinished(runs.RunFinished{RunID: "run-z"})
	if len(forgotten) != 1 || forgotten[0] != "run-z" {
		t.Fatalf("forgotten: %v", forgotten)
	}
	_ = e
}

// A held call must not be cut by the child: the child's deadline outlasts the
// permission hold plus the broker's own call budget.
func TestGate_ChildDeadlineOutlastsAHeldCall(t *testing.T) {
	if runsock.ChildCallDeadline < runs.BrowserGateWait()+browser.DefaultCallTimeout+10*time.Second {
		t.Fatalf("child deadline %v is shorter than the hold %v plus the call budget %v", runsock.ChildCallDeadline, runs.BrowserGateWait(), browser.DefaultCallTimeout)
	}
}

// Only a human can answer the card: a REAL card is pending, an identified run
// connection (what an agent's MCP child or shell can be) is refused by the
// dispatcher for every answer method, the card stays pending, and the human's own
// connection answers it.
func TestGate_OnlyAHumanAnswersACard(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	e.api.browser.gate = func(int64) bool { return true }
	res := make(chan error, 1)
	go func() {
		res <- e.api.Runs.BrowserGate(context.Background(), runs.BrowserCall{RunID: e.runID, Tool: "browser_tabs"})
	}()
	var reqID string
	for deadline := time.Now().Add(10 * time.Second); reqID == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, p := range e.api.Runs.PendingPermissions() {
			if p.RunID == e.runID && p.Detail != nil && p.Detail.Kind == "browser" {
				reqID = p.RequestID
			}
		}
	}
	if reqID == "" {
		t.Fatal("no card was raised")
	}
	agent := e.identified(t)
	for _, m := range []string{"run.respondPermission", "run.answerPermission"} {
		resp := callWS(t, agent, m, map[string]any{"runId": e.runID, "requestId": reqID, "optionId": "allow_run"})
		if resp.Error == nil || resp.Error.Code != "forbidden" {
			t.Fatalf("%s over an identified run connection: %+v / %+v, want forbidden", m, resp.Result, resp.Error)
		}
	}
	if e.api.Runs.BrowserGranted(e.runID, "browser_tabs") {
		t.Fatal("the agent's connection approved its own card")
	}
	stillPending := false
	for _, p := range e.api.Runs.PendingPermissions() {
		if p.RequestID == reqID {
			stillPending = true
		}
	}
	if !stillPending {
		t.Fatal("the card must stay pending after the refused answers")
	}
	// The human's own (unidentified) connection answers it.
	if resp := callWS(t, e.admin, "run.respondPermission", map[string]any{"runId": e.runID, "requestId": reqID, "optionId": "allow_run"}); resp.Error != nil {
		t.Fatalf("the human's answer: %+v", resp.Error)
	}
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("the human's answer admits the call: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the held call never finished")
	}
}

var _ = store.BrowserSettings{}

type fakeFleet struct {
	hosts []store.FleetHost
	links map[string][]store.FleetLink
	err   error
}

func (f fakeFleet) ListFleetHosts() ([]store.FleetHost, error) { return f.hosts, f.err }
func (f fakeFleet) ListFleetLinks(id string) ([]store.FleetLink, error) {
	return f.links[id], nil
}

// The live deny source: every smind listener file (live processes only) and the
// direct address of every fleet-linked daemon; bad entries are ignored.
func TestLiveDeny_ListenerFilesAndFleetLinks(t *testing.T) {
	dir := t.TempDir()
	rm, err := listeners.Register(dir, "relay", "127.0.0.1:41000")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listeners.Register(dir, "grpcweb", net.JoinHostPort("::", "41001")); err != nil {
		t.Fatal(err)
	}
	fleet := fakeFleet{
		hosts: []store.FleetHost{{HostID: "h1"}, {HostID: "h2"}},
		links: map[string][]store.FleetLink{
			"h1": {{HostID: "h1", TransportJSON: `{"kind":"direct","url":"wss://peer.example.test:7443/ws"}`}, {HostID: "h1", TransportJSON: `{"kind":"relay"}`}},
			"h2": {{HostID: "h2", TransportJSON: `{"kind":"direct","url":"ws://10.9.9.9/ws"}`}, {HostID: "h2", TransportJSON: `not json`}, {HostID: "h2", TransportJSON: `{"url":"ftp://x/"}`}},
		},
	}
	s := newLiveDenySource(dir, fleet)
	has := func(eps []browser.Endpoint, host string, port int) bool {
		for _, e := range eps {
			if e.Host == host && e.Port == port {
				return true
			}
		}
		return false
	}
	got, gerr := s.Endpoints()
	if gerr != nil {
		t.Fatal(gerr)
	}
	for _, want := range []struct {
		host string
		port int
	}{{"127.0.0.1", 41000}, {"::", 41001}, {"peer.example.test", 7443}, {"10.9.9.9", 80}} {
		if !has(got, want.host, want.port) {
			t.Errorf("missing %s:%d in %+v", want.host, want.port, got)
		}
	}
	if len(got) != 4 {
		t.Fatalf("only the four valid endpoints: %+v", got)
	}
	// The policy built from it refuses them, wildcard bind on every form.
	pol, err := browser.NewPolicy([]string{"10.9.9.9", "peer.example.test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pol = pol.WithLiveDeny(func() ([]browser.Endpoint, error) { return got, nil })
	for _, u := range []string{"http://127.0.0.1:41000/", "http://localhost:41001/", "http://[::1]:41001/", "http://10.9.9.9/", "https://peer.example.test:7443/"} {
		if pol.CheckURL(u).Allowed {
			t.Errorf("%s must be refused (the deny list beats the allowlist)", u)
		}
	}
	// A removed listener drops off (after the cache window); a dead process's file is ignored.
	rm()
	for deadline := time.Now().Add(5 * time.Second); has(epsOf(s), "127.0.0.1", 41000) && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond) // sleep-ok: polling past the half-second cache window
	}
	if has(epsOf(s), "127.0.0.1", 41000) {
		t.Fatal("a removed listener is no longer denied")
	}
	// A file of another (dead) process is ignored; this process's own listeners
	// still come from memory, and the fleet entries remain.
	if err := os.WriteFile(filepath.Join(dir, "999999-ghost-aaaaaaaa.json"), []byte(`{"name":"ghost","addr":"127.0.0.1:42000","pid":999999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := newLiveDenySource(dir, fleet)
	s2.alive = func(pid int) bool { return pid == os.Getpid() }
	if eps := epsOf(s2); has(eps, "127.0.0.1", 42000) || !has(eps, "::", 41001) || len(eps) != 3 {
		t.Fatalf("a dead process's listener file is ignored, this process's listener and the fleet entries stay: %+v", eps)
	}
	// A store failure leaves the listener files in force (s2's dead-process pass
	// removed the old files, so register again).
	if _, err := listeners.Register(dir, "grpcweb", net.JoinHostPort("::", "41001")); err != nil {
		t.Fatal(err)
	}
	s3 := newLiveDenySource(dir, fakeFleet{err: errors.New("db closed")})
	if eps := epsOf(s3); !has(eps, "::", 41001) {
		t.Fatalf("listener files still apply without the store: %+v", eps)
	}
}

func TestLiveDeny_TransportEndpointTable(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		`{"url":"wss://a.test:443/x"}`: "a.test:443", `{"url":"https://a.test"}`: "a.test:443", `{"url":"ws://a.test"}`: "a.test:80",
		`{"url":"http://[::1]:9/"}`: "::1:9",
	} {
		e, ok := transportEndpoint(raw)
		if !ok || e.Host+":"+strconv.Itoa(e.Port) != want {
			t.Errorf("%s -> %+v %v, want %s", raw, e, ok, want)
		}
	}
	for _, raw := range []string{"", "{", `{"url":""}`, `{"url":"ftp://a.test"}`, `{"url":"ws://a.test:0/"}`, `{"url":"ws://a.test:99999/"}`, `{"url":"://x"}`, `{"kind":"relay"}`} {
		if e, ok := transportEndpoint(raw); ok {
			t.Errorf("%q must yield nothing: %+v", raw, e)
		}
	}
}

// The whole path on the real wiring: the real registry's permission gate, the
// real hub and kernel credentials, the real broker (no Chrome installed): a call
// is HELD until a person answers the card through the registry's human path.
func TestBrowserE2E_AHeldCallIsAnsweredThroughTheRealRegistry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the browser's per-run socket is not offered on Windows")
	}
	e := newIdentEnv(t, "hang")
	h := e.api.browser
	task, err := e.db.GetTask(e.seatTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The workspace flag in the store stays OFF so the seat's own run (started by
	// newIdentEnv) is never offered the real browser row, which would replace this
	// test's socket; the host's gate is opened by hand instead.
	h.gate = func(int64) bool { return true }
	h.killSwitch = func() bool { return false }
	h.launcher = func() (browser.Launcher, error) {
		return nil, &browser.UnavailableError{InstallStep: "Install Chromium."}
	}
	dir, err := os.MkdirTemp("", "rs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	hub, err := runsock.NewSystemHub(filepath.Join(dir, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.CloseAll)
	h.socks = hub
	path, err := h.OpenBrowserSocket(e.runID, e.seatTask.ID, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	call := func(line string) <-chan runsock.Response {
		out := make(chan runsock.Response, 1)
		go func() {
			conn, err := runsock.Dial(context.Background(), path)
			if err != nil {
				out <- runsock.Response{Error: "dial: " + err.Error()}
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
			_, _ = conn.Write([]byte(line + "\n"))
			resp, err := runsock.ReadResponse(conn)
			if err != nil {
				resp = runsock.Response{Error: "read: " + err.Error()}
			}
			out <- resp
		}()
		return out
	}
	pending := func() string {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			for _, p := range e.api.Runs.PendingPermissions() {
				if p.RunID == e.runID && p.Detail != nil && p.Detail.Kind == "browser" {
					return p.RequestID
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("no browser card was raised")
		return ""
	}
	// 1. A read is HELD: nothing answers until the person does.
	first := call(`{"tool":"browser_tabs"}`)
	id := pending()
	select {
	case r := <-first:
		t.Fatalf("the call was answered before the card: %+v", r)
	case <-time.After(300 * time.Millisecond):
	}
	if err := e.api.Runs.RespondPermission(e.runID, id, "allow_run"); err != nil {
		t.Fatal(err)
	}
	if r := <-first; r.Error != "" || !strings.Contains(string(r.Result.(json.RawMessage)), "no browser installed") {
		t.Fatalf("after the answer the call reaches the broker: %+v", r)
	}
	// 2. The grant admits the next read with no card.
	second := call(`{"tool":"browser_snapshot"}`)
	if r := <-second; r.Error != "" {
		t.Fatalf("a granted read: %+v", r)
	}
	for _, p := range e.api.Runs.PendingPermissions() {
		if p.RunID == e.runID {
			t.Fatal("a granted read must not raise a card")
		}
	}
	// 3. Opening a page asks, and a denial is an error reply.
	open := call(`{"tool":"browser_open","args":{"url":"http://localhost:3000/x?token=S"}}`)
	id2 := pending()
	for _, p := range e.api.Runs.PendingPermissions() {
		if p.RequestID == id2 && (strings.Contains(p.Summary, "token=") || p.Detail.Browser.URL != "http://localhost:3000/x") {
			t.Fatalf("the card shows the query or a wrong URL: %q %+v", p.Summary, p.Detail.Browser)
		}
	}
	if err := e.api.Runs.RespondPermission(e.runID, id2, "deny"); err != nil {
		t.Fatal(err)
	}
	if r := <-open; !strings.Contains(r.Error, "the person said no") {
		t.Fatalf("a denial: %+v", r)
	}
	// 4. Turning the workspace switch OFF through the human-only settings call ends the grant.
	if !e.api.Runs.BrowserGranted(e.runID, "browser_snapshot") {
		t.Fatal("test setup: the read grant is held")
	}
	if _, err := setCall(h, settingsHumanRC, `{"workspaceId":`+strconv.FormatInt(task.WorkspaceID, 10)+`,"enabled":false}`); err != nil {
		t.Fatal(err)
	}
	if e.api.Runs.BrowserGranted(e.runID, "browser_snapshot") {
		t.Fatal("the grant ended with the switch")
	}
	h.gate = func(int64) bool { return false }
	if r := <-call(`{"tool":"browser_snapshot"}`); !strings.Contains(r.Error, "not enabled for this workspace") {
		t.Fatalf("after the switch is off: %+v", r)
	}
}

// The gate may hold a call for a person: if the workspace switch or the kill
// switch goes off while it is held, the call ends instead of running.
func TestGate_SwitchesAreReadAgainAfterTheGateReturns(t *testing.T) {
	t.Parallel()
	e, h, _ := gateHost(t)
	rb := &recordingBroker{}
	h.broker = rb
	h.permit = func(context.Context, runs.BrowserCall) error {
		h.gate = func(int64) bool { return false } // turned off while the card was open
		return nil
	}
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "not enabled for this workspace") {
		t.Fatalf("workspace switch off meanwhile: %v", err)
	}
	h.gate = func(int64) bool { return true }
	ended := 0
	h.endAllSessions = func() { ended++ }
	h.permit = func(context.Context, runs.BrowserCall) error {
		h.killSwitch = func() bool { return true }
		return nil
	}
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "disabled by the daemon") || ended != 1 {
		t.Fatalf("kill switch on meanwhile: %v ended=%d", err, ended)
	}
	if rb.calls != 0 {
		t.Fatal("the broker ran a call whose switch went off while it was held")
	}
}

// The kill switch drops every grant and ends every session on the TRANSITION, not
// only when some call notices; a grant answered while it is on dies at the next
// tick too. (A flip on and back off between two polls with no call is not seen:
// stated in the plan.)
func TestKillSwitchWatch_RevokesOnTheTransition(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	on := false
	revoked, ended := 0, 0
	h := &browserHost{
		revokeAll:      func() { mu.Lock(); revoked++; mu.Unlock() },
		endAllSessions: func() { mu.Lock(); ended++; mu.Unlock() },
	}
	h.startKillSwitchWatch(10*time.Millisecond, func() bool { mu.Lock(); defer mu.Unlock(); return on })
	t.Cleanup(func() { close(h.stopWatch) })
	time.Sleep(60 * time.Millisecond) // sleep-ok: the watcher must NOT act while the switch stays off
	mu.Lock()
	if revoked != 0 || ended != 0 {
		mu.Unlock()
		t.Fatalf("nothing happens while the switch is off: revoked=%d ended=%d", revoked, ended)
	}
	on = true
	mu.Unlock()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) { // sleep-ok: polling
		mu.Lock()
		done := revoked >= 3 && ended == 1
		mu.Unlock()
		if done {
			return
		}
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("the transition ends the sessions once and keeps revoking while on: revoked=%d ended=%d", revoked, ended)
}

// epsOf is Endpoints without the error, for assertions about the entries.
func epsOf(s *liveDenySource) []browser.Endpoint {
	eps, _ := s.Endpoints()
	return eps
}

// An unreadable or flooded listener directory FAILS CLOSED: no session and no
// call (never "no entries"), the policy refuses every address, and this
// process's own listeners stay reserved from memory even when their files are
// deleted.
func TestLiveDeny_UnreadableDirectoryFailsClosedAndLocalListenersSurviveDeletion(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := newLiveDenySource(file, nil) // a regular file where the directory should be
	if _, err := s.Endpoints(); err == nil {
		t.Fatal("an unreadable listener directory must be an error")
	}
	pol, err := browser.NewPolicy(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pol = pol.WithLiveDeny(s.Endpoints)
	for _, u := range []string{"http://localhost:3000/", "http://127.0.0.1:8080/x"} {
		if pol.CheckURL(u).Allowed {
			t.Fatalf("%s must be refused while the reserved set cannot be read", u)
		}
	}
	// The host refuses a session and a call, not just the URLs.
	e, h, _ := gateHost(t)
	h.liveDeny = s.EndpointsContext
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "listener records cannot be read") {
		t.Fatalf("a call with an unreadable listener directory: %v", err)
	}
	// This process's own listeners come from memory: deleting the file changes nothing.
	dir := t.TempDir()
	rm, err := listeners.Register(dir, "oauth-callback", "127.0.0.1:43999")
	if err != nil {
		t.Fatal(err)
	}
	defer rm()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	s2 := newLiveDenySource(dir, nil)
	eps, err := s2.Endpoints()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ep := range eps {
		if ep.Host == "127.0.0.1" && ep.Port == 43999 {
			found = true
		}
	}
	if !found {
		t.Fatalf("a listener registered by this process stays reserved after its file and directory were deleted: %+v", eps)
	}
}

// Many concurrent decisions trigger ONE refresh (single flight), not one per caller.
func TestLiveDeny_RefreshIsSingleFlight(t *testing.T) {
	dir := t.TempDir()
	var reads int32
	var mu sync.Mutex
	s := newLiveDenySource(dir, nil)
	s.alive = func(int) bool { mu.Lock(); reads++; mu.Unlock(); return true }
	rm, err := listeners.Register(dir, "relay", "127.0.0.1:43998")
	if err != nil {
		t.Fatal(err)
	}
	defer rm()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Endpoints() }()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if reads != 1 {
		t.Fatalf("50 concurrent decisions read the directory %d times, want exactly one refresh", reads)
	}
}

// A panic in the refresh must not wedge every later decision: it fails closed once,
// releases its waiters, and the next call works. A waiter whose context ends leaves
// instead of waiting for the refresh.
func TestLiveDeny_APanicingRefreshReleasesTheFlightAndWaitersHonourContext(t *testing.T) {
	dir := t.TempDir()
	s := newLiveDenySource(dir, nil)
	rm, err := listeners.Register(dir, "relay", "127.0.0.1:43997")
	if err != nil {
		t.Fatal(err)
	}
	defer rm()
	// 1. A panic in the refresh: an error (fail closed), no deadlock, then recovery.
	panics := true
	s.alive = func(int) bool {
		if panics {
			panic("boom")
		}
		return true
	}
	if _, err := s.Endpoints(); err == nil {
		t.Fatal("a panicking refresh must fail closed with an error")
	}
	panics = false
	done := make(chan error, 1)
	go func() { _, err := s.Endpoints(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the refresh after a panic works: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a panic in the refresh left the flight set: every later call deadlocks")
	}
	// 2. A waiter leaves with its context while a refresh is stuck.
	time.Sleep(liveDenyTTL + 50*time.Millisecond) // sleep-ok: past the cache window so the next call refreshes
	block := make(chan struct{})
	started := make(chan struct{})
	s.alive = func(int) bool { close(started); <-block; return true }
	go func() { _, _ = s.Endpoints() }() // the leader, stuck in alive()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := s.EndpointsContext(ctx); err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("a waiter must leave when its context ends: %v after %v", err, time.Since(start))
	}
	close(block)
}

// pendingBrowserCard waits for the run's browser card and returns its request id.
func pendingBrowserCard(t *testing.T, e *identEnv) string {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, p := range e.api.Runs.PendingPermissions() {
			if p.RunID == e.runID && p.Detail != nil && p.Detail.Kind == "browser" {
				return p.RequestID
			}
		}
	}
	t.Fatal("no card was raised")
	return ""
}

// Closing the run's socket (the single path run end and revoke both use) ends the
// run's grants even though the run itself goes on.
func TestClosingTheSocketDropsTheRunsGrants(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	res := make(chan error, 1)
	go func() {
		res <- e.api.Runs.BrowserGate(context.Background(), runs.BrowserCall{RunID: e.runID, Tool: "browser_tabs"})
	}()
	if err := e.api.Runs.RespondPermission(e.runID, pendingBrowserCard(t, e), "allow_run"); err != nil {
		t.Fatal(err)
	}
	if err := <-res; err != nil {
		t.Fatal(err)
	}
	if !e.api.Runs.BrowserGranted(e.runID, "browser_tabs") {
		t.Fatal("the read grant was given")
	}
	e.api.browser.CloseBrowserSocket(e.runID)
	if e.api.Runs.BrowserGranted(e.runID, "browser_tabs") {
		t.Fatal("closing the run's socket must end its grants")
	}
	if !e.api.Runs.RunIsRunning(e.runID) {
		t.Fatal("the run itself goes on")
	}
}

// A card held for a run whose socket closes: the call ends with the socket's
// context, the card leaves the inbox, and an answer taken before the close leaves
// no grant.
func TestClosingTheSocketEndsAHeldCard(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ctx, cancel := context.WithCancel(context.Background()) // the socket's context
	res := make(chan error, 1)
	go func() {
		res <- e.api.Runs.BrowserGate(ctx, runs.BrowserCall{RunID: e.runID, Tool: "browser_tabs"})
	}()
	pendingBrowserCard(t, e)
	e.api.browser.CloseBrowserSocket(e.runID)
	cancel()
	select {
	case err := <-res:
		if err == nil {
			t.Fatal("the held call must not be admitted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the held call never ended")
	}
	for _, p := range e.api.Runs.PendingPermissions() {
		if p.RunID == e.runID && p.Detail != nil && p.Detail.Kind == "browser" {
			t.Fatalf("the card %s stays pending after the socket closed", p.RequestID)
		}
	}
	if e.api.Runs.BrowserGranted(e.runID, "browser_tabs") {
		t.Fatal("no grant results")
	}
}
