package wsapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Tests for the per-run mcp_servers event (ADR-0018's per-run indicator,
// M4/M5): the run.logs entry, the run.attach stream event, and the
// raw-bytes secret audit. Modeled on mcp_test.go's harness (real API
// server, fake ACP agent, mcp.create).

// newMcpRunEnv wires a real API server whose GLM runner carries a real
// MCP source against db, plus a task and one open connection. Not
// parallel: it sets FAKEAGENT_MCP_CAPS.
func newMcpRunEnv(t *testing.T, caps string) (*wsenv, store.Task) {
	t.Helper()
	t.Setenv("FAKEAGENT_MCP_CAPS", caps)
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := taskrunner.New(wm,
		taskrunner.WithACPCommand(taskrunner.ProviderGLM, []string{fakeACPAgentPath}),
		taskrunner.WithMcpServers(mcpservers.New(db)),
	)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	t.Cleanup(srv.Close)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })
	return &wsenv{srv: srv, db: db, ws: ws}, task
}

// wsenv bundles one live API server, its store, and an open connection.
type wsenv struct {
	srv *httptest.Server
	db  *store.Store
	ws  *websocket.Conn
}

// runPromptCollectFrames drives a task.prompt turn to completion and
// returns the run's ID plus every raw wire frame the connection received
// (streamed events and the terminal response alike), so tests can assert
// against bytes.
func (e *wsenv) runPromptCollectFrames(t *testing.T, taskID int64) (string, [][]byte) {
	t.Helper()
	sendRequest(t, e.ws, "p", "task.prompt", map[string]any{
		"taskId": taskID, "provider": "glm", "prompt": "hi",
	})
	var (
		runID  string
		frames [][]byte
		term   bool
	)
	deadline := time.Now().Add(10 * time.Second)
	for !term {
		data := nextRawMessage(t, e.ws, time.Until(deadline))
		frames = append(frames, data)
		var env envelope
		if json.Unmarshal(data, &env) != nil || env.ID != "p" || len(env.Result) == 0 {
			continue // a streamed event, not the terminal response
		}
		if env.Error != nil {
			t.Fatalf("task.prompt error: %v", env.Error.Message)
		}
		var result taskPromptResult
		if err := json.Unmarshal(env.Result, &result); err != nil {
			t.Fatalf("decode task.prompt result %s: %v", data, err)
		}
		runID, term = result.RunID, true
	}
	return runID, frames
}

// attachCollectFrames attaches to a finished runID on a second connection
// and returns every raw frame until the terminal response.
func (e *wsenv) attachCollectFrames(t *testing.T, runID string) [][]byte {
	t.Helper()
	ws := dialWS(t, e.srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })
	sendRequest(t, ws, "a", "run.attach", map[string]any{"runId": runID})
	var frames [][]byte
	deadline := time.Now().Add(10 * time.Second)
	for {
		data := nextRawMessage(t, ws, time.Until(deadline))
		frames = append(frames, data)
		var env envelope
		if json.Unmarshal(data, &env) != nil || env.ID != "a" || len(env.Result) == 0 {
			continue // a backfilled event, not the terminal response
		}
		if env.Error != nil {
			t.Fatalf("run.attach error: %v", env.Error.Message)
		}
		return frames
	}
}

// seedMcpServers creates the standard pair: an active stdio server and an
// http server the stdio-only fakeagent drops (transport_unsupported).
func seedMcpServers(t *testing.T, db *store.Store) {
	t.Helper()
	reg := mcpservers.New(db)
	if _, err := reg.Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "go", Enabled: true}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "remote", Transport: "http", URL: "https://r/mcp", Enabled: true}); err != nil {
		t.Fatalf("Create(remote): %v", err)
	}
}

// TestRunLogs_McpServersEntry (M4): after the run, run.logs carries an
// entry {type:"mcp_servers", mcpServers:[...]} with the exact states.
func TestRunLogs_McpServersEntry(t *testing.T) {
	env, task := newMcpRunEnv(t, "stdio")
	seedMcpServers(t, env.db)
	runID, _ := env.runPromptCollectFrames(t, task.ID)

	sendRequest(t, env.ws, "logs", "run.logs", map[string]any{"runId": runID})
	resp := readEnvelopeFor(t, env.ws, "logs", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.logs error: %v", resp.Error.Message)
	}
	var result struct {
		Events []struct {
			Type       string                      `json:"type"`
			McpServers []taskrunner.McpServerState `json:"mcpServers"`
		} `json:"events"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode run.logs result: %v", err)
	}
	var entries []taskrunner.McpServerState
	for _, e := range result.Events {
		if e.Type == "mcp_servers" {
			entries = append(entries, e.McpServers...)
		}
	}
	want := []taskrunner.McpServerState{
		{Name: "pw", Transport: "stdio", Status: "active"},
		{Name: "remote", Transport: "http", Status: "dropped", Reason: "transport_unsupported"},
	}
	if len(entries) != 2 || entries[0] != want[0] || entries[1] != want[1] {
		t.Fatalf("run.logs mcp_servers entries = %+v, want %+v", entries, want)
	}
}

// TestRunAttach_McpServersStreamEvent (M4): a late run.attach backfills
// the mcp_servers event as a stream "mcp_servers" frame with the expected
// params.
func TestRunAttach_McpServersStreamEvent(t *testing.T) {
	env, task := newMcpRunEnv(t, "stdio")
	seedMcpServers(t, env.db)
	runID, _ := env.runPromptCollectFrames(t, task.ID)

	attachFrames := env.attachCollectFrames(t, runID)
	found := false
	for _, frame := range attachFrames {
		var env0 struct {
			Event  string          `json:"event"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(frame, &env0) != nil || env0.Event != "mcp_servers" {
			continue
		}
		var p runMcpServersParams
		if err := json.Unmarshal(env0.Params, &p); err != nil {
			t.Fatalf("decode mcp_servers params %s: %v", env0.Params, err)
		}
		want := []taskrunner.McpServerState{
			{Name: "pw", Transport: "stdio", Status: "active"},
			{Name: "remote", Transport: "http", Status: "dropped", Reason: "transport_unsupported"},
		}
		if len(p.McpServers) != 2 || p.McpServers[0] != want[0] || p.McpServers[1] != want[1] {
			t.Fatalf("mcp_servers params = %+v, want %+v", p.McpServers, want)
		}
		found = true
	}
	if !found {
		for _, f := range attachFrames {
			t.Logf("frame: %s", f)
		}
		t.Fatal("no mcp_servers stream event in run.attach frames")
	}
}

// TestMcpServersEvent_NoSecretReachesWire (M5): with distinctive secret
// strings seeded into env and header values, args, command and url of a
// stdio and an http server (both active, so nothing is echoed in a
// note), none of those secrets -- nor the command/url/args -- appear in
// the raw bytes of the run.logs response, the run.attach stream frames,
// or the persisted run_events.event_data rows, while the server names
// do. Asserted against raw bytes, not decoded structs.
func TestMcpServersEvent_NoSecretReachesWire(t *testing.T) {
	env, task := newMcpRunEnv(t, "stdio,http")
	reg := mcpservers.New(env.db)
	if _, err := reg.Create(store.McpServer{
		Name: "pw", Transport: "stdio", Command: "go",
		Args: `["ARGSECRET99"]`, Env: `{"TOKEN":"ENVSECRET99"}`, Enabled: true,
	}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}
	if _, err := reg.Create(store.McpServer{
		Name: "remote", Transport: "http", URL: "https://URLSECRET99/mcp",
		Headers: `{"Authorization":"Bearer HDRSECRET99"}`, Enabled: true,
	}); err != nil {
		t.Fatalf("Create(remote): %v", err)
	}

	runID, frames := env.runPromptCollectFrames(t, task.ID)
	attachFrames := env.attachCollectFrames(t, runID)

	sendRequest(t, env.ws, "logs", "run.logs", map[string]any{"runId": runID})
	logsRaw, err := json.Marshal(readEnvelopeFor(t, env.ws, "logs", 5*time.Second))
	if err != nil {
		t.Fatalf("marshal run.logs response: %v", err)
	}

	events, err := env.db.ListRunEvents(runID)
	if err != nil {
		t.Fatalf("ListRunEvents(): %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no persisted run_events rows")
	}

	leaks := []string{"ENVSECRET99", "HDRSECRET99", "ARGSECRET99", "URLSECRET99", "Bearer"}
	for _, frame := range append(append(append([][]byte{}, frames...), attachFrames...), logsRaw) {
		for _, leak := range leaks {
			if strings.Contains(string(frame), leak) {
				t.Fatalf("wire frame %s leaked %q", frame, leak)
			}
		}
	}
	var sawMcp bool
	for _, row := range events {
		for _, leak := range leaks {
			if strings.Contains(row.EventData, leak) {
				t.Fatalf("run_events row %d leaked %q: %s", row.Seq, leak, row.EventData)
			}
		}
		if strings.Contains(row.EventData, `"mcpServers"`) {
			sawMcp = true
		}
	}
	if !sawMcp {
		t.Fatal("no persisted mcp_servers event row")
	}
	for _, name := range []string{`"pw"`, `"remote"`} {
		if !strings.Contains(string(logsRaw), name) {
			t.Fatalf("run.logs response %s missing server name %s", logsRaw, name)
		}
	}
}
