package taskrunner

import (
	"context"
	"testing"

	"github.com/spacingmind/smind/internal/providers"
)

// runACPWithBrowser runs one turn of provider with a browser policy whose socket
// opener is spied, and returns the spy and the run's error.
func runACPWithBrowser(t *testing.T, r *Runner, taskID int64, provider Provider) (*socketSpy, error) {
	t.Helper()
	spy := &socketSpy{}
	ctx := WithRunContract(context.Background(), RunContract{Mcp: spy.policy()})
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(ctx, taskID, taskID, provider, "hi", nil, PermissionSettings{}, "", "", events)
	}()
	_ = drainEvents(events)
	return spy, <-errCh
}

// The reach table decides the ACP path's call site (runner.go, runACP): a provider
// the table says no to (an agent without MCP servers, a plugin provider even when
// its manifest declares MCP) opens no browser socket and builds no row; an offered
// provider opens one; a provider the registry does not know never starts.
func TestACPPath_ReachTableDecidesTheSocket(t *testing.T) {
	t.Setenv("FAKEAGENT_MCP_CAPS", "stdio,http")
	wm, task := newTestTask(t, "")
	reg := providers.New()
	for _, s := range []providers.Spec{
		{ID: "acp-nomcp", Label: "No MCP", Runner: providers.RunnerACP, Command: []string{fakeACPAgentPath}, NoMCPServers: true},
		{ID: "acp-offered", Label: "Offered", Runner: providers.RunnerACP, Command: []string{fakeACPAgentPath}},
	} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	r := New(wm, WithProviderRegistry(reg))

	if spy, err := runACPWithBrowser(t, r, task.ID, "acp-nomcp"); err != nil || spy.opened != 0 {
		t.Fatalf("no-MCP provider: opened %d sockets, err %v; want none", spy.opened, err)
	}
	if spy, err := runACPWithBrowser(t, r, task.ID, "acp-offered"); err != nil || spy.opened != 1 {
		t.Fatalf("offered provider: opened %d sockets, err %v; want one", spy.opened, err)
	}
	if spy, err := runACPWithBrowser(t, r, task.ID, "ghost"); err == nil || spy.opened != 0 {
		t.Fatalf("unknown provider: opened %d sockets, err %v; want an error and no socket", spy.opened, err)
	}
}

// A plugin provider whose manifest DOES declare MCP support (NoMCPServers false) is
// still never handed the browser row: the table, not the flag, excludes it.
func TestACPPath_PluginProviderGetsNoBrowserSocket(t *testing.T) {
	f := newPluginFixture(t, "", func(s *providers.Spec) { s.NoMCPServers = false })
	spy, err := runACPWithBrowser(t, f.r, f.task.ID, "acme/agent")
	if err != nil {
		t.Fatalf("plugin run: %v", err)
	}
	if spy.opened != 0 {
		t.Fatalf("a plugin provider opened %d browser sockets, want none", spy.opened)
	}
}
