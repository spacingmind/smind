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
