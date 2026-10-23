package ghctx

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/forge"
)

const sentinelToken = "SENTINEL-s3cr3t-T0KEN-value-9f2c"

func TestMapTokenSource(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"keyring":                         "keyring",
		"":                                "none",
		"GH_TOKEN":                        "env",
		"GITHUB_TOKEN":                    "env",
		"GH_ENTERPRISE_TOKEN":             "env",
		"/home/u/.config/gh/hosts.yml":    "file",
		`C:\Users\u\AppData\gh\hosts.yml`: "file",
		"/var/folders/x/tmp.1/hosts.yml":  "file",
		"hosts.yml":                       "file",
		"something-unexpected":            "file",
	}
	for in, want := range cases {
		if got := mapTokenSource(in); got != want {
			t.Errorf("mapTokenSource(%q) = %q, want %q", in, got, want)
		}
	}
}

func statusStub(hosts string, ver string, verErr error) Runner {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return ver, verErr
		case "auth status --json hosts":
			return hosts, nil
		}
		return "", errors.New("unexpected: " + strings.Join(args, " "))
	}
}

func TestStatus_ParsesHostsAndMapsSource(t *testing.T) {
	t.Parallel()
	hosts := `{"hosts":{
	  "github.com":[
	    {"state":"success","active":false,"host":"github.com","login":"old","scopes":"repo","tokenSource":"keyring","gitProtocol":"ssh"},
	    {"state":"success","active":true,"host":"github.com","login":"ann","scopes":"gist, read:org, repo","tokenSource":"keyring","gitProtocol":"https"}],
	  "ghe.example.com":[
	    {"state":"error","error":"HTTP 401: Bad credentials","active":true,"host":"ghe.example.com","login":"bob","tokenSource":"/home/bob/.config/gh/hosts.yml","gitProtocol":"https"}]
	}}`
	c := New(WithRunner(statusStub(hosts, "gh version 2.101.0 (2026-09-15)\nhttps://x\n", nil)))
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.CLI.Installed || st.CLI.Version != "2.101.0" || len(st.Hosts) != 2 {
		t.Fatalf("status = %+v", st)
	}
	g, e := st.Hosts[1], st.Hosts[0] // sorted: ghe.example.com, github.com
	if g.Host != "github.com" || !g.Authenticated || g.Login != "ann" || g.Protocol != "https" || g.Source != "keyring" ||
		strings.Join(g.Scopes, "|") != "gist|read:org|repo" {
		t.Fatalf("github.com = %+v (must pick the active account)", g)
	}
	if e.Host != "ghe.example.com" || e.Authenticated || e.Source != "file" || len(e.Scopes) != 0 {
		t.Fatalf("ghe = %+v", e)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "/home/") || strings.Contains(string(b), "hosts.yml") {
		t.Fatalf("status leaked a path: %s", b)
	}
}

func TestStatus_SignedOutAndMissing(t *testing.T) {
	t.Parallel()
	c := New(WithRunner(statusStub(`{"hosts":{}}`, "gh version 2.1.0 (x)\n", nil)))
	st, err := c.Status(context.Background())
	if err != nil || !st.CLI.Installed || st.Hosts == nil || len(st.Hosts) != 0 {
		t.Fatalf("signed out: %+v, %v", st, err)
	}
	c = New(WithRunner(statusStub("", "", errors.New(`exec: "gh": executable file not found in $PATH`))))
	st, err = c.Status(context.Background())
	if err != nil || st.CLI.Installed || st.Hosts == nil || len(st.Hosts) != 0 {
		t.Fatalf("missing gh must be a status, got %+v, %v", st, err)
	}
}

func TestLogin_ErrorsMappedAndTokenRedacted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		err     error
		wantErr string
	}{
		{"401", errors.New("gh: exit status 1: error validating token: HTTP 401: Bad credentials " + sentinelToken), forge.CodeTokenInvalid},
		{"scopes", errors.New("gh: exit status 1: error validating token: missing required scope 'read:org' " + sentinelToken), forge.CodeTokenScopes},
		{"timeout", context.DeadlineExceeded, forge.CodeTimeout},
		{"missing", errors.New(`exec: "gh": executable file not found in $PATH`), forge.CodeCLIMissing},
		{"other", errors.New("gh: exit status 1: boom " + sentinelToken), forge.CodeError},
	}
	for _, tc := range cases {
		c := New(WithStdinRunner(func(context.Context, string, string, ...string) (string, error) { return "", tc.err }))
		err := c.Login(context.Background(), "github.com", sentinelToken)
		var fe *forge.Error
		if !errors.As(err, &fe) || fe.Code != tc.wantErr {
			t.Errorf("%s: err = %v, want code %s", tc.name, err, tc.wantErr)
			continue
		}
		if strings.Contains(fe.Detail, sentinelToken) || strings.Contains(fe.Error(), sentinelToken) {
			t.Errorf("%s: token leaked: detail %q", tc.name, fe.Detail)
		}
	}
}

func TestLogin_RejectsBadHost(t *testing.T) {
	t.Parallel()
	c := New(WithStdinRunner(func(context.Context, string, string, ...string) (string, error) {
		t.Error("runner must not run for a bad host")
		return "", nil
	}))
	for _, h := range []string{"", "-evil", "a b", "x/y", "h:1"} {
		if err := c.Login(context.Background(), h, "tok"); err == nil {
			t.Errorf("Login host %q accepted", h)
		}
	}
}

// fakeGHScript is a `gh` stand-in that records argv, environment and stdin to
// files in $FAKE_GH_DIR and answers per $FAKE_GH_MODE. On a failed login it
// echoes the token it read from stdin to stderr, the worst case for a leak.
const fakeGHScript = `#!/bin/sh
d="$FAKE_GH_DIR"
echo "$@" >> "$d/argv.log"
env >> "$d/env.log"
case "$1 $2" in
"auth login")
  tok=$(cat)
  echo "$tok" >> "$d/stdin.log"
  case "$FAKE_GH_MODE" in
    ok) touch "$d/authed"; exit 0 ;;
    bad) echo "error validating token: HTTP 401: Bad credentials ($tok)" >&2; exit 1 ;;
    scopes) echo "error validating token: missing required scope 'read:org' ($tok)" >&2; exit 1 ;;
    *) echo "unexpected failure ($tok)" >&2; exit 1 ;;
  esac ;;
"auth status")
  if [ -f "$d/authed" ]; then
    echo '{"hosts":{"github.com":[{"state":"success","active":true,"host":"github.com","login":"ann","scopes":"repo, read:org","tokenSource":"keyring","gitProtocol":"https"}]}}'
  else
    echo '{"hosts":{}}'
  fi ;;
"auth logout") rm -f "$d/authed" ;;
*) case "$1" in --version) echo "gh version 2.101.0 (2026-09-15)" ;; esac ;;
esac
`

// installFakeGH puts the fake gh first on PATH and returns the record dir.
func installFakeGH(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake gh")
	}
	bin, rec := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGHScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_DIR", rec)
	return rec
}

func readRec(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(b)
}

// TestLogin_RealSubprocessStdinOnly runs the real runner against a fake gh
// binary: the token is on stdin, and in neither argv nor the child
// environment; the child also gets GH_PROMPT_DISABLED.
func TestLogin_RealSubprocessStdinOnly(t *testing.T) {
	rec := installFakeGH(t)
	t.Setenv("FAKE_GH_MODE", "ok")
	c := New()
	if err := c.Login(context.Background(), "github.com", sentinelToken); err != nil {
		t.Fatalf("Login: %v", err)
	}
	argv, env, stdin := readRec(t, rec, "argv.log"), readRec(t, rec, "env.log"), readRec(t, rec, "stdin.log")
	if !strings.Contains(stdin, sentinelToken) {
		t.Fatalf("token never reached stdin: %q", stdin)
	}
	if strings.Contains(argv, sentinelToken) || strings.Contains(env, sentinelToken) {
		t.Fatalf("token in argv or env:\nargv=%s", argv)
	}
	for _, want := range []string{"auth login", "--hostname github.com", "--with-token", "--git-protocol https"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
	if !strings.Contains(env, "GH_PROMPT_DISABLED=1") {
		t.Error("child env lacks GH_PROMPT_DISABLED=1")
	}
	st, err := c.Status(context.Background())
	if err != nil || !st.Authenticated("github.com") || st.CLI.Version != "2.101.0" {
		t.Fatalf("Status after login = %+v, %v", st, err)
	}
	if err := c.Logout(context.Background(), "github.com"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if st, _ := c.Status(context.Background()); st.Authenticated("github.com") {
		t.Fatal("still authenticated after logout")
	}
	if !strings.Contains(readRec(t, rec, "argv.log"), "auth logout --hostname github.com --user ann") {
		t.Errorf("logout argv = %q", readRec(t, rec, "argv.log"))
	}
}

func TestLogin_RealSubprocessFailureNeverEchoesToken(t *testing.T) {
	for mode, code := range map[string]string{"bad": forge.CodeTokenInvalid, "scopes": forge.CodeTokenScopes, "crash": forge.CodeError} {
		rec := installFakeGH(t)
		t.Setenv("FAKE_GH_MODE", mode)
		err := New().Login(context.Background(), "github.com", sentinelToken)
		var fe *forge.Error
		if !errors.As(err, &fe) || fe.Code != code {
			t.Fatalf("%s: err = %v, want %s", mode, err, code)
		}
		if strings.Contains(fe.Detail, sentinelToken) {
			t.Fatalf("%s: gh echoed the token and it reached the detail: %q", mode, fe.Detail)
		}
		if !strings.Contains(readRec(t, rec, "stdin.log"), sentinelToken) {
			t.Fatalf("%s: token did not reach stdin", mode)
		}
	}
}
