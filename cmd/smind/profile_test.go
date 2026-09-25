package main

import (
	"bytes"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

// newTestProfileDaemon starts a real wsapi.Handler-backed test server and
// writes a config.yaml pointing dialDaemon at it, mirroring
// TestRunAccountAddDispatchesCredentialFromStdin's setup. Returns the
// store (for asserting on stored profiles directly) and a func to capture
// what the CLI printed to stdout while running fn.
func newTestProfileDaemon(t *testing.T) (home string, s *store.Store) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("SMIND_HOME", home)

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	registry := accounts.New(s)
	token, err := auth.LoadOrCreateToken(home)
	if err != nil {
		t.Fatalf("LoadOrCreateToken() error = %v", err)
	}
	handler, err := wsapi.Handler(workspace.New(s), registry, nil, s, token)
	if err != nil {
		t.Fatalf("wsapi.Handler() error = %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("server:\n  port: "+u.Port()+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home, s
}

func TestRunProfileAddPrintsCreatedRow(t *testing.T) {
	_, s := newTestProfileDaemon(t)

	var code int
	out := captureStdout(t, func() {
		code = run([]string{"profile", "add", "UI work", "claude-native", "--approval-policy", "manual"})
	})
	if code != 0 {
		t.Fatalf("run(profile add) = %d, want 0; stdout: %s", code, out)
	}
	if !bytes.Contains([]byte(out), []byte("UI work")) {
		t.Fatalf("stdout = %q, want it to contain the created profile's name", out)
	}

	list, err := profiles.New(s).List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "UI work" || list[0].Provider != "claude-native" || list[0].ApprovalPolicy != "manual" {
		t.Fatalf("stored profiles = %+v, want one UI work/claude-native/manual profile", list)
	}
}

func TestRunProfileAddInvalidProviderExitsNonzero(t *testing.T) {
	newTestProfileDaemon(t)

	var code int
	out := captureStdout(t, func() {
		code = run([]string{"profile", "add", "x", "not-a-real-provider"})
	})
	if code == 0 {
		t.Fatalf("run(profile add, bad provider) = 0, want nonzero; stdout: %s", out)
	}
}

func TestRunProfileLsListsAddedProfile(t *testing.T) {
	newTestProfileDaemon(t)

	if code := run([]string{"profile", "add", "UI work", "claude-native"}); code != 0 {
		t.Fatalf("run(profile add) = %d, want 0", code)
	}

	var code int
	out := captureStdout(t, func() {
		code = run([]string{"profile", "ls"})
	})
	if code != 0 {
		t.Fatalf("run(profile ls) = %d, want 0", code)
	}
	if !bytes.Contains([]byte(out), []byte("UI work")) {
		t.Fatalf("profile ls output = %q, want it to list the added profile", out)
	}
}

func TestRunProfileRmRemovesProfile(t *testing.T) {
	_, s := newTestProfileDaemon(t)

	var code int
	addOut := captureStdout(t, func() {
		code = run([]string{"profile", "add", "throwaway", "claude-native"})
	})
	if code != 0 {
		t.Fatalf("run(profile add) = %d, want 0", code)
	}
	list, err := profiles.New(s).List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %+v, %v, want one profile; add output: %s", list, err, addOut)
	}
	id := list[0].ID

	if code := run([]string{"profile", "rm", strconv.FormatInt(id, 10)}); code != 0 {
		t.Fatalf("run(profile rm) = %d, want 0", code)
	}

	after, err := profiles.New(s).List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("stored profiles after rm = %+v, want none", after)
	}
}
