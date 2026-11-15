package taskrunner

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func browserEnv() map[string]string {
	return map[string]string{"SMIND_HOME": "/home/x", "SMIND_RUN_ID": "run-1"}
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
	got, err := r.mcpRowsFor(policyCtx(&McpPolicy{Browser: &BrowserPolicy{Env: browserEnv()}}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names(got), []string{"a", "smind-browser"}) {
		t.Fatalf("names = %v", names(got))
	}
	row := got[1]
	var args []string
	_ = json.Unmarshal([]byte(row.Args), &args)
	var env map[string]string
	_ = json.Unmarshal([]byte(row.Env), &env)
	if row.Transport != "stdio" || row.Command != "/opt/smind" || !reflect.DeepEqual(args, []string{"mcp", "browser"}) || env["SMIND_RUN_ID"] != "run-1" || !row.Enabled {
		t.Fatalf("the browser row: %+v", row)
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
	got, _ := r.mcpRowsFor(policyCtx(&McpPolicy{Browser: &BrowserPolicy{Env: browserEnv()}}), 1)
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

type tokenSpy struct{ issued, revoked int }

func (s *tokenSpy) policy() *McpPolicy {
	return &McpPolicy{BrowserOnly: true, Browser: &BrowserPolicy{
		Env:    browserEnv(),
		Issue:  func() string { s.issued++; return "tok-issued" },
		Revoke: func() { s.revoked++ },
	}}
}

func TestBrowserCapabilityIsIssuedOnlyWhereTheRowIsDelivered(t *testing.T) {
	t.Parallel()
	r := policyRunner()
	// A delivery point: the row carries the freshly issued capability, once.
	spy := &tokenSpy{}
	p := spy.policy()
	got, err := r.mcpRowsFor(policyCtx(p), 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("rows = %+v, err %v", got, err)
	}
	var env map[string]string
	_ = json.Unmarshal([]byte(got[0].Env), &env)
	if spy.issued != 1 || env["SMIND_BROWSER_TOKEN"] != "tok-issued" || env["SMIND_RUN_ID"] != "run-1" {
		t.Fatalf("issued %d, env %v", spy.issued, env)
	}
	if _, leaked := p.Browser.Env["SMIND_BROWSER_TOKEN"]; leaked {
		t.Fatal("the capability must not be written into the policy's own env map")
	}
	// A path that delivers nothing (codex-native, a plugin provider): no row, no capability.
	spy2 := &tokenSpy{}
	got, err = r.mcpRowsForDelivery(policyCtx(spy2.policy()), 1, false)
	if err != nil || len(got) != 0 || spy2.issued != 0 {
		t.Fatalf("a row nobody will receive must not be built or minted for: rows=%v issued=%d err=%v", names(got), spy2.issued, err)
	}
	if browserRowDelivered(true) || !browserRowDelivered(false) {
		t.Fatal("a plugin provider never gets the browser row, an ordinary provider does")
	}
}

func TestBrowserCapabilityIsRevokedWhenTheProviderSkipsTheRow(t *testing.T) {
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
		spy := &tokenSpy{}
		revokeBrowserIfSkipped(policyCtx(spy.policy()), tc.inactive, tc.unresolved)
		if spy.revoked != tc.revoked {
			t.Errorf("%s: revoked %d, want %d", name, spy.revoked, tc.revoked)
		}
	}
	spy := &tokenSpy{}
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
