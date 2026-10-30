package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

// TestServer_ProviderList_RoundTrip proves provider.list's wire shape --
// a no-params request producing
// {providers: [{id, label, kind, credentialKind, accountProvider}]} --
// round-trips over a real WebSocket connection, returning every provider
// taskrunner.SupportedProviders declares (the single source of truth
// RunPrompt's dispatch stays in sync with), including GLM's "cli" Kind --
// the signal accounts-dialog.tsx uses to render it as externally-managed
// instead of omitting it or offering a credential form -- and, for the
// other three, the CredentialKind/AccountProvider pair accounts-dialog.tsx
// derives its Connect-button and manual-add rows from (Item 7d), rather
// than from its own hand-maintained provider constants.
func TestServer_ProviderList_RoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "provider.list", nil)
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("provider.list error = %v", resp.Error.Message)
	}
	var result providerListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode provider.list result: %v", err)
	}

	type wantInfo struct {
		label           string
		kind            taskrunner.ProviderKind
		credentialKind  taskrunner.ProviderCredentialKind
		accountProvider string
	}
	want := map[taskrunner.Provider]wantInfo{
		"claude-native": {label: "Claude Code", credentialKind: taskrunner.CredentialKindOAuth, accountProvider: "anthropic"},
		"glm":           {label: "GLM", kind: taskrunner.ProviderKindCLI},
		"kimi":          {label: "Kimi", credentialKind: taskrunner.CredentialKindAPIKey, accountProvider: "kimi"},
		"codex-native":  {label: "Codex", credentialKind: taskrunner.CredentialKindOAuth, accountProvider: "openai"},
		"opencode":      {label: "OpenCode", kind: taskrunner.ProviderKindCLI},
		"copilot":       {label: "GitHub Copilot", kind: taskrunner.ProviderKindCLI},
		"cline":         {label: "Cline", kind: taskrunner.ProviderKindCLI},
		"goose":         {label: "Goose", kind: taskrunner.ProviderKindCLI},
		"cursor":        {label: "Cursor", kind: taskrunner.ProviderKindCLI},
	}
	if len(result.Providers) != len(want) {
		t.Fatalf("provider.list returned %d providers, want %d: %+v", len(result.Providers), len(want), result.Providers)
	}
	for _, p := range result.Providers {
		info, ok := want[p.ID]
		if !ok {
			t.Fatalf("provider.list returned unknown provider id %q", p.ID)
		}
		if p.Label != info.label {
			t.Fatalf("provider %q label = %q, want %q", p.ID, p.Label, info.label)
		}
		if p.Kind != info.kind {
			t.Fatalf("provider %q kind = %q, want %q", p.ID, p.Kind, info.kind)
		}
		if p.CredentialKind != info.credentialKind {
			t.Fatalf("provider %q credentialKind = %q, want %q", p.ID, p.CredentialKind, info.credentialKind)
		}
		if p.AccountProvider != info.accountProvider {
			t.Fatalf("provider %q accountProvider = %q, want %q", p.ID, p.AccountProvider, info.accountProvider)
		}
		if len(p.Modes) == 0 || p.DefaultMode == "" {
			t.Fatalf("provider %q has no mode catalog: modes %v default %q", p.ID, p.Modes, p.DefaultMode)
		}
		delete(want, p.ID)
	}
}

// callProviderTest sends a provider.test request for provider and decodes
// its providerTestResult, failing the test on a wire-level error.
func callProviderTest(t *testing.T, ws *websocket.Conn, id, provider string) providerTestResult {
	t.Helper()
	sendRequest(t, ws, id, "provider.test", map[string]string{"provider": provider})
	resp := readEnvelopeFor(t, ws, id, 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("provider.test error = %v", resp.Error.Message)
	}
	var result providerTestResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode provider.test result: %v", err)
	}
	return result
}

// TestServer_ProviderTest_CredentialMissing proves an account-credential
// provider (here "anthropic") with no stored account at all comes back
// not-ok with a detail explaining why -- the "credential missing" case a
// freshly-installed daemon hits for every provider before anyone connects
// an account.
func TestServer_ProviderTest_CredentialMissing(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	result := callProviderTest(t, ws, "1", "anthropic")
	if result.OK {
		t.Fatalf("provider.test ok = true, want false (no account configured): detail = %q", result.Detail)
	}
	if result.Detail == "" {
		t.Fatalf("provider.test detail is empty, want an explanation")
	}
}

// TestServer_ProviderTest_CredentialPresent proves an account-credential
// provider with a stored, unexpired OAuth account comes back ok.
func TestServer_ProviderTest_CredentialPresent(t *testing.T) {
	t.Parallel()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	if _, err := registry.AddOAuth("anthropic", "work", accounts.OAuthCredential{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}
	runner := newTestRunner(wm)
	srv := newTestWSServerWithAccounts(t, wm, registry, runner, s, "tok")
	ws := dialWS(t, srv, "tok")

	result := callProviderTest(t, ws, "1", "anthropic")
	if !result.OK {
		t.Fatalf("provider.test ok = false, want true: detail = %q", result.Detail)
	}
	if result.Detail == "" {
		t.Fatalf("provider.test detail is empty, want a description of which account it used")
	}
}

// TestServer_ProviderTest_CredentialExpired proves a stored OAuth account
// whose ExpiresAt is already in the past comes back not-ok, distinct from
// having no account at all (accounts.Registry.EnsureFresh is the one that
// would actually refresh it; this diagnostic never does).
func TestServer_ProviderTest_CredentialExpired(t *testing.T) {
	t.Parallel()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	if _, err := registry.AddOAuth("openai", "work", accounts.OAuthCredential{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}
	runner := newTestRunner(wm)
	srv := newTestWSServerWithAccounts(t, wm, registry, runner, s, "tok")
	ws := dialWS(t, srv, "tok")

	result := callProviderTest(t, ws, "1", "openai")
	if result.OK {
		t.Fatalf("provider.test ok = true, want false (credential expired): detail = %q", result.Detail)
	}
}

// TestServer_ProviderTest_CLIMissing proves a cli-kind provider (GLM, whose
// providerCLICommand entry is npx) comes back not-ok when its executable
// isn't on $PATH, with a detail naming the missing binary. Overrides $PATH
// to a directory with nothing in it rather than relying on the test host's
// real npx being absent (which it may well not be) -- this can't run in
// parallel with other tests since t.Setenv forbids it.
func TestServer_ProviderTest_CLIMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	result := callProviderTest(t, ws, "1", "glm")
	if result.OK {
		t.Fatalf("provider.test ok = true, want false (npx not on PATH): detail = %q", result.Detail)
	}
	if result.Detail == "" {
		t.Fatalf("provider.test detail is empty, want it to name the missing binary")
	}
}

// TestTestProviderCLI_MissingDetailNamesPathSource proves the not-found
// detail stays as-is without SMIND_PATH_SOURCE and, with it (set by the
// desktop app when it starts the daemon), names the source and the effective
// PATH.
func TestTestProviderCLI_MissingDetailNamesPathSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("SMIND_PATH_SOURCE", "")

	plain := testProviderCLI([]string{"smind-no-such-cli"})
	if plain.OK {
		t.Fatalf("ok = true for a missing binary: %q", plain.Detail)
	}
	if !strings.HasPrefix(plain.Detail, `"smind-no-such-cli" not found on PATH: `) || strings.Contains(plain.Detail, "daemon started by") {
		t.Fatalf("detail without SMIND_PATH_SOURCE = %q, want the plain not-found message", plain.Detail)
	}

	t.Setenv("SMIND_PATH_SOURCE", "desktop app, PATH from login shell")
	withSrc := testProviderCLI([]string{"smind-no-such-cli"})
	want := " (daemon started by the desktop app, PATH from login shell: " + dir + ")"
	if withSrc.OK || !strings.HasSuffix(withSrc.Detail, want) {
		t.Fatalf("detail = %q (ok=%v), want suffix %q", withSrc.Detail, withSrc.OK, want)
	}
}

// TestServer_ProviderTest_CLIPresent proves a cli-kind provider comes back
// ok once its executable resolves on $PATH -- a fake, harmless executable
// standing in for the real npx/codex/claude binary.
func TestServer_ProviderTest_CLIPresent(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "npx")
	content := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		// npm installs npx as npx.cmd; a shim-only install is exactly
		// what provider.test must report as present (PATHEXT lookup).
		fake = filepath.Join(dir, "npx.cmd")
		content = "@echo off\r\nexit /b 0\r\n"
	}
	if err := os.WriteFile(fake, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake npx: %v", err)
	}
	t.Setenv("PATH", dir)

	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	result := callProviderTest(t, ws, "1", "glm")
	if !result.OK {
		t.Fatalf("provider.test ok = false, want true (fake npx on PATH): detail = %q", result.Detail)
	}
}

func TestProviderList_ImagesField(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "provider.list", nil)
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("provider.list error = %v", resp.Error.Message)
	}
	var raw struct {
		Providers []struct {
			ID     string `json:"id"`
			Images string `json:"images"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(resp.Result, &raw); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"claude-native": "yes", "codex-native": "yes", "glm": "no", "kimi": "unknown",
		"opencode": "yes", "copilot": "unknown", "cline": "unknown", "goose": "unknown", "cursor": "unknown"}
	if len(raw.Providers) != len(want) {
		t.Fatalf("providers = %+v", raw.Providers)
	}
	for _, p := range raw.Providers {
		if p.Images != want[p.ID] {
			t.Errorf("%s images = %q, want %q", p.ID, p.Images, want[p.ID])
		}
	}
}

// TestProviderList_CooldownUntil: provider.list reports cooldownUntil
// (RFC 3339 UTC) only for a provider on cooldown and leaves every other
// field unchanged (ADR-0041 F1).
func TestProviderList_CooldownUntil(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	list := func(id string) (providerListResult, map[string]json.RawMessage) {
		sendRequest(t, ws, id, "provider.list", nil)
		resp := readEnvelopeFor(t, ws, id, 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("provider.list error = %v", resp.Error.Message)
		}
		var typed providerListResult
		if err := json.Unmarshal(resp.Result, &typed); err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Providers []map[string]json.RawMessage `json:"providers"`
		}
		if err := json.Unmarshal(resp.Result, &raw); err != nil {
			t.Fatal(err)
		}
		byID := map[string]json.RawMessage{}
		for _, p := range raw.Providers {
			var id string
			_ = json.Unmarshal(p["id"], &id)
			if c, ok := p["cooldownUntil"]; ok {
				byID[id] = c
			}
		}
		return typed, byID
	}

	before, cooling := list("1")
	if len(cooling) != 0 {
		t.Fatalf("cooldownUntil present with no cooldown: %v", cooling)
	}

	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if err := db.SetProviderCooldown("glm", until, "r", time.Now()); err != nil {
		t.Fatal(err)
	}
	after, cooling := list("2")
	if len(cooling) != 1 {
		t.Fatalf("cooldownUntil on %v, want only glm", cooling)
	}
	var got string
	_ = json.Unmarshal(cooling["glm"], &got)
	if got != until.Format(time.RFC3339) || !strings.HasSuffix(got, "Z") {
		t.Fatalf("glm cooldownUntil = %q, want %q (UTC)", got, until.Format(time.RFC3339))
	}
	for i := range after.Providers {
		a, b := after.Providers[i], before.Providers[i]
		a.CooldownUntil = ""
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("provider %s changed besides cooldownUntil:\n%+v\n%+v", a.ID, a, b)
		}
	}
}
