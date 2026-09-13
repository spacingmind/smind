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

// TestRunAccountTestPrintsOkAndFailureDetail drives `smind account test
// <provider>` against a real wsapi.Handler (like account_test.go's
// account-add test -- provider.test never touches the network or mutates
// state, it's just a $PATH lookup or a stored-credential check, so there's
// no need for account_login_test.go's fake-server workaround). It covers
// both outcomes handleProviderTest can report: an unconfigured CLI
// provider's binary missing from $PATH (failure, exit 1) and an
// account-credential provider id with no stored account (also a failure,
// exit 1, but through the other code path) -- both print the detail line,
// success additionally prefixed with "ok\t".
func TestRunAccountTestPrintsOkAndFailureDetail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	// Make sure the CLI-kind lookup actually fails regardless of the host's
	// real $PATH.
	t.Setenv("PATH", t.TempDir())

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

	out, code := runCapturingStdout(t, []string{"account", "test", "claude-native"})
	if code != 1 {
		t.Fatalf("run(account test claude-native) = %d, want 1 (stdout: %s)", code, out)
	}
	if bytes.Contains([]byte(out), []byte("ok\t")) {
		t.Fatalf("stdout = %q, want no ok prefix for a missing binary", out)
	}
	if len(bytes.TrimSpace([]byte(out))) == 0 {
		t.Fatalf("stdout = %q, want a non-empty detail line", out)
	}

	out, code = runCapturingStdout(t, []string{"account", "test", "openai"})
	if code != 1 {
		t.Fatalf("run(account test openai) = %d, want 1 (stdout: %s)", code, out)
	}
	if bytes.Contains([]byte(out), []byte("ok\t")) {
		t.Fatalf("stdout = %q, want no ok prefix with no stored account", out)
	}
}

func TestRunAccountTestUsage(t *testing.T) {
	if code := run([]string{"account", "test"}); code != 2 {
		t.Fatalf("run(account test) with no args = %d, want 2", code)
	}
	if code := run([]string{"account", "test", "openai", "extra"}); code != 2 {
		t.Fatalf("run(account test) with two args = %d, want 2", code)
	}
}

// runCapturingStdout runs the CLI with args and returns what it wrote to
// os.Stdout alongside its exit code, mirroring the stdout-capture pattern
// duplicated across account_test.go and account_login_test.go.
func runCapturingStdout(t *testing.T, args []string) (string, int) {
	t.Helper()
	var stdout bytes.Buffer
	previousStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = write
	defer func() { os.Stdout = previousStdout }()

	code := run(args)

	if err := write.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	if _, err := io.Copy(&stdout, read); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := read.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return stdout.String(), code
}
