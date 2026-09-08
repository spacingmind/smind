package main

import (
	"bytes"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/auth"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
	"github.com/spacingmind/smind/internal/wsapi"
)

func TestRunAccountAddDispatchesCredentialFromStdin(t *testing.T) {
	home := t.TempDir()
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

	stdin := withStdin(t, "sk-test-from-stdin\n")
	defer stdin()
	var stdout bytes.Buffer
	previousStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = write
	defer func() { os.Stdout = previousStdout }()

	if code := run([]string{"account", "add", "openai", "work"}); code != 0 {
		t.Fatalf("run(account add) = %d, want 0", code)
	}
	if err := write.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	if _, err := io.Copy(&stdout, read); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := read.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("sk-test-from-stdin")) {
		t.Fatalf("account add wrote credential to stdout: %q", stdout.String())
	}

	list, err := registry.List()
	if err != nil {
		t.Fatalf("registry.List() error = %v", err)
	}
	if len(list) != 1 || list[0].APIKey == nil || list[0].APIKey.Key != "sk-test-from-stdin" {
		t.Fatalf("stored accounts = %+v, want stdin credential", list)
	}
}

func withStdin(t *testing.T, value string) func() {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	if _, err := write.WriteString(value); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := write.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	previous := os.Stdin
	os.Stdin = read
	return func() {
		os.Stdin = previous
		_ = read.Close()
	}
}
