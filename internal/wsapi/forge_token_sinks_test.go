package wsapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeGHScript is a `gh` stand-in that records argv, environment and stdin to
// files in $FAKE_GH_DIR and answers per $FAKE_GH_MODE. On a failed login it
// echoes the token it read on stdin to stderr -- the worst case for a leak.
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

// lockedBuf is a goroutine-safe sink.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *lockedBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *lockedBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureStdoutStderr redirects the process's stdout and stderr into a buffer until
// the test ends.
func captureStdoutStderr(t *testing.T) *lockedBuf {
	t.Helper()
	buf := &lockedBuf{}
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = w, w
	done := make(chan struct{})
	go func() { _, _ = io.Copy(buf, r); close(done) }()
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = w.Close()
		<-done
		_ = r.Close()
	})
	return buf
}

// dumpDB renders every row of every table of the sqlite file at path, plus
// the row counts of the tables a token must never reach.
func dumpDB(t *testing.T, path string) (dump string, counts map[string]int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	var sb strings.Builder
	counts = map[string]int{}
	for _, tb := range tables {
		r, err := db.Query(`SELECT * FROM "` + tb + `"`)
		if err != nil {
			t.Fatalf("dump %s: %v", tb, err)
		}
		counts[tb] = 0
		cols, _ := r.Columns()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			counts[tb]++
			fmt.Fprintf(&sb, "%s:", tb)
			for _, v := range vals {
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				fmt.Fprintf(&sb, " %v", v)
			}
			sb.WriteByte('\n')
		}
		r.Close()
	}
	return sb.String(), counts
}

// TestForgeToken_NeverLogged runs a sentinel token through every success and
// failure path of the sign-in surface -- over every channel, with a gh that
// echoes the token back on its stderr -- and then looks for the sentinel in
// everything the daemon could have written it to: log output, stdout/stderr,
// the event bus, the database (request_log, run_events, run logs and every
// other table), every RPC result and error (message and data.detail), and
// the argv and environment of the gh subprocess. The only place it may be is
// gh's stdin. Not parallel: it swaps process-wide log/stdio and PATH.
func TestForgeToken_NeverLogged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake gh")
	}
	rec, bin := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGHScript), 0o755); err != nil {
		t.Fatal(err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+origPath)
	t.Setenv("FAKE_GH_DIR", rec)

	logs := &lockedBuf{}
	oldSlog, oldLogW := slog.Default(), log.Writer()
	log.SetOutput(logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(oldSlog); log.SetOutput(oldLogW) })
	stdio := captureStdoutStderr(t)

	dbPath := filepath.Join(t.TempDir(), "smind.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	wm := workspace.New(db)
	newTestTask(t, wm, "")
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok") // the real gh client: PATH finds the fake
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	topics := make([]string, 0, len(knownTopics))
	for k := range knownTopics {
		topics = append(topics, k)
	}
	sort.Strings(topics)
	sub := subscribeTopics(api, topics...)

	rpcOut := &lockedBuf{}
	record := func(env envelope) envelope {
		b, _ := json.Marshal(env)
		rpcOut.Write(b)
		rpcOut.Write([]byte("\n"))
		return env
	}
	tokParams := map[string]any{"host": "github.com", "token": authSentinel}
	reset := func() { _ = os.Remove(filepath.Join(rec, "authed")); api.forgeAuth.invalidate() }

	wantCodes := map[string]string{"ok": "", "bad": forge.CodeTokenInvalid, "scopes": forge.CodeTokenScopes, "crash": forge.CodeError}
	for _, mode := range []string{"ok", "bad", "scopes", "crash"} {
		t.Setenv("FAKE_GH_MODE", mode)
		for _, ch := range []Channel{ChannelLoopback, ChannelTLS, ChannelE2EE} {
			reset()
			env := record(callOn(t, api, ch, "forge.setToken", tokParams))
			switch want := wantCodes[mode]; {
			case want == "" && env.Error != nil:
				t.Fatalf("%s/%s: unexpected error %+v", mode, ch, env.Error)
			case want != "" && (env.Error == nil || env.Error.Code != want):
				t.Fatalf("%s/%s: response %+v, want code %s", mode, ch, env, want)
			}
			record(callOn(t, api, ch, "forge.status", map[string]any{"refresh": true}))
			if mode == "ok" {
				record(callOn(t, api, ch, "forge.logout", map[string]any{"host": "github.com"}))
			}
		}
	}

	// Refused and malformed requests that carry the sentinel.
	t.Setenv("FAKE_GH_MODE", "ok")
	reset()
	record(callOn(t, api, ChannelPlain, "forge.setToken", tokParams))
	record(callOn(t, api, ChannelPlain, "forge.logout", map[string]any{"host": "github.com", "token": authSentinel}))
	record(callOn(t, api, ChannelLoopback, "forge.setToken", map[string]any{"token": authSentinel + " with a space"}))
	record(callOn(t, api, ChannelLoopback, "forge.setToken", map[string]any{"host": authSentinel + "/x", "token": authSentinel}))
	record(callOn(t, api, ChannelLoopback, "forge.setToken", map[string]any{"token": map[string]any{"x": authSentinel}}))
	record(callOn(t, api, ChannelLoopback, "forge.setToken", json.RawMessage(`["`+authSentinel+`"]`)))
	record(callOn(t, api, ChannelLoopback, "forge.logout", map[string]any{"host": authSentinel + "/x"}))
	for method, raw := range map[string]string{"forge.setToken": `{"token":"` + authSentinel + `"}`, "forge.logout": `{"host":"github.com","token":"` + authSentinel + `"}`} {
		_, err := api.hs[method](context.Background(), &requestContext{runScoped: true, channel: ChannelLoopback}, json.RawMessage(raw))
		b, _ := json.Marshal(codedError(err))
		rpcOut.Write(b)
	}
	// A login the caller gave up on: forge_timeout.
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = api.hs["forge.setToken"](cctx, &requestContext{channel: ChannelLoopback}, json.RawMessage(`{"token":"`+authSentinel+`"}`))
	if c := codedError(err); c.Code != forge.CodeTimeout {
		t.Fatalf("cancelled login = %+v, want forge_timeout", c)
	}
	b, _ := json.Marshal(codedError(err))
	rpcOut.Write(b)
	// gh not installed.
	t.Setenv("PATH", t.TempDir())
	record(callOn(t, api, ChannelLoopback, "forge.setToken", tokParams))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+origPath)

	time.Sleep(100 * time.Millisecond) // let any async publisher or writer finish
	events, _ := json.Marshal(queued(sub))
	dbDump, counts := dumpDB(t, dbPath)
	stdin := readFileOrEmpty(t, filepath.Join(rec, "stdin.log"))
	if !strings.Contains(stdin, authSentinel) {
		t.Fatalf("the token never reached gh's stdin (the test proves nothing): %q", stdin)
	}
	if !strings.Contains(dbDump, "Task") || len(counts) < 10 {
		t.Fatalf("the database dump does not cover the tables (tables=%d)", len(counts))
	}
	if counts["request_log"] != 0 || counts["run_events"] != 0 {
		t.Fatalf("rows written to request_log/run_events: %v", counts)
	}
	sinks := map[string]string{
		"log output":       logs.String(),
		"stdout/stderr":    stdio.String(),
		"event bus":        string(events),
		"database":         dbDump,
		"rpc responses":    rpcOut.String(),
		"gh argv":          readFileOrEmpty(t, filepath.Join(rec, "argv.log")),
		"gh environment":   readFileOrEmpty(t, filepath.Join(rec, "env.log")),
		"daemon.info":      envJSON(t, callOn(t, api, ChannelLoopback, "daemon.info", nil)),
		"forge.status now": envJSON(t, callOn(t, api, ChannelLoopback, "forge.status", nil)),
	}
	for name, content := range sinks {
		if strings.Contains(content, authSentinel) {
			t.Errorf("the token is in the %s:\n%s", name, sinkSnippet(content, authSentinel))
		}
	}
	if !strings.Contains(rpcOut.String(), `"detail"`) {
		t.Error("no data.detail was exercised by the failure paths")
	}
}

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func envJSON(t *testing.T, env envelope) string {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sinkSnippet(s, needle string) string {
	i := strings.Index(s, needle)
	lo, hi := max(0, i-80), min(len(s), i+len(needle)+80)
	return s[lo:hi]
}
