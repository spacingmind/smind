package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/browser/runsock"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// There is no browser RPC on the websocket at all: the child reaches the
// browser only through its run's own socket, so no identified connection can.
func TestBrowserCall_IsNotAWebsocketMethod(t *testing.T) {
	e := newProcEnv(t)
	for _, m := range []string{"browser.call", "browser.socket", "browser.open"} {
		if _, ok := e.api.hs[m]; ok {
			t.Errorf("%s is a wsapi method: the browser must not be reachable over the websocket", m)
		}
		if slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s is on RunScopedMethods", m)
		}
	}
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "browser.") {
			t.Errorf("%s: no browser method may be run-scoped", m)
		}
	}
}

func TestBrowserCall_AnIdentifiedConnectionCannotCallTheBrowser(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	for _, tool := range []string{"browser_tabs", "browser_open"} {
		resp := callWS(t, ws, "browser.call", map[string]any{"tool": tool})
		if resp.Error == nil {
			t.Fatalf("browser.call over an identified connection answered: %s", resp.Result)
		}
	}
	e.api.browser.gate = func(int64) bool { return true } // even with every gate open
	if resp := callWS(t, ws, "browser.call", map[string]any{"tool": "browser_tabs"}); resp.Error == nil {
		t.Fatal("no gate setting makes the websocket a browser path")
	}
}

func callRunJSON(t *testing.T, h *browserHost, runID string, taskID int64, tool string, args string) (string, error) {
	t.Helper()
	res, err := h.callRun(context.Background(), runID, taskID, tool, json.RawMessage(args))
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

func TestBrowserCall_DaemonRechecksTheGateOnEveryCall(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	h := e.api.browser
	tid := e.seatTask.ID
	// Dark by default (gate closed, no workspace switch): refused.
	if _, err := callRunJSON(t, h, e.runID, tid, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "not enabled for this workspace") {
		t.Fatalf("dark by default: %v", err)
	}
	// Open for this test only: it now reaches the broker.
	h.gate = func(int64) bool { return true }
	h.launcher = func() (browser.Launcher, error) {
		return nil, &browser.UnavailableError{InstallStep: "Install Chromium."}
	}
	out, err := callRunJSON(t, h, e.runID, tid, "browser_tabs", `{}`)
	if err != nil || !strings.Contains(out, "no browser installed") {
		t.Fatalf("the plain install text is the answer when there is no browser: %s %v", out, err)
	}
	// And closed again: refused again, per call.
	h.gate = func(int64) bool { return false }
	if _, err := callRunJSON(t, h, e.runID, tid, "browser_tabs", `{}`); err == nil {
		t.Fatal("the gate is read per call")
	}
	// An ended run is refused, and an unknown task.
	h.gate = func(int64) bool { return true }
	h.live = func(string) bool { return false }
	if _, err := callRunJSON(t, h, e.runID, tid, "browser_tabs", `{}`); err == nil || !strings.Contains(err.Error(), "the run has ended") {
		t.Fatalf("an ended run: %v", err)
	}
	h.live = func(string) bool { return true }
	if _, err := callRunJSON(t, h, e.runID, 987654, "browser_tabs", `{}`); err == nil {
		t.Fatal("an unknown task is refused")
	}
}

// The socket's handler is bound to the run and task it was opened for: a request
// cannot name another, and a foreign argument never starts a session.
func TestBrowserHost_TheSocketHandlerIsBoundToItsRun(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	h := e.api.browser
	hub := &fakeHub{}
	h.socks = hub
	var sessions atomic.Int32
	h.gate = func(int64) bool { return true }
	h.launcher = func() (browser.Launcher, error) {
		sessions.Add(1)
		return nil, &browser.UnavailableError{InstallStep: "x"}
	}
	path, err := h.OpenBrowserSocket(e.runID, e.seatTask.ID, 4242)
	if err != nil || path != "/sock/"+e.runID {
		t.Fatalf("open: %q %v", path, err)
	}
	if hub.openedRun != e.runID || hub.openedRoot != 4242 {
		t.Fatalf("the socket is opened for this run and its root pid: %+v", hub)
	}
	// Inside args a run or workspace is an unknown argument, refused before any session starts.
	for _, args := range []string{`{"runId":"another"}`, `{"workspaceId":99}`, `{"workspace":"x"}`} {
		res, err := hub.handler(context.Background(), runsock.Request{Tool: "browser_tabs", Args: json.RawMessage(args)})
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		b, _ := json.Marshal(res)
		if !strings.Contains(string(b), `"isError":true`) || !strings.Contains(string(b), "unknown argument") {
			t.Fatalf("args %s: %s", args, b)
		}
	}
	if sessions.Load() != 0 {
		t.Fatalf("a refused argument must never start a session: %d", sessions.Load())
	}
}

type fakeHub struct {
	openedRun  string
	openedRoot int
	handler    runsock.Handler
	closed     []string
	closedAll  bool
	err        error
}

func (f *fakeHub) Open(runID string, rootPID int, h runsock.Handler) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.openedRun, f.openedRoot, f.handler = runID, rootPID, h
	return "/sock/" + runID, nil
}
func (f *fakeHub) OpenPending(runID string, h runsock.Handler) (string, func(int) error, error) {
	if f.err != nil {
		return "", nil, f.err
	}
	f.openedRun, f.handler = runID, h
	return "/sock/" + runID, func(pid int) error { f.openedRoot = pid; return nil }, nil
}
func (f *fakeHub) Close(runID string) { f.closed = append(f.closed, runID) }
func (f *fakeHub) CloseAll()          { f.closedAll = true }

func TestBrowserHost_OpenFailsWithoutAHubOrWhenTheHubFails(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	h := e.api.browser
	h.socks = nil
	if _, err := h.OpenBrowserSocket("r", 1, 2); err == nil {
		t.Fatal("no socket host means no socket")
	}
	h.socks = &fakeHub{err: errors.New("no dir")}
	if _, err := h.OpenBrowserSocket("r", 1, 2); err == nil {
		t.Fatal("a hub error is an error (the runner then delivers no row)")
	}
	h.CloseBrowserSocket("r") // no panic
}

func TestBrowserCall_RecordsOnTheRunTimeline(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	h := e.api.browser
	h.gate = func(int64) bool { return true }
	h.launcher = func() (browser.Launcher, error) { return nil, &browser.UnavailableError{InstallStep: "x"} }
	if _, err := callRunJSON(t, h, e.runID, e.seatTask.ID, "browser_open", `{"url":"http://localhost:3000/a?token=SECRET"}`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var note string
	for time.Now().Before(deadline) && note == "" {
		hist, _, err := e.api.Runs.History(e.runID)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range hist {
			if ev.Type == taskrunner.EventTypeSessionNote && strings.HasPrefix(ev.Text, "browser browser_open") {
				note = ev.Text
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if note == "" {
		t.Fatal("every call must leave a line in the run's timeline")
	}
	if strings.Contains(note, "SECRET") || !strings.Contains(note, "-> error") {
		t.Fatalf("the line carries the outcome and no query: %q", note)
	}
}

func TestBrowserCall_ImagesAreReadOnlyWhereTheCatalogueSaysSo(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang") // glm: Images "no"
	if e.api.browser.imagesRead(e.runID) {
		t.Fatal("glm cannot read images")
	}
	if e.api.browser.imagesRead("no-such-run") {
		t.Fatal("an unknown run reads no images")
	}
}

func TestImagesReadFrom(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bool{"yes": true, "no": false, "unknown": false, "": false, "YES": false, "true": false} {
		if got := imagesReadFrom(in); got != want {
			t.Errorf("imagesReadFrom(%q) = %v, want %v", in, got, want)
		}
	}
}

type recordingBroker struct {
	ended []string
	calls int
}

func (b *recordingBroker) Call(context.Context, browser.Run, string, json.RawMessage) browser.Result {
	b.calls++
	return browser.Result{}
}
func (b *recordingBroker) EndRun(id string) { b.ended = append(b.ended, id) }

func TestBrowserHost_AFinishedRunEndsItsSocketAndBrowserSession(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	rb := &recordingBroker{}
	e.api.browser.broker = rb
	hub := &fakeHub{}
	e.api.browser.socks = hub
	e.api.browser.OnRunFinished(runs.RunFinished{RunID: "run-x"})
	if len(hub.closed) != 1 || hub.closed[0] != "run-x" {
		t.Fatalf("a finished run's socket must be closed: %v", hub.closed)
	}
	if len(rb.ended) != 1 || rb.ended[0] != "run-x" {
		t.Fatalf("a finished run must end its browser session: %v", rb.ended)
	}
	e.api.browser.close()
	if !hub.closedAll {
		t.Fatal("daemon shutdown closes every socket")
	}
}

// The whole path with a real hub and the kernel's real peer credentials: this
// process is the run's root, so it is in the tree; the call runs through the
// shared core, and a run whose gate closes is refused with an error reply.
func TestBrowserHost_EndToEndThroughARealSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the browser's per-run socket is not offered on Windows")
	}
	e := newIdentEnv(t, "hang")
	h := e.api.browser
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
	open := true
	h.gate = func(int64) bool { return open }
	h.launcher = func() (browser.Launcher, error) {
		return nil, &browser.UnavailableError{InstallStep: "Install Chromium."}
	}
	path, err := h.OpenBrowserSocket(e.runID, e.seatTask.ID, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	call := func() runsock.Response {
		conn, err := runsock.Dial(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write([]byte(`{"tool":"browser_tabs"}` + "\n")); err != nil {
			t.Fatal(err)
		}
		resp, err := runsock.ReadResponse(conn)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if r := call(); r.Error != "" || !strings.Contains(string(r.Result.(json.RawMessage)), "no browser installed") {
		t.Fatalf("a call through the socket reaches the broker: %+v", r)
	}
	open = false
	if r := call(); !strings.Contains(r.Error, "not enabled for this workspace") {
		t.Fatalf("the gate is re-read per call on the socket path: %+v", r)
	}
	h.CloseBrowserSocket(e.runID)
	if _, err := runsock.Dial(context.Background(), path); err == nil {
		t.Fatal("the socket is gone when the run's socket is closed")
	}
}

// The wiring in server.go: the registry's socket host is the browser host, so a
// delivered browser row opens a real per-run socket (and nothing else can).
func TestBrowserWiring_RegistrySocketHostIsTheBrowserHost(t *testing.T) {
	e := newProcEnv(t)
	if e.api.Runs.BrowserSocketHost() != runs.BrowserSockets(e.api.browser) {
		t.Fatal("the registry must open run sockets through the browser host")
	}
}

// Building the API never touches the disk for the browser: the socket directory
// is created on the first Open only, so tests (and daemons that never run a
// browser) cannot create or sweep anything in a real home, and the test home is
// a temp directory, never the developer's ~/.smind.
func TestBrowserHostConstructionTouchesNoDiskAndTheTestHomeIsNotTheRealOne(t *testing.T) {
	e := newProcEnv(t)
	home := config.Dir()
	if real, err := os.UserHomeDir(); err == nil && strings.HasPrefix(home, filepath.Join(real, ".smind")) {
		t.Fatalf("the test home is the developer's real home: %s", home)
	}
	if _, err := os.Lstat(filepath.Join(home, "browser", "sock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructing the API created the socket directory: %v", err)
	}
	_ = e
}
