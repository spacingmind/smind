package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/store"
)

type fixedMcpSource []store.McpServer

func (s fixedMcpSource) ListForWorkspace(int64) ([]store.McpServer, error) { return s, nil }

func seatEnv() map[string]string {
	return map[string]string{
		"SMIND_HOME": "/home/x", "SMIND_RUN_ID": "run-1", "SMIND_TASK_ID": "7", "SMIND_CHAT_ID": "9",
	}
}

func policyCtx(p *McpPolicy) context.Context {
	return WithRunContract(context.Background(), RunContract{Mcp: p})
}

func policyRunner(rows ...store.McpServer) *Runner {
	r := &Runner{}
	WithMcpServers(fixedMcpSource(rows))(r)
	WithSmindBinary("/opt/smind")(r)
	return r
}

func names(rows []store.McpServer) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestMcpRowsFor_LegacyUnchanged(t *testing.T) {
	t.Parallel()
	rows := []store.McpServer{stdioRow("a", "x"), stdioRow("smind", "user-smind")}
	r := policyRunner(rows...)
	got, err := r.mcpRowsFor(context.Background(), 1)
	if err != nil || !reflect.DeepEqual(got, rows) {
		t.Fatalf("rows = %+v, err %v; want unchanged %+v", got, err, rows)
	}
	// A contract without an Mcp policy is also legacy.
	got, _ = r.mcpRowsFor(WithRunContract(context.Background(), RunContract{Instructions: "x"}), 1)
	if !reflect.DeepEqual(got, rows) {
		t.Fatalf("rows with instructions-only contract = %+v", got)
	}
}

func TestMcpRowsFor_ManagedStripsUserSmindRow(t *testing.T) {
	t.Parallel()
	r := policyRunner(stdioRow("a", "x"), stdioRow("smind", "user-smind"))
	got, err := r.mcpRowsFor(policyCtx(&McpPolicy{Orchestration: true, Env: seatEnv()}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names(got), []string{"a", "smind"}) {
		t.Fatalf("names = %v", names(got))
	}
	if got[1].Command != "/opt/smind" {
		t.Fatalf("smind row is the user's: %+v", got[1])
	}
}

func TestMcpRowsFor_OrchestrationAddsBuiltinEntry(t *testing.T) {
	t.Parallel()
	r := policyRunner()
	got, err := r.mcpRowsFor(policyCtx(&McpPolicy{Orchestration: true, Env: seatEnv()}), 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("rows = %+v, err %v", got, err)
	}
	row := got[0]
	var args []string
	var env map[string]string
	if err := json.Unmarshal([]byte(row.Args), &args); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(row.Env), &env); err != nil {
		t.Fatal(err)
	}
	if row.Name != "smind" || row.Transport != "stdio" || !row.Enabled || row.Command != "/opt/smind" ||
		!reflect.DeepEqual(args, []string{"mcp", "serve"}) || !reflect.DeepEqual(env, seatEnv()) {
		t.Fatalf("row = %+v", row)
	}
}

func TestMcpRowsFor_PeerHasNoSmindEntry(t *testing.T) {
	t.Parallel()
	r := policyRunner(stdioRow("a", "x"), stdioRow("smind", "user-smind"))
	got, err := r.mcpRowsFor(policyCtx(&McpPolicy{}), 1)
	if err != nil || !reflect.DeepEqual(names(got), []string{"a"}) {
		t.Fatalf("names = %v, err %v", names(got), err)
	}
}

// runClaudeCtx runs one claude-native turn under ctx and returns the CLI
// argv plus the content of the --mcp-config file it was given ("" if none).
func runClaudeCtx(t *testing.T, r *Runner, task store.Task, ctx context.Context) ([]string, string) {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderClaudeNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(*task.WorktreePath, "args"))
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(*task.WorktreePath, "mcp-config-content"))
	return strings.Split(string(data), "\n"), string(content)
}

func hasFlag(args []string, name string) bool {
	_, ok := flagValue(args, name)
	return ok
}

func registerUserSmind(t *testing.T, st *store.Store) {
	t.Helper()
	reg := mcpservers.New(st)
	if _, err := reg.Create(store.McpServer{Name: "smind", Transport: "stdio", Command: "user-smind", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(store.McpServer{Name: "pw", Transport: "stdio", Command: "npx", Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRunClaude_PeerHasStrictMCPAndNoSmindServer(t *testing.T) {
	r, st, task := newClaudeMcpEnv(t)
	registerUserSmind(t, st)
	args, content := runClaudeCtx(t, r, task, policyCtx(&McpPolicy{}))
	if !hasFlag(args, "--strict-mcp-config") {
		t.Fatalf("--strict-mcp-config missing: %q", args)
	}
	var cfg struct {
		McpServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("config %q: %v", content, err)
	}
	if _, ok := cfg.McpServers["smind"]; ok || cfg.McpServers["pw"] == nil {
		t.Fatalf("servers = %v, want pw only", cfg.McpServers)
	}
}

func TestRunClaude_PeerNoRowsStillStrict(t *testing.T) {
	r, _, task := newClaudeMcpEnv(t)
	args, _ := runClaudeCtx(t, r, task, policyCtx(&McpPolicy{}))
	if !hasFlag(args, "--strict-mcp-config") || hasFlag(args, "--mcp-config") {
		t.Fatalf("args = %q, want strict and no --mcp-config", args)
	}
}

func TestRunClaude_LeadGetsSmindEntryWithIdentityEnv(t *testing.T) {
	r, st, task := newClaudeMcpEnv(t)
	registerUserSmind(t, st)
	WithSmindBinary("/opt/smind")(r)
	args, content := runClaudeCtx(t, r, task, policyCtx(&McpPolicy{Orchestration: true, Env: seatEnv()}))
	if !hasFlag(args, "--strict-mcp-config") {
		t.Fatalf("--strict-mcp-config missing: %q", args)
	}
	var cfg struct {
		McpServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("config %q: %v", content, err)
	}
	s := cfg.McpServers["smind"]
	if s.Command != "/opt/smind" || !reflect.DeepEqual(s.Args, []string{"mcp", "serve"}) || !reflect.DeepEqual(s.Env, seatEnv()) {
		t.Fatalf("smind entry = %+v", s)
	}
}

func TestRunClaude_LegacyProfileNoStrict(t *testing.T) {
	r, st, task := newClaudeMcpEnv(t)
	registerUserSmind(t, st)
	args, content := runClaudeCtx(t, r, task, context.Background())
	if hasFlag(args, "--strict-mcp-config") {
		t.Fatalf("strict flag on a legacy run: %q", args)
	}
	if !strings.Contains(content, "user-smind") {
		t.Fatalf("legacy run lost the registered smind row: %s", content)
	}
}

func runACPCtx(t *testing.T, r *Runner, task store.Task, ctx context.Context) {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderGLM, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
}

func TestRunACP_PeerMcpServersOmitSmind(t *testing.T) {
	r, st, task, dump := newMcpTestEnv(t, "stdio,http")
	registerUserSmind(t, st)
	runACPCtx(t, r, task, policyCtx(&McpPolicy{}))
	got := readMcpDump(t, dump)
	if strings.Contains(got, `"smind"`) || !strings.Contains(got, `"pw"`) {
		t.Fatalf("mcpServers = %s, want pw without smind", got)
	}
}

func TestRunACP_LeadMcpServersIncludeSmind(t *testing.T) {
	r, st, task, dump := newMcpTestEnv(t, "stdio,http")
	registerUserSmind(t, st)
	self, _ := os.Executable()
	WithSmindBinary(self)(r)
	runACPCtx(t, r, task, policyCtx(&McpPolicy{Orchestration: true, Env: seatEnv()}))
	var entries []struct {
		Name    string   `json:"name"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Env     []struct{ Name, Value string }
	}
	if err := json.Unmarshal([]byte(readMcpDump(t, dump)), &entries); err != nil {
		t.Fatal(err)
	}
	var found int
	for _, e := range entries {
		if e.Name != "smind" {
			continue
		}
		found++
		if e.Command != self || !reflect.DeepEqual(e.Args, []string{"mcp", "serve"}) || len(e.Env) != 4 {
			t.Fatalf("smind entry = %+v", e)
		}
	}
	if found != 1 {
		t.Fatalf("entries = %+v, want exactly one smind", entries)
	}
}

func TestRunCodex_ManagedRunNotEnforcedNote(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "")
	r := codexRunner(wm)
	ctx := policyCtx(&McpPolicy{})
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, task.ID, task.ID, ProviderCodexNative, "hi", denyAllDecider{}, PermissionSettings{}, "", "", events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range got {
		if e.Type == EventTypeSessionNote && strings.Contains(e.Text, "tool policy is not enforced") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("not-enforced notes = %d, want 1", n)
	}
}
