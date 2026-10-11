package taskrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
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

// absCmd turns a Unix-flavored fixture path into a genuinely absolute
// path for the host OS: filepath.IsAbs("/abs/npx") is false on Windows,
// which would wrongly route the already-absolute fixture command through
// PATH resolution.
func absCmd(p string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(`C:\\`, strings.TrimPrefix(p, "/"))
	}
	return p
}

// TestAcpMcpServers_MapsStdioAndHttpWireShape proves each transport maps
// to its ACP wire shape: args as an array, env/headers as arrays of
// {name,value} objects sorted by key for determinism.
func TestAcpMcpServers_MapsStdioAndHttpWireShape(t *testing.T) {
	t.Parallel()
	all := staticMcpCaps{stdio: true, http: true, sse: true}
	look := func(string) (string, error) { return "/resolved/bin", nil }

	rows := []store.McpServer{
		stdioRow("playwright", absCmd("/abs/npx")),
		{Transport: "http", Name: "pplx", URL: "https://mcp.pplx.ai/mcp", Headers: `{"Authorization":"Bearer t"}`, Enabled: true},
		{Transport: "sse", Name: "legacy", URL: "https://old/mcp", Headers: `{"X":"1"}`, Enabled: true},
	}
	wire, inactive, _, _, err := acpMcpServers(rows, all, look)
	if err != nil {
		t.Fatalf("acpMcpServers() error = %v", err)
	}
	if len(inactive) != 0 {
		t.Fatalf("inactive = %v, want none", inactive)
	}
	if len(wire) != 3 {
		t.Fatalf("wire = %v, want 3 entries", wire)
	}
	wantStdio := `{"type":"stdio","name":"playwright","command":` + strconv.Quote(absCmd("/abs/npx")) + `,"args":["--flag"],"env":[{"name":"A","value":"1"},{"name":"B","value":"2"}]}`
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
		stdioRow("playwright", absCmd("/abs/npx")),
		{Transport: "http", Name: "pplx", URL: "https://mcp.pplx.ai/mcp", Enabled: true},
	}

	t.Run("v2 agent advertising only http", func(t *testing.T) {
		t.Parallel()
		wire, inactive, _, _, err := acpMcpServers(rows, staticMcpCaps{http: true}, look)
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
		wire, inactive, _, _, err := acpMcpServers(rows, staticMcpCaps{stdio: true}, look)
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
		wire, inactive, _, _, err := acpMcpServers([]store.McpServer{stdioRow("s", "/x"), {Transport: "http", Name: "h", URL: "u", Enabled: true}}, staticMcpCaps{}, look)
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
		wire, _, _, _, err := acpMcpServers([]store.McpServer{stdioRow("pw", "npx")}, staticMcpCaps{stdio: true}, func(cmd string) (string, error) {
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
		content := "#!/bin/sh\nexit 0\n"
		if runtime.GOOS == "windows" {
			// Bare-command resolution goes through PATHEXT on Windows;
			// an extensionless script can never be found, while an
			// npm-style fakenpx.cmd is -- matching how a real "npx"
			// MCP server command resolves.
			exe = filepath.Join(dir, "fakenpx.cmd")
			content = "@echo off\r\n"
		}
		if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
			t.Fatalf("write fakenpx: %v", err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		wire, _, _, _, err := acpMcpServers([]store.McpServer{stdioRow("pw", "fakenpx")}, staticMcpCaps{stdio: true}, exec.LookPath)
		if err != nil {
			t.Fatalf("acpMcpServers() error = %v", err)
		}
		cmd := wire[0].(acp.McpServerStdio).Command
		if cmd != exe {
			t.Fatalf("command = %q, want %q", cmd, exe)
		}
	})
}

// TestAcpMcpServers_UnresolvableCommandIsSkippedWithoutLeakingSecrets: a
// bare command not on PATH is skipped (not an error, so the run goes on),
// reported naming only the server name and command -- never env/args
// values like SUPERSECRET -- while the other servers are still mapped.
func TestAcpMcpServers_UnresolvableCommandIsSkippedWithoutLeakingSecrets(t *testing.T) {
	t.Parallel()
	row := store.McpServer{
		Name:      "playwright",
		Transport: "stdio",
		Command:   "no-such-command-xyz",
		Args:      `["SUPERSECRET"]`,
		Env:       `{"TOKEN":"SUPERSECRET"}`,
		Enabled:   true,
	}
	good := stdioRow("good", absCmd("/abs/tool"))
	wire, inactive, unresolved, _, err := acpMcpServers([]store.McpServer{row, good}, staticMcpCaps{stdio: true}, exec.LookPath)
	if err != nil {
		t.Fatalf("acpMcpServers() error = %v, want the unresolvable command skipped, not failed", err)
	}
	if len(inactive) != 0 {
		t.Fatalf("inactive = %v, want none", inactive)
	}
	if len(wire) != 1 || wire[0].(acp.McpServerStdio).Name != "good" {
		t.Fatalf("wire = %v, want only the resolvable server", wire)
	}
	if len(unresolved) != 1 || !strings.Contains(unresolved[0], "playwright") || !strings.Contains(unresolved[0], "no-such-command-xyz") {
		t.Fatalf("unresolved = %v, want one entry naming the server and command", unresolved)
	}
	if strings.Contains(unresolved[0], "SUPERSECRET") {
		t.Fatalf("unresolved = %v, leaked a secret", unresolved)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{}, "", "", events)
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
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
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
	v, ok := flagValue(args, "--mcp-config")
	if !ok || v == "" || v == want {
		t.Fatalf("--mcp-config = %q (present %v), want a file path (not inline JSON); args %q", v, ok, args)
	}
	// The CLI saw the expected JSON in the file it was pointed at.
	got, err := os.ReadFile(filepath.Join(*task.WorktreePath, "mcp-config-content"))
	if err != nil {
		t.Fatalf("read mcp-config-content: %v", err)
	}
	if string(got) != want {
		t.Fatalf("mcp config file content =\n%s\nwant\n%s", got, want)
	}
}

// claudeMcpTempDir points os.TempDir at a fresh dir (so a test can assert
// nothing is left behind) and returns it. Not usable with t.Parallel.
func claudeMcpTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("temp dir not empty: %v", names)
	}
}

func seedClaudeMcpSecretServers(t *testing.T, st *store.Store) {
	t.Helper()
	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Env: `{"TOKEN":"SUPERSECRET-ENV"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "remote", Transport: "http", URL: "https://r/mcp", Headers: `{"Authorization":"Bearer SUPERSECRET-HDR"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(remote): %v", err)
	}
}

func TestRunner_RunPrompt_ClaudeNativeMcpConfigFileMode(t *testing.T) {
	r, st, task := newClaudeMcpEnv(t)
	seedClaudeMcpSecretServers(t, st)
	claudeMcpTempDir(t)

	runClaudeMcp(t, r, task)

	modes, err := os.ReadFile(filepath.Join(*task.WorktreePath, "mcp-config-modes"))
	if err != nil {
		t.Fatalf("read mcp-config-modes: %v", err)
	}
	if string(modes) != "600 700" {
		t.Fatalf("file/dir modes = %q, want %q", modes, "600 700")
	}
	content, err := os.ReadFile(filepath.Join(*task.WorktreePath, "mcp-config-content"))
	if err != nil {
		t.Fatalf("read mcp-config-content: %v", err)
	}
	want, err := claudeMcpConfig(mustListMcp(t, st))
	if err != nil || string(content) != want {
		t.Fatalf("file content = %s, want %s (err %v)", content, want, err)
	}
}

func mustListMcp(t *testing.T, st *store.Store) []store.McpServer {
	t.Helper()
	rows, err := mcpservers.New(st).ListForWorkspace(0)
	if err != nil {
		t.Fatalf("ListForWorkspace: %v", err)
	}
	return rows
}

func TestRunner_RunPrompt_ClaudeNativeMcpConfigSecretNotInArgv(t *testing.T) {
	r, st, task := newClaudeMcpEnv(t)
	seedClaudeMcpSecretServers(t, st)
	claudeMcpTempDir(t)

	args := runClaudeMcp(t, r, task)

	for _, a := range args {
		if strings.Contains(a, "SUPERSECRET") {
			t.Fatalf("argv element leaks a secret: %q", a)
		}
	}
	v, ok := flagValue(args, "--mcp-config")
	if !ok || !filepath.IsAbs(v) {
		t.Fatalf("--mcp-config = %q (present %v), want an absolute file path", v, ok)
	}
}

func TestRunner_RunPrompt_ClaudeNativeMcpConfigRemovedAfterRun(t *testing.T) {
	r, st, task := newClaudeMcpEnv(t)
	seedClaudeMcpSecretServers(t, st)
	tmp := claudeMcpTempDir(t)

	t.Run("success", func(t *testing.T) {
		runClaudeMcp(t, r, task)
		assertDirEmpty(t, tmp)
	})

	t.Run("spawn failure", func(t *testing.T) {
		// An unknown scenario makes the fake CLI exit before the
		// initialize handshake, so the spawn itself fails.
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("no-such-scenario"), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
		events := make(chan Event)
		errCh := make(chan error, 1)
		go func() {
			errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
		}()
		drainEvents(events)
		if err := <-errCh; err == nil {
			t.Fatal("RunPrompt() error = nil, want a spawn failure")
		}
		assertDirEmpty(t, tmp)
	})

	t.Run("cancelled run", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("init-hang"), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		events := make(chan Event)
		errCh := make(chan error, 1)
		go func() {
			errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
		}()
		// Wait for the first event (the run is live, CLI spawned), then cancel.
		<-events
		cancel()
		go drainEvents(events)
		<-errCh
		assertDirEmpty(t, tmp)
	})
}

func TestRunner_RunPrompt_ClaudeNativeNoMcpServersCreatesNoTempFile(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	tmp := claudeMcpTempDir(t)

	args := runClaudeMcp(t, r, task)
	if _, ok := flagValue(args, "--mcp-config"); ok {
		t.Fatalf("--mcp-config present with no rows: %v", args)
	}
	assertDirEmpty(t, tmp)
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

// TestRunner_RunPrompt_ACPActiveMcpServerSecretNeverLeaksOutsideWire is the
// ADR-0018 / AC9 audit for the path where the secret IS legitimately sent:
// a stdio server and an http server both reach the agent (so the dump file
// carries the values), yet no emitted Event (the only thing internal/runs
// persists as run_events) and no daemon log line may contain them.
func TestRunner_RunPrompt_ACPActiveMcpServerSecretNeverLeaksOutsideWire(t *testing.T) {
	r, st, task, dump := newMcpTestEnv(t, "stdio,http")
	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "go", Env: `{"TOKEN":"SUPERSECRET-ENV"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "remote", Transport: "http", URL: "https://r/mcp", Headers: `{"Authorization":"Bearer SUPERSECRET-HDR"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(remote): %v", err)
	}

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	events := runMcpPrompt(t, r, task)

	wire := readMcpDump(t, dump)
	if !strings.Contains(wire, "SUPERSECRET-ENV") || !strings.Contains(wire, "SUPERSECRET-HDR") {
		t.Fatalf("wire dump = %s, want both secrets delivered to the agent", wire)
	}
	for _, e := range events {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "SUPERSECRET") {
			t.Fatalf("event leaked a secret: %s", raw)
		}
	}
	if strings.Contains(logs.String(), "SUPERSECRET") {
		t.Fatalf("log output leaked a secret: %s", logs.String())
	}
}

// TestRunner_RunPrompt_ACPUnresolvableMcpCommandDegradesToNote: one stdio
// server whose command isn't on PATH is skipped with a session note
// naming it (and its command, never its secrets), and the run still
// executes with the remaining servers instead of failing.
func TestRunner_RunPrompt_ACPUnresolvableMcpCommandDegradesToNote(t *testing.T) {
	r, st, task, dump := newMcpTestEnv(t, "stdio,http")
	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "broken", Transport: "stdio", Command: "no-such-command-xyz", Env: `{"TOKEN":"SUPERSECRET"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(broken): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "good", Transport: "stdio", Command: "go", Enabled: true}); err != nil {
		t.Fatalf("Create(good): %v", err)
	}

	events := runMcpPrompt(t, r, task)

	var notes []string
	var done bool
	for _, e := range events {
		switch e.Type {
		case EventTypeSessionNote:
			notes = append(notes, e.Text)
		case EventTypeDone:
			done = true
		}
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "SUPERSECRET") {
			t.Fatalf("event leaked a secret: %s", raw)
		}
	}
	if !done {
		t.Fatalf("no done event: the prompt did not run; events %+v", events)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "broken") || !strings.Contains(notes[0], "no-such-command-xyz") {
		t.Fatalf("session notes = %v, want one naming broken and its command", notes)
	}
	got := readMcpDump(t, dump)
	if strings.Contains(got, "broken") || !strings.Contains(got, `"good"`) {
		t.Fatalf("dump = %s, want only the good server", got)
	}
}

func TestMcpSecretsAndScrub(t *testing.T) {
	t.Parallel()
	rows := []store.McpServer{
		{Name: "pw", Transport: "stdio", Command: "npx", Args: `["-y","--token=ARGSECRET99","POSITIONALSECRET"]`, Env: `{"TOKEN":"ENVSECRET123","N":"1"}`},
		{Name: "r", Transport: "http", URL: "https://r/mcp", Headers: `{"Authorization":"Bearer HDRSECRET456"}`},
	}
	secrets := mcpSecrets(rows)
	text := "failed: ENVSECRET123 Bearer HDRSECRET456 HDRSECRET456 ARGSECRET99 POSITIONALSECRET -y"
	got := scrubSecrets(text, secrets)
	for _, leak := range []string{"ENVSECRET123", "HDRSECRET456", "ARGSECRET99", "POSITIONALSECRET"} {
		if strings.Contains(got, leak) {
			t.Fatalf("scrubSecrets() = %q, still contains %q", got, leak)
		}
	}
	if !strings.Contains(got, "-y") {
		t.Fatalf("scrubSecrets() = %q, want bare flags untouched", got)
	}
	if got := scrubSecrets("nothing here", nil); got != "nothing here" {
		t.Fatalf("scrubSecrets(no secrets) = %q", got)
	}
}

// scrubStubACP is an acpBackend whose session setup fails with an error
// echoing a secret, as an agent that reflects its MCP env back might.
type scrubStubACP struct {
	acpBackend
	loadSupported bool
	secret        string
}

func (scrubStubACP) Initialize(context.Context) error { return nil }
func (scrubStubACP) Close() error                     { return nil }
func (scrubStubACP) SupportsMcpStdio() bool           { return true }
func (scrubStubACP) SupportsMcpHttp() bool            { return true }
func (scrubStubACP) SupportsMcpSSE() bool             { return true }
func (s scrubStubACP) SupportsLoadSession() bool      { return s.loadSupported }
func (s scrubStubACP) NewSession(context.Context, string, []any) (string, []acp.ConfigOption, error) {
	return "", nil, errors.New("new failed with " + s.secret)
}
func (s scrubStubACP) LoadSession(context.Context, string, string, []any) ([]acp.ConfigOption, error) {
	return nil, errors.New("load failed with " + s.secret)
}

// TestRunner_RunPrompt_ACPSessionErrorsScrubMcpSecrets: a secret an agent
// echoes in a session/new or session/load error must reach neither the
// session note, the logs, nor the returned run error.
func TestRunner_RunPrompt_ACPSessionErrorsScrubMcpSecrets(t *testing.T) {
	const secret = "SUPERSECRET-ENV"
	for _, resume := range []bool{false, true} {
		name := "session/new"
		if resume {
			name = "session/load then session/new"
		}
		t.Run(name, func(t *testing.T) {
			st, wm, _ := newTestStoreAndManager(t)
			task := newTestTaskUnder(t, wm)
			if _, err := mcpservers.New(st).Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "go", Env: `{"TOKEN":"` + secret + `"}`, Enabled: true}); err != nil {
				t.Fatalf("Create(): %v", err)
			}
			r := New(wm, WithMcpServers(mcpservers.New(st)))
			r.newACPClient = func([]string, ...acp.Option) (acpBackend, error) {
				return scrubStubACP{loadSupported: resume, secret: secret}, nil
			}
			if resume {
				r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderGLM, SessionID: "old"})
			}

			var logs bytes.Buffer
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			events := make(chan Event)
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", "", events)
			}()
			got := drainEvents(events)
			err := <-errCh
			if err == nil || !strings.Contains(err.Error(), "new failed") {
				t.Fatalf("RunPrompt() error = %v, want the new-session failure", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("run error leaked a secret: %v", err)
			}
			for _, e := range got {
				raw, _ := json.Marshal(e)
				if strings.Contains(string(raw), secret) {
					t.Fatalf("event leaked a secret: %s", raw)
				}
			}
			if strings.Contains(logs.String(), secret) {
				t.Fatalf("log leaked a secret: %s", logs.String())
			}
			if resume {
				var noted bool
				for _, e := range got {
					noted = noted || (e.Type == EventTypeSessionNote && strings.Contains(e.Text, "load failed"))
				}
				if !noted {
					t.Fatalf("events %+v, want a resume-failure note carrying the scrubbed error", got)
				}
			}
		})
	}
}

func TestClaudeFailedMcpServers(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"type":"system","subtype":"init","mcp_servers":[{"name":"b","status":"failed"},{"name":"ok","status":"connected"},{"name":"a","status":"failed"},{"name":"auth","status":"needs-auth"}]}`)
	got := claudeFailedMcpServers(raw)
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("claudeFailedMcpServers() = %v, want [a b]", got)
	}
	if got := claudeFailedMcpServers(json.RawMessage(`{"type":"system"}`)); got != nil {
		t.Fatalf("claudeFailedMcpServers(no mcp_servers) = %v, want nil", got)
	}
	if got := claudeFailedMcpServers(json.RawMessage(`not json`)); got != nil {
		t.Fatalf("claudeFailedMcpServers(bad json) = %v, want nil", got)
	}
}

// TestRunner_RunPrompt_ClaudeNativeFailedMcpServerEmitsNoteWithoutSecrets:
// the CLI's init message reports a failed server; the run surfaces its
// name as a session note, and neither events nor logs carry the secret
// seeded into the servers' env/headers (it legitimately reaches only the
// CLI, via the private --mcp-config file).
func TestRunner_RunPrompt_ClaudeNativeFailedMcpServerEmitsNoteWithoutSecrets(t *testing.T) {
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("mcp-init"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	r := claudeNativeRunner(t, wm)
	WithMcpServers(mcpservers.New(st))(r)
	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Env: `{"TOKEN":"SUPERSECRET-ENV"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "broken", Transport: "http", URL: "https://b/mcp", Headers: `{"Authorization":"Bearer SUPERSECRET-HDR"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(broken): %v", err)
	}

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	var notes []string
	for _, e := range got {
		if e.Type == EventTypeSessionNote {
			notes = append(notes, e.Text)
		}
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "SUPERSECRET") {
			t.Fatalf("event leaked a secret: %s", raw)
		}
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "broken") || strings.Contains(notes[0], "pw") {
		t.Fatalf("session notes = %v, want one naming only the failed server", notes)
	}
	if strings.Contains(logs.String(), "SUPERSECRET") {
		t.Fatalf("log output leaked a secret: %s", logs.String())
	}
}

// mcpServerEvents filters a run's events down to the EventTypeMcpServers
// ones, in order.
func mcpServerEvents(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if e.Type == EventTypeMcpServers {
			out = append(out, e)
		}
	}
	return out
}

// assertMcpStatesNoSecrets fails when the event's McpServers JSON contains
// any of the given secret-bearing strings (env/header values, args,
// command, url) -- the ADR-0018 redaction rule for the mcp_servers event.
func assertMcpStatesNoSecrets(t *testing.T, e Event, leaks ...string) {
	t.Helper()
	raw, _ := json.Marshal(e.McpServers)
	for _, leak := range leaks {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("mcp_servers event JSON %s leaked %q", raw, leak)
		}
	}
}

// TestAcpMcpServerStates_ActiveDroppedAndUnresolved: the pure mapping of
// rows + capabilities to []McpServerState -- active for wire entries,
// dropped/transport_unsupported for capability-dropped rows,
// dropped/command_not_found for an unresolvable stdio command, in row
// order -- with no secret/command/url value in the JSON.
func TestAcpMcpServerStates_ActiveDroppedAndUnresolved(t *testing.T) {
	t.Parallel()
	look := func(cmd string) (string, error) {
		if cmd == "no-such-command-xyz" {
			return "", os.ErrNotExist
		}
		return "/resolved/" + cmd, nil
	}
	rows := []store.McpServer{
		{Name: "pw", Transport: "stdio", Command: "go", Args: `["ARG-SECRET-99"]`, Env: `{"TOKEN":"ENV-SECRET-99"}`, Enabled: true},
		{Name: "dropme", Transport: "http", URL: "https://URL-SECRET-99/mcp", Enabled: true},
		{Name: "broken", Transport: "stdio", Command: "no-such-command-xyz", Enabled: true},
	}
	_, _, _, states, err := acpMcpServers(rows, staticMcpCaps{stdio: true}, look)
	if err != nil {
		t.Fatalf("acpMcpServers() error = %v", err)
	}
	want := []McpServerState{
		{Name: "pw", Transport: "stdio", Status: McpStatusActive},
		{Name: "dropme", Transport: "http", Status: McpStatusDropped, Reason: McpReasonTransportUnsupported},
		{Name: "broken", Transport: "stdio", Status: McpStatusDropped, Reason: McpReasonCommandNotFound},
	}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %+v, want %+v", states, want)
	}
	assertMcpStatesNoSecrets(t, Event{McpServers: states}, "ENV-SECRET-99", "ARG-SECRET-99", "URL-SECRET-99", "no-such-command-xyz")
}

// TestRunner_RunPrompt_ACPEmitsMcpServersEvent: an ACP run with mixed
// servers emits exactly one mcp_servers event, in row order, before the
// session is created -- active for the wire entries, dropped with the
// right reason for the capability-dropped and unresolvable ones.
func TestRunner_RunPrompt_ACPEmitsMcpServersEvent(t *testing.T) {
	r, st, task, _ := newMcpTestEnv(t, "stdio")
	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "go", Env: `{"TOKEN":"ENV-SECRET-99"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "dropme", Transport: "http", URL: "https://URL-SECRET-99/mcp", Enabled: true}); err != nil {
		t.Fatalf("Create(dropme): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "broken", Transport: "stdio", Command: "no-such-command-xyz", Args: `["ARG-SECRET-99"]`, Enabled: true}); err != nil {
		t.Fatalf("Create(broken): %v", err)
	}

	events := runMcpPrompt(t, r, task)

	got := mcpServerEvents(events)
	if len(got) != 1 {
		t.Fatalf("mcp_servers events = %d, want exactly 1 (events %+v)", len(got), got)
	}
	want := []McpServerState{
		{Name: "pw", Transport: "stdio", Status: McpStatusActive},
		{Name: "dropme", Transport: "http", Status: McpStatusDropped, Reason: McpReasonTransportUnsupported},
		{Name: "broken", Transport: "stdio", Status: McpStatusDropped, Reason: McpReasonCommandNotFound},
	}
	if !reflect.DeepEqual(got[0].McpServers, want) {
		t.Fatalf("mcp_servers = %+v, want %+v", got[0].McpServers, want)
	}
	assertMcpStatesNoSecrets(t, got[0], "ENV-SECRET-99", "ARG-SECRET-99", "URL-SECRET-99", "no-such-command-xyz")
}

// TestRunner_RunPrompt_ACPNoMcpServersEventWhenNoneConfigured is the
// regression guard: a run with no applicable servers emits no mcp_servers
// event at all, exactly as before the feature.
func TestRunner_RunPrompt_ACPNoMcpServersEventWhenNoneConfigured(t *testing.T) {
	r, _, task, _ := newMcpTestEnv(t, "stdio,http")
	events := runMcpPrompt(t, r, task)
	if got := mcpServerEvents(events); len(got) != 0 {
		t.Fatalf("mcp_servers events = %+v, want none", got)
	}
}

// TestRunner_RunPrompt_ClaudeNativeEmitsMcpServersAndFailedUpdate: the
// claude-native run emits the initial all-active event, then a second one
// after the CLI init message that marks the CLI-reported failed server
// failed/failed_to_start and the rest active.
func TestRunner_RunPrompt_ClaudeNativeEmitsMcpServersAndFailedUpdate(t *testing.T) {
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("mcp-init"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	r := claudeNativeRunner(t, wm)
	WithMcpServers(mcpservers.New(st))(r)
	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Env: `{"TOKEN":"ENV-SECRET-99"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}
	if _, err := reg.Create(store.McpServer{Name: "broken", Transport: "http", URL: "https://URL-SECRET-99/mcp", Enabled: true}); err != nil {
		t.Fatalf("Create(broken): %v", err)
	}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	mcp := mcpServerEvents(got)
	if len(mcp) != 2 {
		t.Fatalf("mcp_servers events = %d, want 2 (events %+v)", len(mcp), mcp)
	}
	wantInitial := []McpServerState{
		{Name: "pw", Transport: "stdio", Status: McpStatusActive},
		{Name: "broken", Transport: "http", Status: McpStatusActive},
	}
	if !reflect.DeepEqual(mcp[0].McpServers, wantInitial) {
		t.Fatalf("initial mcp_servers = %+v, want %+v", mcp[0].McpServers, wantInitial)
	}
	wantUpdate := []McpServerState{
		{Name: "pw", Transport: "stdio", Status: McpStatusActive},
		{Name: "broken", Transport: "http", Status: McpStatusFailed, Reason: McpReasonFailedToStart},
	}
	if !reflect.DeepEqual(mcp[1].McpServers, wantUpdate) {
		t.Fatalf("update mcp_servers = %+v, want %+v", mcp[1].McpServers, wantUpdate)
	}
	for _, e := range mcp {
		assertMcpStatesNoSecrets(t, e, "ENV-SECRET-99", "URL-SECRET-99", "npx")
	}
}

// TestRunner_RunPrompt_ClaudeNativeNoSecondMcpEventWithoutFailures: with
// no CLI-reported failures there is exactly one mcp_servers event, the
// initial all-active one.
func TestRunner_RunPrompt_ClaudeNativeNoSecondMcpEventWithoutFailures(t *testing.T) {
	r, st, task := newClaudeMcpEnv(t)
	if _, err := mcpservers.New(st).Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Enabled: true}); err != nil {
		t.Fatalf("Create(pw): %v", err)
	}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	mcp := mcpServerEvents(got)
	if len(mcp) != 1 {
		t.Fatalf("mcp_servers events = %d, want exactly 1 (events %+v)", len(mcp), mcp)
	}
	want := []McpServerState{{Name: "pw", Transport: "stdio", Status: McpStatusActive}}
	if !reflect.DeepEqual(mcp[0].McpServers, want) {
		t.Fatalf("mcp_servers = %+v, want %+v", mcp[0].McpServers, want)
	}
}

// TestRunner_RunPrompt_CodexEmitsMcpServersProviderUnsupported: a Codex
// run with configured servers emits exactly one mcp_servers event, every
// row dropped/provider_unsupported.
func TestRunner_RunPrompt_CodexEmitsMcpServersProviderUnsupported(t *testing.T) {
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	if _, err := mcpservers.New(st).Create(store.McpServer{Name: "remote", Transport: "http", URL: "https://URL-SECRET-99/mcp", Headers: `{"Authorization":"Bearer HDR-SECRET-99"}`, Enabled: true}); err != nil {
		t.Fatalf("Create(remote): %v", err)
	}
	r := codexRunner(wm)
	WithMcpServers(mcpservers.New(st))(r)

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderCodexNative, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}

	mcp := mcpServerEvents(got)
	if len(mcp) != 1 {
		t.Fatalf("mcp_servers events = %d, want exactly 1 (events %+v)", len(mcp), mcp)
	}
	want := []McpServerState{{Name: "remote", Transport: "http", Status: McpStatusDropped, Reason: McpReasonProviderUnsupported}}
	if !reflect.DeepEqual(mcp[0].McpServers, want) {
		t.Fatalf("mcp_servers = %+v, want %+v", mcp[0].McpServers, want)
	}
	assertMcpStatesNoSecrets(t, mcp[0], "HDR-SECRET-99", "URL-SECRET-99")
}
