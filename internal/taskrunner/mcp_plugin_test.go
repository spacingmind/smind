package taskrunner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/store"
)

const pluginSecret = "SENTINEL-plugin-secret-4f1c"

type pluginSettings map[string]string

func (p pluginSettings) Setting(plugin, key string) (string, bool) {
	v, ok := p[plugin+"/"+key]
	return v, ok
}

// newPluginMcpEnv installs and enables a plugin contributing one stdio MCP
// server whose env holds a secret placeholder, over st's overlay.
func newPluginMcpEnv(t *testing.T, st *store.Store) (*plugins.Manager, *bool) {
	t.Helper()
	on := true
	mgr := plugins.New(plugins.Options{
		Home: t.TempDir(), Store: st, Version: "0.15.0", Enabled: func() bool { return on },
		Settings: pluginSettings{"acme/token": pluginSecret},
	})
	st.SetOverlay(mgr)
	src := t.TempDir()
	manifest := `{"id":"acme","name":"Acme","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},` +
		`"settings":[{"key":"token","type":"secret","label":"t"}],` +
		`"contributes":{"mcpServers":[{"name":"files","transport":"stdio","command":["go","version"],"env":{"TOKEN":"${setting.token}"}}]}}`
	if err := os.WriteFile(filepath.Join(src, "smind-plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := mgr.Install(t.Context(), src)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := mgr.Enable("acme", in.Consent.Fingerprint); err != nil {
		t.Fatalf("enable: %v", err)
	}
	return mgr, &on
}

// TestMcpContribution_OwnedReadOnly_InjectedAcpAndClaude_RemovedOnDisable
// proves an enabled plugin's MCP server is injected into runs exactly like a
// user row on BOTH paths (ACP and claude-native), that its row is read-only,
// and that it disappears from the next run when the plugin is disabled,
// removed or the global switch is off, with no restart.
func TestMcpContribution_OwnedReadOnly_InjectedAcpAndClaude_RemovedOnDisable(t *testing.T) {
	t.Run("acp", func(t *testing.T) {
		r, st, task, dump := newMcpTestEnv(t, "stdio,http")
		mgr, on := newPluginMcpEnv(t, st)

		runMcpPrompt(t, r, task)
		got := readMcpDump(t, dump)
		if !strings.Contains(got, `"name":"acme__files"`) || !strings.Contains(got, pluginSecret) {
			t.Fatalf("the owned server must reach the agent with its secret substituted, got %s", got)
		}

		// Read-only through the registry every caller shares.
		reg := mcpservers.New(st)
		rows, _ := reg.List()
		var owned store.McpServer
		for _, m := range rows {
			if m.OwnerPlugin == "acme" {
				owned = m
			}
		}
		if owned.Name != "acme:files" || strings.Contains(owned.Env, pluginSecret) {
			t.Fatalf("listing row = %+v", owned)
		}
		var oe *store.OwnedRowError
		for _, err := range []error{
			func() error {
				_, e := reg.Update(store.McpServer{ID: owned.ID, Name: "x", Transport: "stdio", Command: "y"})
				return e
			}(),
			reg.Delete(owned.ID),
			func() error { _, e := reg.SetEnabled(owned.ID, false); return e }(),
		} {
			if !errors.As(err, &oe) || oe.Plugin != "acme" {
				t.Fatalf("a write to an owned row must be refused naming the plugin, got %v", err)
			}
		}

		// Disabled: the NEXT run does not get it. No restart.
		if _, err := mgr.Disable("acme"); err != nil {
			t.Fatal(err)
		}
		runMcpPrompt(t, r, task)
		if got := readMcpDump(t, dump); got != "[]" {
			t.Fatalf("after disable the run must carry no server, got %s", got)
		}
		in, _ := mgr.Get("acme")
		if _, err := mgr.Enable("acme", in.Consent.Fingerprint); err != nil {
			t.Fatal(err)
		}
		runMcpPrompt(t, r, task)
		if !strings.Contains(readMcpDump(t, dump), "acme__files") {
			t.Fatal("re-enabling applies to the next run")
		}
		*on = false
		runMcpPrompt(t, r, task)
		if got := readMcpDump(t, dump); got != "[]" {
			t.Fatalf("global switch off: no server, got %s", got)
		}
		*on = true
		if err := mgr.Remove("acme", false); err != nil {
			t.Fatal(err)
		}
		runMcpPrompt(t, r, task)
		if got := readMcpDump(t, dump); got != "[]" {
			t.Fatalf("removed: no server, got %s", got)
		}
	})

	t.Run("claude-native", func(t *testing.T) {
		r, st, task := newClaudeMcpEnv(t)
		mgr, on := newPluginMcpEnv(t, st)

		args := runClaudeMcp(t, r, task)
		if _, ok := flagValue(args, "--mcp-config"); !ok {
			t.Fatalf("--mcp-config missing for an owned server: %q", args)
		}
		cfg, err := os.ReadFile(filepath.Join(*task.WorktreePath, "mcp-config-content"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(cfg), `"acme__files"`) || !strings.Contains(string(cfg), pluginSecret) {
			t.Fatalf("the config file must hold the owned server with its secret: %s", cfg)
		}
		for _, a := range args {
			if strings.Contains(a, pluginSecret) {
				t.Fatalf("the secret must never be in argv: %q", args)
			}
		}

		_ = os.Remove(filepath.Join(*task.WorktreePath, "mcp-config-content"))
		if _, err := mgr.Disable("acme"); err != nil {
			t.Fatal(err)
		}
		args = runClaudeMcp(t, r, task)
		if _, ok := flagValue(args, "--mcp-config"); ok {
			t.Fatalf("a disabled plugin's server must not be injected: %q", args)
		}
		in, _ := mgr.Get("acme")
		_, _ = mgr.Enable("acme", in.Consent.Fingerprint)
		*on = false
		args = runClaudeMcp(t, r, task)
		if _, ok := flagValue(args, "--mcp-config"); ok {
			t.Fatalf("global switch off: nothing injected: %q", args)
		}
	})
}

// TestMcpContribution_OwnedServer_SecretNotInRunEvents: the substituted value
// is scrubbed from run errors like any MCP secret (it rides in the rows
// mcpSecrets scans) and appears in no emitted event.
func TestMcpContribution_OwnedServer_SecretNotInRunEvents(t *testing.T) {
	r, st, task, _ := newMcpTestEnv(t, "stdio,http")
	newPluginMcpEnv(t, st)
	events := runMcpPrompt(t, r, task)
	for _, e := range events {
		if strings.Contains(fmt.Sprintf("%+v", e), pluginSecret) {
			t.Fatalf("event %+v leaks the plugin secret", e)
		}
	}
	rows, err := r.mcpServersForWorkspace(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range mcpSecrets(rows) {
		found = found || s == pluginSecret
	}
	if !found {
		t.Fatal("the substituted secret must be among the values scrubbed from run errors")
	}
}
