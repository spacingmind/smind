package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/store"
)

// staticMcpCaps is an mcpCapableBackend with fixed transport support.
type staticMcpCaps struct {
	stdio, http, sse bool
}

func (c staticMcpCaps) SupportsMcpStdio() bool { return c.stdio }
func (c staticMcpCaps) SupportsMcpHttp() bool  { return c.http }
func (c staticMcpCaps) SupportsMcpSSE() bool   { return c.sse }

func stdioRow(name, command string) store.McpServer {
	return store.McpServer{Name: name, Transport: "stdio", Command: command, Args: `["--flag"]`, Env: `{"B":"2","A":"1"}`, Enabled: true}
}

// TestAcpMcpServers_MapsStdioAndHttpWireShape proves each transport maps
// to its ACP wire shape: args as an array, env/headers as arrays of
// {name,value} objects sorted by key for determinism.
func TestAcpMcpServers_MapsStdioAndHttpWireShape(t *testing.T) {
	t.Parallel()
	all := staticMcpCaps{stdio: true, http: true, sse: true}
	look := func(string) (string, error) { return "/resolved/bin", nil }

	rows := []store.McpServer{
		stdioRow("playwright", "/abs/npx"),
		{Transport: "http", Name: "pplx", URL: "https://mcp.pplx.ai/mcp", Headers: `{"Authorization":"Bearer t"}`, Enabled: true},
		{Transport: "sse", Name: "legacy", URL: "https://old/mcp", Headers: `{"X":"1"}`, Enabled: true},
	}
	wire, inactive, err := acpMcpServers(rows, all, look)
	if err != nil {
		t.Fatalf("acpMcpServers() error = %v", err)
	}
	if len(inactive) != 0 {
		t.Fatalf("inactive = %v, want none", inactive)
	}
	if len(wire) != 3 {
		t.Fatalf("wire = %v, want 3 entries", wire)
	}
	wantStdio := `{"type":"stdio","name":"playwright","command":"/abs/npx","args":["--flag"],"env":[{"name":"A","value":"1"},{"name":"B","value":"2"}]}`
	if got, _ := json.Marshal(wire[0]); string(got) != wantStdio {
		t.Fatalf("stdio entry = %s, want %s", got, wantStdio)
	}
	wantHTTP := `{"type":"http","name":"pplx","url":"https://mcp.pplx.ai/mcp","headers":[{"name":"Authorization","value":"Bearer t"}]}`
	if got, _ := json.Marshal(wire[1]); string(got) != wantHTTP {
		t.Fatalf("http entry = %s, want %s", got, wantHTTP)
	}
	wantSSE := `{"type":"sse","name":"legacy","url":"https://old/mcp","headers":[{"name":"X","value":"1"}]}`
	if got, _ := json.Marshal(wire[2]); string(got) != wantSSE {
		t.Fatalf("sse entry = %s, want %s", got, wantSSE)
	}
}

// TestAcpMcpServers_DropsTransportAgentDoesNotAdvertise: a v2-shaped
// agent advertising only http drops the stdio server (named in inactive)
// and keeps the http one; a v1 agent advertising nothing keeps stdio
// (the v1 baseline) and drops http.
func TestAcpMcpServers_DropsTransportAgentDoesNotAdvertise(t *testing.T) {
	t.Parallel()
	look := func(string) (string, error) { return "/resolved/bin", nil }
	rows := []store.McpServer{
		stdioRow("playwright", "/abs/npx"),
		{Transport: "http", Name: "pplx", URL: "https://mcp.pplx.ai/mcp", Enabled: true},
	}

	t.Run("v2 agent advertising only http", func(t *testing.T) {
		t.Parallel()
		wire, inactive, err := acpMcpServers(rows, staticMcpCaps{http: true}, look)
		if err != nil {
			t.Fatalf("acpMcpServers() error = %v", err)
		}
		if len(wire) != 1 {
			t.Fatalf("wire = %v, want only the http entry", wire)
		}
		if got, _ := json.Marshal(wire[0]); !strings.Contains(string(got), `"name":"pplx"`) {
			t.Fatalf("wire[0] = %s, want pplx", got)
		}
		if len(inactive) != 1 || inactive[0] != "playwright" {
			t.Fatalf("inactive = %v, want [playwright]", inactive)
		}
	})

	// A v1 agent advertising nothing reports stdio support through
	// acp.Client.SupportsMcpStdio's baseline (true) with http false; the
	// filter here just honors whatever the backend reports.
	t.Run("v1 agent advertising nothing keeps stdio, drops http", func(t *testing.T) {
		t.Parallel()
		wire, inactive, err := acpMcpServers(rows, staticMcpCaps{stdio: true}, look)
		if err != nil {
			t.Fatalf("acpMcpServers() error = %v", err)
		}
		if len(wire) != 1 {
			t.Fatalf("wire = %v, want only the stdio entry (v1 baseline)", wire)
		}
		if got, _ := json.Marshal(wire[0]); !strings.Contains(string(got), `"name":"playwright"`) {
			t.Fatalf("wire[0] = %s, want playwright", got)
		}
		if len(inactive) != 1 || inactive[0] != "pplx" {
			t.Fatalf("inactive = %v, want [pplx]", inactive)
		}
	})

	t.Run("agent advertising nothing yields non-nil empty wire", func(t *testing.T) {
		t.Parallel()
		wire, inactive, err := acpMcpServers([]store.McpServer{stdioRow("s", "/x"), {Transport: "http", Name: "h", URL: "u", Enabled: true}}, staticMcpCaps{}, look)
		if err != nil {
			t.Fatalf("acpMcpServers() error = %v", err)
		}
		if len(wire) != 0 {
			t.Fatalf("wire = %v, want empty", wire)
		}
		if wire == nil {
			t.Fatal("wire = nil, want non-nil []any{}")
		}
		if len(inactive) != 2 {
			t.Fatalf("inactive = %v, want both names", inactive)
		}
	})
}

// TestAcpMcpServers_ResolvesBareCommandToAbsolutePath: a bare stdio
// command goes through lookPath and comes out absolute -- both via an
// injected lookPath and via the real PATH with a temp executable.
func TestAcpMcpServers_ResolvesBareCommandToAbsolutePath(t *testing.T) {

	t.Run("injected lookPath", func(t *testing.T) {
		wire, _, err := acpMcpServers([]store.McpServer{stdioRow("pw", "npx")}, staticMcpCaps{stdio: true}, func(cmd string) (string, error) {
			if cmd != "npx" {
				t.Errorf("lookPath called with %q, want npx", cmd)
			}
			return "relative/npx", nil // deliberately relative: Abs must lift it
		})
		if err != nil {
			t.Fatalf("acpMcpServers() error = %v", err)
		}
		cmd := wire[0].(acp.McpServerStdio).Command
		if !filepath.IsAbs(cmd) {
			t.Fatalf("command = %q, want absolute", cmd)
		}
	})

	t.Run("real PATH", func(t *testing.T) {
		t.Setenv("PATH", "") // silence parallel+Setenv; reset below
		dir := t.TempDir()
		exe := filepath.Join(dir, "fakenpx")
		if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write fakenpx: %v", err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		wire, _, err := acpMcpServers([]store.McpServer{stdioRow("pw", "fakenpx")}, staticMcpCaps{stdio: true}, exec.LookPath)
		if err != nil {
			t.Fatalf("acpMcpServers() error = %v", err)
		}
		cmd := wire[0].(acp.McpServerStdio).Command
		if cmd != exe {
			t.Fatalf("command = %q, want %q", cmd, exe)
		}
	})
}

// TestAcpMcpServers_UnresolvableCommandErrorsWithoutLeakingSecrets: a
// bare command not on PATH fails with an error naming only the server
// name and command -- never env/args values like SUPERSECRET.
func TestAcpMcpServers_UnresolvableCommandErrorsWithoutLeakingSecrets(t *testing.T) {
	t.Parallel()
	row := store.McpServer{
		Name:      "playwright",
		Transport: "stdio",
		Command:   "no-such-command-xyz",
		Args:      `["SUPERSECRET"]`,
		Env:       `{"TOKEN":"SUPERSECRET"}`,
		Enabled:   true,
	}
	_, _, err := acpMcpServers([]store.McpServer{row}, staticMcpCaps{stdio: true}, exec.LookPath)
	if err == nil {
		t.Fatal("acpMcpServers() error = nil, want unresolvable-command error")
	}
	if !strings.Contains(err.Error(), "playwright") || !strings.Contains(err.Error(), "no-such-command-xyz") {
		t.Fatalf("error = %v, want it to name the server and command", err)
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Fatalf("error = %v, leaked a secret", err)
	}
}

// newMcpTestEnv wires a Runner against a real store-backed MCP source and
// the fake ACP agent, with FAKEAGENT_MCP_DUMP/CAPS set. Not usable from
// parallel tests: it mutates the process environment (t.Setenv).
func newMcpTestEnv(t *testing.T, mcpCaps string, agentArgs ...string) (*Runner, *store.Store, store.Task, string) {
	t.Helper()
	t.Setenv("FAKEAGENT_MCP_CAPS", mcpCaps)
	dump := filepath.Join(t.TempDir(), "mcp-dump.json")
	t.Setenv("FAKEAGENT_MCP_DUMP", dump)

	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)

	r := New(wm, WithMcpServers(mcpservers.New(st)))
	r.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		return acp.New(append([]string{fakeACPAgentPath}, agentArgs...), opts...)
	}
	return r, st, task, dump
}

func runMcpPrompt(t *testing.T, r *Runner, task store.Task) []Event {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	return got
}

func readMcpDump(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mcp dump: %v", err)
	}
	return string(data)
}

// TestRunner_RunPrompt_ACPSendsConfiguredMcpServers proves end to end
// that an enabled row for the task's workspace reaches the agent's
// session/new mcpServers (absolute stdio command), while a row restricted
// to a different workspace does not.
func TestRunner_RunPrompt_ACPSendsConfiguredMcpServers(t *testing.T) {
	r, st, task, dump := newMcpTestEnv(t, "stdio,http")

	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "playwright", Transport: "stdio", Command: "go", Env: `{"A":"1"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(playwright): %v", err)
	}
	other, err := st.CreateWorkspace(store.Workspace{Title: "Other"})
	if err != nil {
		t.Fatalf("CreateWorkspace(): %v", err)
	}
	restricted, err := reg.Create(store.McpServer{Name: "elsewhere", Transport: "http", URL: "https://other/mcp", Enabled: true})
	if err != nil {
		t.Fatalf("Create(elsewhere): %v", err)
	}
	if err := st.AddWorkspaceMcpServer(other.ID, restricted.ID); err != nil {
		t.Fatalf("AddWorkspaceMcpServer(): %v", err)
	}

	runMcpPrompt(t, r, task)

	got := readMcpDump(t, dump)
	var entries []map[string]any
	if err := json.Unmarshal([]byte(got), &entries); err != nil {
		t.Fatalf("dump %s is not a JSON array: %v", got, err)
	}
	if len(entries) != 1 {
		t.Fatalf("dump = %s, want exactly the playwright entry", got)
	}
	pw := entries[0]
	if pw["type"] != "stdio" || pw["name"] != "playwright" {
		t.Fatalf("dump entry = %v, want stdio playwright", pw)
	}
	cmd, _ := pw["command"].(string)
	if !filepath.IsAbs(cmd) || filepath.Base(cmd) != "go" {
		t.Fatalf("command = %v, want an absolute go path", pw["command"])
	}
	if env, _ := pw["env"].([]any); len(env) != 1 {
		t.Fatalf("env = %v, want one {name,value} entry", pw["env"])
	}
	if strings.Contains(got, "elsewhere") {
		t.Fatalf("dump = %s, want the other-workspace server absent", got)
	}
}

// TestRunner_RunPrompt_ACPNoMcpWhenNoneConfiguredIsEmptyArray is the
// regression guard: with a source configured but zero applicable rows,
// session/new still carries a literal [].
func TestRunner_RunPrompt_ACPNoMcpWhenNoneConfiguredIsEmptyArray(t *testing.T) {
	r, _, task, dump := newMcpTestEnv(t, "stdio,http")
	runMcpPrompt(t, r, task)
	if got := readMcpDump(t, dump); got != "[]" {
		t.Fatalf("dump = %s, want []", got)
	}
}

// TestRunner_RunPrompt_ACPInactiveMcpServerEmitsSessionNote: an agent
// advertising no mcp transport (v1, nothing advertised) drops an http
// server, emits a session note naming it, dumps [], and no event carries
// the row's secret header value.
func TestRunner_RunPrompt_ACPInactiveMcpServerEmitsSessionNote(t *testing.T) {
	r, st, task, dump := newMcpTestEnv(t, "")
	if _, err := mcpservers.New(st).Create(store.McpServer{
		Name: "playwright", Transport: "http", URL: "https://pw/mcp",
		Headers: `{"Authorization":"Bearer SUPERSECRET"}`, Enabled: true,
	}); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	events := runMcpPrompt(t, r, task)

	var notes []string
	for _, e := range events {
		if e.Type == EventTypeSessionNote {
			notes = append(notes, e.Text)
		}
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "playwright") {
		t.Fatalf("session notes = %v, want one naming playwright", notes)
	}
	for _, e := range events {
		if e.Text != "" && strings.Contains(e.Text, "SUPERSECRET") {
			t.Fatalf("event leaked a secret: %+v", e)
		}
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "SUPERSECRET") {
			t.Fatalf("event JSON leaked a secret: %s", raw)
		}
	}
	if got := readMcpDump(t, dump); got != "[]" {
		t.Fatalf("dump = %s, want []", got)
	}
}

// TestRunner_RunPrompt_ACPResumePathCarriesMcpServers: the second turn
// resumes via session/load; the servers must ride on that call too.
func TestRunner_RunPrompt_ACPResumePathCarriesMcpServers(t *testing.T) {
	r, st, task, dump := newMcpTestEnv(t, "stdio", "loadSession")
	if _, err := mcpservers.New(st).Create(store.McpServer{Name: "playwright", Transport: "stdio", Command: "go", Enabled: true}); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	runMcpPrompt(t, r, task)
	if err := os.Remove(dump); err != nil {
		t.Fatalf("remove dump: %v", err)
	}
	runMcpPrompt(t, r, task)

	if m := sessionInitMethod(t, *task.WorktreePath); m != "session/load" {
		t.Fatalf("second run session-init-method = %q, want session/load", m)
	}
	if got := readMcpDump(t, dump); !strings.Contains(got, `"playwright"`) {
		t.Fatalf("session/load dump = %s, want the playwright server", got)
	}
}

func TestRunner_RunPrompt_CodexNoteMcpServersUnsupported(t *testing.T) {
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	if _, err := mcpservers.New(st).Create(store.McpServer{
		Name: "playwright", Transport: "http", URL: "https://pw/mcp",
		Headers: `{"Authorization":"Bearer SUPERSECRET"}`, Enabled: true,
	}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	r := codexRunner(wm)
	WithMcpServers(mcpservers.New(st))(r)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{}, "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	var note string
	for _, e := range got {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "SUPERSECRET") {
			t.Fatalf("event leaked a secret: %s", raw)
		}
		if e.Type == EventTypeSessionNote {
			note = e.Text
		}
	}
	if !strings.Contains(note, "playwright") || !strings.Contains(note, "codex") {
		t.Fatalf("session note = %q, want one naming playwright for codex", note)
	}
}

func TestClaudeMcpConfig_BuildsMergedMcpServersJSON(t *testing.T) {
	rows := []store.McpServer{
		{Name: "pw", Transport: "stdio", Command: "npx", Args: `["-y","pw"]`, Env: `{"B":"2","A":"1"}`},
		{Name: "bare", Transport: "stdio", Command: "tool", Args: `[]`, Env: `{}`},
		{Name: "remote", Transport: "http", URL: "https://r/mcp", Headers: `{"Authorization":"x"}`},
		{Name: "events", Transport: "sse", URL: "https://e/sse"},
	}
	got, err := claudeMcpConfig(rows)
	if err != nil {
		t.Fatalf("claudeMcpConfig() error = %v", err)
	}
	want := `{"mcpServers":{"bare":{"command":"tool","type":"stdio"},"events":{"type":"sse","url":"https://e/sse"},"pw":{"args":["-y","pw"],"command":"npx","env":{"A":"1","B":"2"},"type":"stdio"},"remote":{"headers":{"Authorization":"x"},"type":"http","url":"https://r/mcp"}}}`
	if got != want {
		t.Fatalf("claudeMcpConfig() =\n%s\nwant\n%s", got, want)
	}
	if got, err := claudeMcpConfig(nil); err != nil || got != "" {
		t.Fatalf("claudeMcpConfig(nil) = %q, %v; want empty", got, err)
	}
}

// runClaudeMcp runs a claude-native turn with the echo-args fake CLI and
// returns the CLI's argv.
func runClaudeMcp(t *testing.T, r *Runner, task store.Task) []string {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	return strings.Split(string(data), "\n")
}

func newClaudeMcpEnv(t *testing.T) (*Runner, *store.Store, store.Task) {
	t.Helper()
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("echo-args"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	r := claudeNativeRunner(t, wm)
	WithMcpServers(mcpservers.New(st))(r)
	return r, st, task
}

func TestRunner_RunPrompt_ClaudeNativePassesMcpConfig(t *testing.T) {
	t.Parallel()
	r, st, task := newClaudeMcpEnv(t)

	args := runClaudeMcp(t, r, task)
	if _, ok := flagValue(args, "--mcp-config"); ok {
		t.Fatalf("--mcp-config present with no rows: %v", args)
	}

	if _, err := mcpservers.New(st).Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Args: `["-y"]`, Enabled: true}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	args = runClaudeMcp(t, r, task)
	want := `{"mcpServers":{"pw":{"args":["-y"],"command":"npx","type":"stdio"}}}`
	if v, ok := flagValue(args, "--mcp-config"); !ok || v != want {
		t.Fatalf("--mcp-config = %q (present %v), want %q; args %q", v, ok, want, args)
	}
}

func TestRunner_RunPrompt_ClaudeNativeWorkspaceRestriction(t *testing.T) {
	t.Parallel()
	r, st, task := newClaudeMcpEnv(t)
	reg := mcpservers.New(st)
	other, err := st.CreateWorkspace(store.Workspace{Title: "Other"})
	if err != nil {
		t.Fatalf("CreateWorkspace(): %v", err)
	}
	restricted, err := reg.Create(store.McpServer{Name: "elsewhere", Transport: "http", URL: "https://o/mcp", Enabled: true})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if err := st.AddWorkspaceMcpServer(other.ID, restricted.ID); err != nil {
		t.Fatalf("AddWorkspaceMcpServer(): %v", err)
	}

	args := runClaudeMcp(t, r, task)
	if v, ok := flagValue(args, "--mcp-config"); ok {
		t.Fatalf("--mcp-config = %q, want none for a server restricted to another workspace", v)
	}
}
