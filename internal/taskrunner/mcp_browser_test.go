package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

func browserEnv() map[string]string {
	return map[string]string{"SMIND_HOME": "/home/x", "SMIND_RUN_ID": "run-1"}
}

// socketPolicy is a browser policy whose Socket records the root pid it was
// opened for and returns a fixed path.
func socketPolicy(root *int) *BrowserPolicy {
	return &BrowserPolicy{Env: browserEnv(), Socket: func(pid int) (string, error) { *root = pid; return "/run/sock/x.sock", nil }}
}

func TestMcpRowsFor_BrowserRowOnlyWithBrowserPolicy(t *testing.T) {
	t.Parallel()
	r := policyRunner(stdioRow("a", "x"))
	for name, p := range map[string]*McpPolicy{
		"orchestration only": {Orchestration: true, Env: seatEnv()},
		"empty policy":       {},
	} {
		got, err := r.mcpRowsFor(policyCtx(p), 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names(got) {
			if n == "smind-browser" {
				t.Errorf("%s: the browser row must be absent without a Browser policy", name)
			}
		}
	}
	var root int
	got, err := r.mcpRowsForDelivery(policyCtx(&McpPolicy{Browser: socketPolicy(&root)}), 1, true, 4242)
	if err != nil {
		t.Fatal(err)
	}
	if root != 4242 {
		t.Fatalf("the socket is opened for the agent process: %d", root)
	}
	if !reflect.DeepEqual(names(got), []string{"a", "smind-browser"}) {
		t.Fatalf("names = %v", names(got))
	}
	row := got[1]
	var args []string
	_ = json.Unmarshal([]byte(row.Args), &args)
	var env map[string]string
	_ = json.Unmarshal([]byte(row.Env), &env)
	if row.Transport != "stdio" || row.Command != "/opt/smind" || !reflect.DeepEqual(args, []string{"mcp", "browser"}) || env["SMIND_RUN_ID"] != "run-1" || env["SMIND_BROWSER_SOCKET"] != "/run/sock/x.sock" || !row.Enabled {
		t.Fatalf("the browser row: %+v", row)
	}
	if _, has := env["SMIND_BROWSER_TOKEN"]; has {
		t.Fatalf("no token exists any more: %v", env)
	}
	for k, v := range env {
		if strings.Contains(strings.ToLower(k), "token") || strings.Contains(strings.ToLower(k), "secret") || strings.Contains(v, "tok") {
			t.Fatalf("the row's env must carry no secret: %s=%s", k, v)
		}
	}
}

func TestMcpRowsFor_ReservedBrowserNameNeverReachesAnyRun(t *testing.T) {
	t.Parallel()
	hostile := stdioRow("smind-browser", "/tmp/evil")
	r := policyRunner(stdioRow("a", "x"), hostile)
	for name, ctx := range map[string]context.Context{
		"no contract":         context.Background(),
		"instructions only":   WithRunContract(context.Background(), RunContract{Instructions: "x"}),
		"managed, no browser": policyCtx(&McpPolicy{Orchestration: true, Env: seatEnv()}),
	} {
		got, err := r.mcpRowsFor(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range got {
			if row.Name == "smind-browser" {
				t.Errorf("%s: a registered smind-browser server reached a run: %+v", name, row)
			}
		}
	}
	var root2 int
	got, _ := r.mcpRowsForDelivery(policyCtx(&McpPolicy{Browser: socketPolicy(&root2)}), 1, true, 4242)
	n := 0
	for _, row := range got {
		if row.Name == "smind-browser" {
			n++
			if row.Command != "/opt/smind" {
				t.Fatalf("only the daemon's own entry may carry the name: %+v", row)
			}
		}
	}
	if n != 1 {
		t.Fatalf("exactly one smind-browser row, got %d", n)
	}
}

func TestWithoutSmindMcp_StripsTheBrowserRowForPluginProviders(t *testing.T) {
	t.Parallel()
	got := withoutSmindMcp([]store.McpServer{stdioRow("a", "x"), stdioRow("smind", "s"), stdioRow("smind-browser", "b")})
	if !reflect.DeepEqual(names(got), []string{"a"}) {
		t.Fatalf("a plugin provider gets neither built-in server: %v", names(got))
	}
}

type socketSpy struct{ opened, revoked, root int }

func (s *socketSpy) policy() *McpPolicy {
	return &McpPolicy{BrowserOnly: true, Browser: &BrowserPolicy{
		Env:    browserEnv(),
		Socket: func(pid int) (string, error) { s.opened++; s.root = pid; return "/run/sock/x.sock", nil },
		Revoke: func() { s.revoked++ },
	}}
}

func TestBrowserSocketIsOpenedOnlyWhereTheRowIsDelivered(t *testing.T) {
	t.Parallel()
	r := policyRunner()
	// A delivery point with a known agent process: the row, the socket opened once for that pid.
	spy := &socketSpy{}
	got, err := r.mcpRowsForDelivery(policyCtx(spy.policy()), 1, true, 777)
	if err != nil || len(got) != 1 {
		t.Fatalf("rows = %+v, err %v", got, err)
	}
	if spy.opened != 1 || spy.root != 777 {
		t.Fatalf("opened %d times for pid %d", spy.opened, spy.root)
	}
	// A path that delivers nothing (codex-native, a plugin provider): no row, no socket.
	spy2 := &socketSpy{}
	got, err = r.mcpRowsForDelivery(policyCtx(spy2.policy()), 1, false, 777)
	if err != nil || len(got) != 0 || spy2.opened != 0 {
		t.Fatalf("a row nobody will receive must not be built or opened for: rows=%v opened=%d err=%v", names(got), spy2.opened, err)
	}
	// An agent whose process id is unknown (the Claude CLI path): no row, no socket: never a weaker check.
	spy3 := &socketSpy{}
	got, err = r.mcpRowsFor(policyCtx(spy3.policy()), 1)
	if err != nil || len(got) != 0 || spy3.opened != 0 {
		t.Fatalf("no known agent process means no browser row and no socket: rows=%v opened=%d err=%v", names(got), spy3.opened, err)
	}
	// A socket that cannot be created: the run goes on without the row.
	bad := &McpPolicy{BrowserOnly: true, Browser: &BrowserPolicy{Env: browserEnv(), Socket: func(int) (string, error) { return "", errors.New("no dir") }}}
	got, err = r.mcpRowsForDelivery(policyCtx(bad), 1, true, 777)
	if err != nil || len(got) != 0 {
		t.Fatalf("a failed socket means no row and no error: rows=%v err=%v", names(got), err)
	}
}

func TestBrowserRowOnlyForOfferedProviders(t *testing.T) {
	t.Parallel()
	reg := providers.New()
	get := func(id string) providers.Spec {
		s, ok := reg.Get(id)
		if !ok {
			t.Fatalf("no provider %q", id)
		}
		return s
	}
	for _, c := range []struct {
		name string
		spec providers.Spec
		have bool
		want bool
	}{
		{"claude-native", get("claude-native"), true, true},
		{"glm", get("glm"), true, true},
		{"opencode", get("opencode"), true, true},
		{"codex-native", get("codex-native"), true, false},
		{"plugin provider", providers.Spec{ID: "p/a", Plugin: "p", Runner: providers.RunnerACP}, true, false},
		{"an agent without MCP servers", providers.Spec{ID: "opencode", NoMCPServers: true}, true, false},
		{"custom acp", providers.Spec{ID: "mine", Custom: true, Runner: providers.RunnerACP}, true, true},
		{"unknown to the registry", providers.Spec{}, false, false},
		{"unknown id", providers.Spec{ID: "nobody"}, true, false},
	} {
		if got := browserRowDelivered(c.spec, c.have); got != c.want {
			t.Errorf("%s: browserRowDelivered = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBrowserSocketIsClosedWhenTheProviderSkipsTheRow(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		inactive, unresolved []string
		revoked              int
	}{
		"nothing skipped":    {nil, nil, 0},
		"another server":     {[]string{"other"}, []string{"third (command \"x\" not found in PATH)"}, 0},
		"browser inactive":   {[]string{"smind-browser"}, nil, 1},
		"browser unresolved": {nil, []string{"smind-browser (command \"/opt/smind\" not found in PATH)"}, 1},
		"a lookalike name":   {[]string{"smind-browser-2", "my-smind-browser"}, nil, 0},
	} {
		spy := &socketSpy{}
		revokeBrowserIfSkipped(policyCtx(spy.policy()), tc.inactive, tc.unresolved)
		if spy.revoked != tc.revoked {
			t.Errorf("%s: revoked %d, want %d", name, spy.revoked, tc.revoked)
		}
	}
	spy := &socketSpy{}
	revokeBrowserAll(policyCtx(spy.policy()))
	if spy.revoked != 1 {
		t.Fatal("revokeBrowserAll must revoke")
	}
	// No policy, no panic.
	revokeBrowserAll(context.Background())
	revokeBrowserIfSkipped(context.Background(), []string{"smind-browser"})
}

// A policy that exists only to carry the browser grant must not turn on strict
// MCP config: that would hide the person's own servers from a claude run that
// never asked for a managed tool set.
func TestStrictMcpConfigIsNotTurnedOnByTheBrowserGrantAlone(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		c    RunContract
		want bool
	}{
		"no policy":                 {RunContract{}, false},
		"instructions only":         {RunContract{Instructions: "x"}, false},
		"browser grant alone":       {RunContract{Mcp: &McpPolicy{BrowserOnly: true, Browser: &BrowserPolicy{}}}, false},
		"managed (peer, no tools)":  {RunContract{Mcp: &McpPolicy{}}, true},
		"orchestration seat":        {RunContract{Mcp: &McpPolicy{Orchestration: true}}, true},
		"managed seat plus browser": {RunContract{Mcp: &McpPolicy{Orchestration: true, Browser: &BrowserPolicy{}}}, true},
	} {
		if got := useStrictMcp(tc.c); got != tc.want {
			t.Errorf("%s: useStrictMcp = %v, want %v", name, got, tc.want)
		}
	}
}

// The claude launch path asks useStrictMcp, not "is there a policy".
func TestClaudeLaunchUsesUseStrictMcp(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("runner.go")
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), "opts = append(opts, claudecode.WithStrictMCPConfig())")
	if i < 0 {
		t.Fatal("the strict-config option is gone from the claude launch path")
	}
	if !strings.Contains(string(src)[max(0, i-400):i], "useStrictMcp(contract)") {
		t.Fatal("the strict-config option must be gated by useStrictMcp, so a browser-only grant does not hide the person's own MCP servers")
	}
}
