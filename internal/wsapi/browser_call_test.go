package wsapi

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestBrowserCall_IsRunScopedAndNotHumanOnly(t *testing.T) {
	e := newProcEnv(t)
	for _, m := range browserCallMethods {
		if _, ok := e.api.hs[m]; !ok {
			t.Fatalf("%s is not a wsapi method", m)
		}
		if !slices.Contains(RunScopedMethods, m) {
			t.Errorf("%s must be on RunScopedMethods: it is the agent's own RPC", m)
		}
		if slices.Contains(permpolicy.HumanOnlyRPCs, m) {
			t.Errorf("%s grants nothing and must not be on the human-only table", m)
		}
		if !pluginNever(m) {
			t.Errorf("%s is reachable from a plugin session", m)
		}
		for capName, ms := range PluginCapabilityTable {
			for _, pm := range ms {
				if pm.Name == m {
					t.Errorf("%s is granted by plugin capability %q", m, capName)
				}
			}
		}
		// An unidentified (human or CLI) caller and a nil context are refused inside the handler.
		for _, rc := range []*requestContext{nil, {local: true}} {
			if _, err := e.api.hs[m](context.Background(), rc, json.RawMessage(`{"tool":"browser_tabs"}`)); err == nil {
				t.Errorf("%s accepted a caller that is not an identified run", m)
			}
		}
	}
}

func TestBrowserCall_UnidentifiedConnectionIsRefused(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	wantForbidden(t, callWS(t, e.admin, "browser.call", map[string]any{"tool": "browser_tabs"}), "only available to an agent run")
}

func TestBrowserCall_DaemonRechecksTheGateOnEveryCall(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	// The browser is dark by default (gate closed, no workspace switch): a child that somehow runs is refused.
	wantForbidden(t, callWS(t, ws, "browser.call", map[string]any{"tool": "browser_tabs"}), "not enabled for this workspace")
	// Open for this test only: it now reaches the broker.
	e.api.browser.gate = func(int64) bool { return true }
	e.api.browser.launcher = func() (browser.Launcher, error) {
		return nil, &browser.UnavailableError{InstallStep: "Install Chromium."}
	}
	resp := callWS(t, ws, "browser.call", map[string]any{"tool": "browser_tabs"})
	wantOK(t, resp, "browser.call with the gate open")
	if !strings.Contains(string(resp.Result), "no browser installed") {
		t.Fatalf("the plain install text is the answer when there is no browser: %s", resp.Result)
	}
	// And closed again: refused again, per call.
	e.api.browser.gate = func(int64) bool { return false }
	wantForbidden(t, callWS(t, ws, "browser.call", map[string]any{"tool": "browser_tabs"}), "not enabled for this workspace")
}

func TestBrowserCall_ArgsCannotChooseRunOrWorkspace(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	var sessions atomic.Int32
	e.api.browser.gate = func(int64) bool { return true }
	e.api.browser.launcher = func() (browser.Launcher, error) {
		sessions.Add(1)
		return nil, &browser.UnavailableError{InstallStep: "x"}
	}
	// A runId or workspace beside tool/args is an invalid request.
	for _, params := range []map[string]any{
		{"tool": "browser_tabs", "runId": "another"},
		{"tool": "browser_tabs", "workspaceId": 99},
		{"tool": "browser_tabs", "args": map[string]any{}, "workspace": "x"},
	} {
		resp := callWS(t, ws, "browser.call", params)
		if resp.Error == nil || resp.Error.Code != codeInvalid {
			t.Fatalf("%v: %+v / %+v, want invalid params", params, resp.Result, resp.Error)
		}
	}
	// Inside args they are unknown arguments, refused before any session starts.
	for _, args := range []map[string]any{{"runId": "another"}, {"workspaceId": 99}, {"workspace": "x"}} {
		resp := callWS(t, ws, "browser.call", map[string]any{"tool": "browser_tabs", "args": args})
		wantOK(t, resp, "browser.call with a foreign argument")
		if !strings.Contains(string(resp.Result), `"isError":true`) || !strings.Contains(string(resp.Result), "unknown argument") {
			t.Fatalf("args %v: %s", args, resp.Result)
		}
	}
	if sessions.Load() != 0 {
		t.Fatalf("a refused argument must never start a session: %d", sessions.Load())
	}
}

func TestBrowserCall_RecordsOnTheRunTimelineAndEndsWithTheRun(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	ws := e.identified(t)
	e.api.browser.gate = func(int64) bool { return true }
	e.api.browser.launcher = func() (browser.Launcher, error) { return nil, &browser.UnavailableError{InstallStep: "x"} }
	callWS(t, ws, "browser.call", map[string]any{"tool": "browser_open", "args": map[string]any{"url": "http://localhost:3000/a?token=SECRET"}})
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

type recordingBroker struct{ ended []string }

func (b *recordingBroker) Call(context.Context, browser.Run, string, json.RawMessage) browser.Result {
	return browser.Result{}
}
func (b *recordingBroker) EndRun(id string) { b.ended = append(b.ended, id) }

func TestBrowserHost_AFinishedRunEndsItsBrowserSession(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	rb := &recordingBroker{}
	e.api.browser.broker = rb
	e.api.browser.OnRunFinished(runs.RunFinished{RunID: "run-x"})
	if len(rb.ended) != 1 || rb.ended[0] != "run-x" {
		t.Fatalf("a finished run must end its browser session: %v", rb.ended)
	}
}
