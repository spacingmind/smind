package wsapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/ghctx"
)

// authSentinel is a token-shaped secret the forge scrubber does NOT know how
// to redact (it is not a gh_/github_pat_ shape), so a leak can only be
// stopped by the code never putting it anywhere.
const authSentinel = "SENTINEL-s3cr3t-T0KEN-value-9f2c"

const authedHosts = `{"hosts":{"github.com":[{"state":"success","active":true,"host":"github.com","login":"ann","scopes":"repo, read:org","tokenSource":"keyring","gitProtocol":"https"}]}}`

// ghFake is a stand-in gh behind ghctx's runners: it keeps a sign-in state,
// records every call (argv, and the stdin of the stdin runner), and can gate
// the first `auth status`.
type ghFake struct {
	mu          sync.Mutex
	authed      bool
	source      string // tokenSource reported
	statusCalls int
	argv        [][]string
	stdins      []string
	loginErr    error
	versionErr  error
	gate        chan struct{} // `auth status` blocks until closed, when non-nil
	entered     chan struct{}
}

func (g *ghFake) client() *ghctx.Client {
	return ghctx.New(ghctx.WithRunner(g.run), ghctx.WithStdinRunner(g.runIn))
}

func (g *ghFake) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	g.argv = append(g.argv, append([]string(nil), args...))
	joined := strings.Join(args, " ")
	switch {
	case joined == "--version":
		err := g.versionErr
		g.mu.Unlock()
		return "gh version 2.101.0 (2026-09-15)\n", err
	case joined == "auth status --json hosts":
		g.statusCalls++
		gate, entered, authed, src := g.gate, g.entered, g.authed, g.source
		g.mu.Unlock()
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		if gate != nil {
			<-gate
		}
		if !authed {
			return `{"hosts":{}}`, nil
		}
		if src == "" {
			src = "keyring"
		}
		q, _ := json.Marshal(src)
		return strings.Replace(authedHosts, `"keyring"`, string(q), 1), nil
	case strings.HasPrefix(joined, "auth logout"):
		g.authed = false
		g.mu.Unlock()
		return "", nil
	case args[0] == "repo":
		authed := g.authed
		g.mu.Unlock()
		if !authed {
			return "", errors.New("gh: exit status 4: To get started with GitHub CLI, please run:  gh auth login")
		}
		return `{"nameWithOwner":"o/r","url":"https://github.com/o/r"}`, nil
	}
	g.mu.Unlock()
	return "", errors.New("gh: unexpected call: " + joined)
}

func (g *ghFake) runIn(_ context.Context, _ string, stdin string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.argv = append(g.argv, append([]string(nil), args...))
	g.stdins = append(g.stdins, stdin)
	if g.loginErr != nil {
		return "", g.loginErr
	}
	g.authed = true
	return "", nil
}

func (g *ghFake) calls() [][]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([][]string(nil), g.argv...)
}

func (g *ghFake) count(prefix string) int {
	n := 0
	for _, a := range g.calls() {
		if strings.HasPrefix(strings.Join(a, " "), prefix) {
			n++
		}
	}
	return n
}

func (g *ghFake) loginCalls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.stdins)
}

func newAuthAPI(t *testing.T, g *ghFake) *API {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	api.SetGithubClient(g.client())
	return api
}

// callOn sends one request over a fresh connection tagged ch (e2ee goes
// through ServeTransport, the relay path) and returns the response. params
// is sent exactly as given when it is a json.RawMessage.
func callOn(t *testing.T, api *API, ch Channel, method string, params any) envelope {
	t.Helper()
	var raw json.RawMessage
	switch p := params.(type) {
	case nil:
	case json.RawMessage:
		raw = p
	default:
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if ch == ChannelE2EE {
		go api.ServeTransport(ctx, server)
	} else {
		go api.serveLocal(ctx, server, ch)
	}
	defer client.Close()
	data, _ := json.Marshal(envelope{ID: "1", Method: method, Params: raw})
	if err := client.Send(data); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Receive()
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(resp, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func decodeAuth(t *testing.T, env envelope, v any) {
	t.Helper()
	if env.Error != nil {
		t.Fatalf("error response: %+v", env.Error)
	}
	if err := json.Unmarshal(env.Result, v); err != nil {
		t.Fatalf("decode %s: %v", env.Result, err)
	}
}

func wantAuthErr(t *testing.T, env envelope, code string) *rpcError {
	t.Helper()
	if env.Error == nil || env.Error.Code != code {
		t.Fatalf("response = %+v, want error code %q", env, code)
	}
	return env.Error
}

func authErrDetail(e *rpcError) string {
	m, _ := e.Data.(map[string]any)
	s, _ := m["detail"].(string)
	return s
}

// --- channel -------------------------------------------------------------

func TestChannel_Tagging(t *testing.T) {
	t.Parallel()
	req := func(remote string, mod func(*http.Request)) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.RemoteAddr = remote
		if mod != nil {
			mod(r)
		}
		return r
	}
	cases := []struct {
		name string
		r    *http.Request
		want Channel
	}{
		{"loopback v4", req("127.0.0.1:5000", nil), ChannelLoopback},
		{"loopback v6", req("[::1]:5000", nil), ChannelLoopback},
		{"tls from anywhere", req("203.0.113.9:5000", func(r *http.Request) { r.TLS = &tls.ConnectionState{} }), ChannelTLS},
		{"tls beats loopback", req("127.0.0.1:5000", func(r *http.Request) { r.TLS = &tls.ConnectionState{} }), ChannelTLS},
		{"remote plain", req("203.0.113.9:5000", nil), ChannelPlain},
		{"remote ipv6 plain", req("[2001:db8::1]:5000", nil), ChannelPlain},
		{"private lan plain", req("192.168.1.20:5000", nil), ChannelPlain},
		{"garbage addr", req("not-an-addr", nil), ChannelPlain},
		{"proxy on this host that forwards plain http", req("127.0.0.1:5000", func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") }), ChannelPlain},
		{"proxy on this host that forwards https", req("127.0.0.1:5000", func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }), ChannelLoopback},
	}
	for _, tc := range cases {
		if got := ChannelOf(tc.r); got != tc.want {
			t.Errorf("%s: ChannelOf = %q, want %q", tc.name, got, tc.want)
		}
	}
	for ch, want := range map[Channel]bool{ChannelLoopback: true, ChannelTLS: true, ChannelE2EE: true, ChannelPlain: false, "": false} {
		if ch.Secure() != want {
			t.Errorf("Channel(%q).Secure() = %v", ch, !want)
		}
	}
}

// TestChannel_TaggingEndToEnd: the tag really lands on requestContext, for
// the /ws handler (with a faked peer address and TLS state), ServeTransport
// and ServeDevice.
func TestChannel_TaggingEndToEnd(t *testing.T) {
	t.Parallel()
	api := newAuthAPI(t, &ghFake{})
	api.hs["test.channel"] = func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		return map[string]any{"channel": rc.channel, "can": canSetToken(rc)}, nil
	}
	ask := func(fake func(*http.Request)) (string, bool) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fake != nil {
				fake(r)
			}
			api.Handler.ServeHTTP(w, r)
		}))
		defer srv.Close()
		ws := dialWS(t, srv, "tok")
		defer ws.Close()
		env := callWS(t, ws, "test.channel", nil)
		var out struct {
			Channel string
			Can     bool
		}
		decodeAuth(t, env, &out)
		return out.Channel, out.Can
	}
	if ch, can := ask(nil); ch != "loopback" || !can {
		t.Errorf("httptest peer = %q can=%v, want loopback true", ch, can)
	}
	if ch, can := ask(func(r *http.Request) { r.RemoteAddr = "203.0.113.9:4444" }); ch != "plain" || can {
		t.Errorf("remote peer = %q can=%v, want plain false", ch, can)
	}
	if ch, can := ask(func(r *http.Request) { r.RemoteAddr = "203.0.113.9:4444"; r.TLS = &tls.ConnectionState{} }); ch != "tls" || !can {
		t.Errorf("remote tls peer = %q can=%v, want tls true", ch, can)
	}
	for name, serve := range map[string]func(*API, context.Context, Transport){
		"ServeTransport": func(a *API, ctx context.Context, tr Transport) { a.ServeTransport(ctx, tr) },
		"ServeDevice":    func(a *API, ctx context.Context, tr Transport) { a.ServeDevice(ctx, tr, "dev-1") },
	} {
		client, server := newPipeTransportPair()
		ctx, cancel := context.WithCancel(context.Background())
		go serve(api, ctx, server)
		data, _ := json.Marshal(envelope{ID: "1", Method: "test.channel"})
		_ = client.Send(data)
		raw, err := client.Receive()
		if err != nil {
			t.Fatal(err)
		}
		var env envelope
		_ = json.Unmarshal(raw, &env)
		var out struct {
			Channel string
			Can     bool
		}
		decodeAuth(t, env, &out)
		if out.Channel != "e2ee" || !out.Can {
			t.Errorf("%s = %+v, want e2ee true", name, out)
		}
		cancel()
		client.Close()
	}
}

// --- forge.status --------------------------------------------------------

func TestForgeStatus_Shapes(t *testing.T) {
	t.Parallel()
	t.Run("authenticated", func(t *testing.T) {
		g := &ghFake{authed: true}
		api := newAuthAPI(t, g)
		env := callOn(t, api, ChannelLoopback, "forge.status", nil)
		var got map[string]any
		decodeAuth(t, env, &got)
		if got["provider"] != "github" || got["canSetToken"] != true {
			t.Fatalf("status = %v", got)
		}
		cli := got["cli"].(map[string]any)
		if cli["installed"] != true || cli["version"] != "2.101.0" {
			t.Fatalf("cli = %v", cli)
		}
		hosts := got["hosts"].([]any)
		h := hosts[0].(map[string]any)
		if len(hosts) != 1 || h["host"] != "github.com" || h["authenticated"] != true || h["login"] != "ann" ||
			h["protocol"] != "https" || h["source"] != "keyring" {
			t.Fatalf("hosts = %v", hosts)
		}
		if sc := h["scopes"].([]any); len(sc) != 2 || sc[0] != "repo" || sc[1] != "read:org" {
			t.Fatalf("scopes = %v", sc)
		}
	})
	t.Run("not authenticated", func(t *testing.T) {
		api := newAuthAPI(t, &ghFake{})
		var got forgeStatusBody
		decodeAuth(t, callOn(t, api, ChannelLoopback, "forge.status", nil), &got)
		if !got.CLI.Installed || got.Hosts == nil || len(got.Hosts) != 0 {
			t.Fatalf("status = %+v", got)
		}
	})
	t.Run("several hosts", func(t *testing.T) {
		api := newAuthAPI(t, &ghFake{})
		api.SetGithubClient(ghctx.New(ghctx.WithRunner(func(_ context.Context, _ string, args ...string) (string, error) {
			if args[0] == "--version" {
				return "gh version 2.101.0 (x)\n", nil
			}
			return `{"hosts":{"github.com":[{"state":"success","active":true,"host":"github.com","login":"a","scopes":"repo","tokenSource":"GH_TOKEN","gitProtocol":"ssh"}],
				"ghe.corp.example":[{"state":"error","active":true,"host":"ghe.corp.example","login":"b","tokenSource":"/home/b/.config/gh/hosts.yml","gitProtocol":"https"}]}}`, nil
		})))
		var got forgeStatusBody
		decodeAuth(t, callOn(t, api, ChannelLoopback, "forge.status", nil), &got)
		if len(got.Hosts) != 2 || got.Hosts[0].Host != "ghe.corp.example" || got.Hosts[0].Authenticated || got.Hosts[0].Source != "file" ||
			got.Hosts[1].Host != "github.com" || got.Hosts[1].Source != "env" || got.Hosts[1].Protocol != "ssh" {
			t.Fatalf("hosts = %+v", got.Hosts)
		}
	})
	t.Run("gh missing", func(t *testing.T) {
		g := &ghFake{versionErr: errors.New(`exec: "gh": executable file not found in $PATH`)}
		api := newAuthAPI(t, g)
		var got forgeStatusBody
		decodeAuth(t, callOn(t, api, ChannelLoopback, "forge.status", nil), &got)
		if got.CLI.Installed || got.Hosts == nil || len(got.Hosts) != 0 || got.Provider != "github" {
			t.Fatalf("status = %+v", got)
		}
	})
	t.Run("canSetToken follows the channel", func(t *testing.T) {
		api := newAuthAPI(t, &ghFake{})
		for ch, want := range map[Channel]bool{ChannelLoopback: true, ChannelTLS: true, ChannelE2EE: true, ChannelPlain: false} {
			var got forgeStatusBody
			decodeAuth(t, callOn(t, api, ch, "forge.status", nil), &got)
			if got.CanSetToken == nil || *got.CanSetToken != want {
				t.Errorf("channel %s: canSetToken = %v, want %v", ch, got.CanSetToken, want)
			}
		}
	})
	t.Run("a run-scoped caller reads but cannot set", func(t *testing.T) {
		api := newAuthAPI(t, &ghFake{})
		res, err := api.hs["forge.status"](context.Background(), &requestContext{runScoped: true, channel: ChannelLoopback}, nil)
		if err != nil || *res.(forgeStatusBody).CanSetToken {
			t.Fatalf("run-scoped status = %+v, %v", res, err)
		}
	})
}

func TestForgeStatus_TokenSourceMapped(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]string{
		"keyring":                      "keyring",
		"GH_TOKEN":                     "env",
		"GITHUB_TOKEN":                 "env",
		"/home/u/.config/gh/hosts.yml": "file",
		`C:\Users\u\gh\hosts.yml`:      "file",
	} {
		api := newAuthAPI(t, &ghFake{authed: true, source: src})
		raw, _ := json.Marshal(callOn(t, api, ChannelLoopback, "forge.status", nil))
		var got forgeStatusBody
		decodeAuth(t, callOn(t, api, ChannelLoopback, "forge.status", nil), &got)
		if got.Hosts[0].Source != want {
			t.Errorf("tokenSource %q -> %q, want %q", src, got.Hosts[0].Source, want)
		}
		if strings.Contains(string(raw), "/home/") || strings.Contains(string(raw), `C:\\`) || strings.Contains(string(raw), "hosts.yml") {
			t.Errorf("tokenSource %q leaked a path: %s", src, raw)
		}
	}
}

type authClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *authClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *authClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClockedAuth(t *testing.T, g *ghFake) (*API, *authClock) {
	t.Helper()
	api := newAuthAPI(t, g)
	clk := &authClock{t: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	api.forgeAuth.now = clk.now
	return api, clk
}

func TestForgeStatus_CachedAndSingleFlight(t *testing.T) {
	t.Parallel()
	g := &ghFake{authed: true, gate: make(chan struct{}), entered: make(chan struct{}, 1)}
	api, clk := newClockedAuth(t, g)
	f := api.forgeAuth

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.status(context.Background(), false); err != nil {
				t.Errorf("status: %v", err)
			}
		}()
	}
	<-g.entered
	time.Sleep(100 * time.Millisecond) // let the others reach the flight
	close(g.gate)
	wg.Wait()
	if n := g.count("auth status"); n != 1 {
		t.Fatalf("8 concurrent reads ran gh auth status %d times, want 1 (single-flight)", n)
	}

	clk.advance(59 * time.Second)
	if _, err := f.status(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if n := g.count("auth status"); n != 1 {
		t.Fatalf("a read inside the 60 s TTL ran gh again (%d calls)", n)
	}
	clk.advance(2 * time.Second)
	if _, err := f.status(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if n := g.count("auth status"); n != 2 {
		t.Fatalf("a read after the TTL ran gh %d times total, want 2", n)
	}
}

func TestForgeStatus_RefreshFloor(t *testing.T) {
	t.Parallel()
	g := &ghFake{authed: true}
	api, clk := newClockedAuth(t, g)
	f := api.forgeAuth
	read := func(refresh bool) {
		if _, err := f.status(context.Background(), refresh); err != nil {
			t.Fatal(err)
		}
	}
	read(false)
	if g.count("auth status") != 1 {
		t.Fatal("first read")
	}
	read(true)
	if n := g.count("auth status"); n != 2 {
		t.Fatalf("first refresh: %d calls, want 2", n)
	}
	read(true)
	clk.advance(4 * time.Second)
	read(true)
	if n := g.count("auth status"); n != 2 {
		t.Fatalf("refreshes inside the 5 s floor ran gh (%d calls, want 2)", n)
	}
	clk.advance(2 * time.Second)
	read(true)
	if n := g.count("auth status"); n != 3 {
		t.Fatalf("a refresh after the floor: %d calls, want 3", n)
	}
	// Hourly cap: with 12 forced refreshes already in the last hour a refresh
	// is served from the cache; one fewer and it reads again.
	f.mu.Lock()
	now := clk.now()
	f.lastForced = now.Add(-time.Minute)
	f.forced = f.forced[:0]
	for i := 0; i < 12; i++ {
		f.forced = append(f.forced, now.Add(-time.Duration(i+1)*time.Minute))
	}
	capped := !f.refreshAllowedLocked(now)
	f.forced = f.forced[1:]
	opened := f.refreshAllowedLocked(now)
	f.mu.Unlock()
	if !capped || !opened {
		t.Fatalf("hourly cap: 12 in the hour allowed=%v (want false), 11 allowed=%v (want true)", !capped, opened)
	}
	clk.advance(2 * time.Hour)
	f.mu.Lock()
	if !f.refreshAllowedLocked(clk.now()) {
		t.Error("a refresh two hours later is still capped")
	}
	f.mu.Unlock()
}

func subscribeTopics(api *API, topics ...string) *subscriber {
	s := newSubscriber()
	s.subscribe(topics)
	api.bus.register(s)
	return s
}

func TestForgeStatusTopic_OnlyOnChange(t *testing.T) {
	t.Parallel()
	g := &ghFake{}
	api, clk := newClockedAuth(t, g)
	sub := subscribeTopics(api, TopicForgeStatus)
	f := api.forgeAuth
	read := func() {
		clk.advance(2 * time.Minute) // past the TTL: a real read each time
		if _, err := f.status(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	}
	read() // baseline
	read() // same state
	if got := queued(sub); len(got) != 0 {
		t.Fatalf("events without a change: %+v", got)
	}
	g.mu.Lock()
	g.authed = true
	g.mu.Unlock()
	read() // a poll that sees a change (signed in elsewhere)
	got := queued(sub)
	if len(got) != 1 || got[0].Topic != TopicForgeStatus {
		t.Fatalf("events after a change = %+v, want one forge.status", got)
	}
	b, _ := json.Marshal(got[0].Payload)
	var p struct {
		Status map[string]any `json:"status"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if _, has := p.Status["canSetToken"]; has {
		t.Fatalf("the event carries canSetToken (per-connection data): %s", b)
	}
	if hosts, _ := p.Status["hosts"].([]any); len(hosts) != 1 || p.Status["provider"] != "github" {
		t.Fatalf("event status = %s", b)
	}
	read() // unchanged again
	if got := queued(sub); len(got) != 1 {
		t.Fatalf("a repeat read published again: %d events", len(got))
	}
}

// --- forge.setToken ------------------------------------------------------

func TestForgeSetToken_StdinOnly(t *testing.T) {
	t.Parallel()
	g := &ghFake{}
	api := newAuthAPI(t, g)
	sub := subscribeTopics(api, TopicForgeStatus)
	for _, ch := range []Channel{ChannelLoopback, ChannelTLS, ChannelE2EE} {
		g.mu.Lock()
		g.authed, g.stdins = false, nil
		g.mu.Unlock()
		env := callOn(t, api, ch, "forge.setToken", map[string]any{"host": "github.com", "token": authSentinel})
		var res struct {
			Status forgeStatusBody `json:"status"`
		}
		decodeAuth(t, env, &res)
		if len(res.Status.Hosts) != 1 || !res.Status.Hosts[0].Authenticated || res.Status.CanSetToken == nil || !*res.Status.CanSetToken {
			t.Fatalf("%s: result = %+v", ch, res)
		}
		g.mu.Lock()
		stdins := append([]string(nil), g.stdins...)
		g.mu.Unlock()
		if len(stdins) != 1 || strings.TrimSpace(stdins[0]) != authSentinel {
			t.Fatalf("%s: stdin = %q, want the token", ch, stdins)
		}
		for _, a := range g.calls() {
			for _, part := range a {
				if strings.Contains(part, authSentinel) {
					t.Fatalf("%s: token in argv %v", ch, a)
				}
			}
		}
		raw, _ := json.Marshal(env)
		if strings.Contains(string(raw), authSentinel) {
			t.Fatalf("%s: token in the response: %s", ch, raw)
		}
	}
	var login []string
	for _, a := range g.calls() {
		if len(a) > 1 && a[0] == "auth" && a[1] == "login" {
			login = a
		}
	}
	if strings.Join(login, " ") != "auth login --hostname github.com --with-token --git-protocol https" {
		t.Fatalf("login argv = %v", login)
	}
	if len(queued(sub)) == 0 {
		t.Fatal("no forge.status event after a successful login")
	}
}

func TestForgeSetToken_DefaultsHostAndRejectsBadInput(t *testing.T) {
	t.Parallel()
	g := &ghFake{}
	api := newAuthAPI(t, g)
	callOn(t, api, ChannelLoopback, "forge.setToken", map[string]any{"token": authSentinel})
	if got := g.calls(); len(got) == 0 || !strings.Contains(strings.Join(got[len(got)-3], " "), "--hostname github.com") {
		t.Fatalf("default host not github.com: %v", got)
	}
	before := g.loginCalls()
	for name, params := range map[string]any{
		"empty token":    map[string]any{"token": ""},
		"space in token": map[string]any{"token": "ghp abc"},
		"newline":        map[string]any{"token": "abc\ndef"},
		"unicode":        map[string]any{"token": "tök€n"},
		"too long":       map[string]any{"token": strings.Repeat("a", 256)},
	} {
		env := callOn(t, api, ChannelLoopback, "forge.setToken", params)
		e := wantAuthErr(t, env, forge.CodeTokenInvalid)
		raw, _ := json.Marshal(env)
		if e.Message != "GitHub rejected that token." || strings.Contains(string(raw), "abc") && name != "space in token" && name != "newline" {
			t.Errorf("%s: response %s", name, raw)
		}
	}
	for _, host := range []string{"-evil", "a b", "x/y", "h:80", "bad host!"} {
		env := callOn(t, api, ChannelLoopback, "forge.setToken", map[string]any{"host": host, "token": "tok"})
		wantAuthErr(t, env, codeInvalid)
	}
	if g.loginCalls() != before {
		t.Fatalf("a rejected request reached gh (%d -> %d login calls)", before, g.loginCalls())
	}
	// A token that is not a string: a fixed sentence, never the json error text.
	env := callOn(t, api, ChannelLoopback, "forge.setToken", json.RawMessage(`{"token": 12345678}`))
	e := wantAuthErr(t, env, codeInvalid)
	if strings.Contains(e.Message, "12345678") || strings.Contains(e.Message, "json") {
		t.Fatalf("message quotes the input: %q", e.Message)
	}
}

func TestForgeSetToken_ErrorsMapped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		code string
		msg  string
	}{
		{"rejected", errors.New("gh: exit status 1: error validating token: HTTP 401: Bad credentials"), forge.CodeTokenInvalid, "GitHub rejected that token."},
		{"scopes", errors.New("gh: exit status 1: error validating token: missing required scope 'read:org'"), forge.CodeTokenScopes, "That token is missing a required scope. A classic token needs `repo` and `read:org`."},
		{"timeout", context.DeadlineExceeded, forge.CodeTimeout, "GitHub didn't answer in time."},
		{"cli missing", errors.New(`exec: "gh": executable file not found in $PATH`), forge.CodeCLIMissing, "The GitHub CLI (`gh`) isn't installed on the daemon host."},
		{"other", errors.New("gh: exit status 1: something odd"), forge.CodeError, "GitHub returned an error."},
	}
	for _, tc := range cases {
		g := &ghFake{loginErr: tc.err}
		api := newAuthAPI(t, g)
		// The failure text carries a token-shaped string and the sentinel.
		g.loginErr = errors.New(tc.err.Error() + " ghp_" + strings.Repeat("k", 30) + " " + authSentinel)
		if tc.code == forge.CodeTimeout {
			g.loginErr = context.DeadlineExceeded
		}
		env := callOn(t, api, ChannelLoopback, "forge.setToken", map[string]any{"token": authSentinel})
		e := wantAuthErr(t, env, tc.code)
		if e.Message != tc.msg {
			t.Errorf("%s: message = %q, want %q", tc.name, e.Message, tc.msg)
		}
		raw, _ := json.Marshal(env)
		if strings.Contains(string(raw), authSentinel) || strings.Contains(string(raw), "ghp_") {
			t.Errorf("%s: secret in the response: %s", tc.name, raw)
		}
		if d := authErrDetail(e); tc.code != forge.CodeTimeout && tc.code != forge.CodeCLIMissing && d == "" {
			t.Errorf("%s: no detail", tc.name)
		}
	}
}

func TestForgeSetToken_InsecureChannelBeforeRead(t *testing.T) {
	t.Parallel()
	g := &ghFake{}
	api := newAuthAPI(t, g)
	for _, method := range []string{"forge.setToken", "forge.logout"} {
		// Valid params, params that would not even parse, and no params: the
		// answer is the same, and nothing is parsed, spawned or read.
		for _, params := range []any{
			map[string]any{"host": "github.com", "token": authSentinel},
			json.RawMessage(`{"token": [1,2,3]}`),
			json.RawMessage(`"just a string"`),
			nil,
		} {
			env := callOn(t, api, ChannelPlain, method, params)
			e := wantAuthErr(t, env, forge.CodeInsecureChannel)
			if e.Message != "This connection isn't encrypted, so smind won't send a token over it. Use the relay, an ssh tunnel or TLS." {
				t.Errorf("%s: message = %q", method, e.Message)
			}
			raw, _ := json.Marshal(env)
			if strings.Contains(string(raw), authSentinel) {
				t.Errorf("%s: token echoed on an insecure channel: %s", method, raw)
			}
		}
	}
	// An unset channel fails closed as well.
	_, err := api.hs["forge.setToken"](context.Background(), &requestContext{}, json.RawMessage(`{"token":"x"}`))
	if codedError(err).Code != forge.CodeInsecureChannel {
		t.Fatalf("zero channel: %v", err)
	}
	if n := len(g.calls()); n != 0 {
		t.Fatalf("a refused request spawned gh %d times: %v", n, g.calls())
	}
}

func TestForgeSetToken_RunScopedRefused(t *testing.T) {
	t.Parallel()
	g := &ghFake{}
	api := newAuthAPI(t, g)
	rc := &requestContext{runScoped: true, channel: ChannelLoopback, local: true}
	for method, raw := range map[string]string{
		"forge.setToken": `{"token":"` + authSentinel + `"}`,
		"forge.logout":   `{"host":"github.com"}`,
	} {
		_, err := api.hs[method](context.Background(), rc, json.RawMessage(raw))
		c := codedError(err)
		if c.Code != codeForbidden || strings.Contains(c.Message, authSentinel) {
			t.Errorf("%s: %+v", method, c)
		}
	}
	if len(g.calls()) != 0 {
		t.Fatalf("a refused agent call spawned gh: %v", g.calls())
	}
}

func TestForgeLogout(t *testing.T) {
	t.Parallel()
	g := &ghFake{authed: true}
	api := newAuthAPI(t, g)
	sub := subscribeTopics(api, TopicForgeStatus)
	var before forgeStatusBody
	decodeAuth(t, callOn(t, api, ChannelLoopback, "forge.status", nil), &before)
	if len(before.Hosts) != 1 {
		t.Fatalf("before = %+v", before)
	}
	var res struct {
		Status forgeStatusBody `json:"status"`
	}
	decodeAuth(t, callOn(t, api, ChannelLoopback, "forge.logout", map[string]any{"host": "github.com"}), &res)
	if len(res.Status.Hosts) != 0 {
		t.Fatalf("after logout = %+v", res.Status)
	}
	if g.count("auth logout --hostname github.com") != 1 {
		t.Fatalf("logout argv: %v", g.calls())
	}
	if len(queued(sub)) != 1 {
		t.Fatalf("events = %+v, want one forge.status", queued(sub))
	}
	wantAuthErr(t, callOn(t, api, ChannelLoopback, "forge.logout", map[string]any{"host": "-bad"}), codeInvalid)
	wantAuthErr(t, callOn(t, api, ChannelLoopback, "forge.logout", nil), codeInvalid)
}

// TestForgeSetToken_LiftsHostSuspension: signed out, the poller suspends the
// host and createPr is blocked with forge_unauthenticated; after setToken the
// suspension is gone at once and the next task.git has no such blocker.
func TestForgeSetToken_LiftsHostSuspension(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	ws, err := wm.GetWorkspace(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	runGitT(t, ws.Path, "remote", "add", "origin", "https://github.com/o/r.git")
	// A commit beyond the fork point, so no earlier blocker hides the forge one.
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, *task.WorktreePath, "add", "f.txt")
	runGitT(t, *task.WorktreePath, "commit", "-m", "work")
	g := &ghFake{}
	api, srv := newGitTestAPI(t, wm, db, nil)
	api.SetGithubClient(g.client())
	ec := newEventConn(t, dialWS(t, srv, "tok"))

	// The refresh finds nobody signed in: typed error, host suspended.
	sendRequest(t, ec.ws, "1", "task.gitRefresh", map[string]any{"taskId": task.ID})
	if resp := ec.nextResponse("1", 5*time.Second); resp.Error == nil || resp.Error.Code != forge.CodeUnauthenticated {
		t.Fatalf("refresh = %+v, want forge_unauthenticated", resp.Error)
	}
	var tg struct {
		Actions map[string]struct {
			Enabled bool   `json:"enabled"`
			Code    string `json:"code"`
		} `json:"actions"`
	}
	ec.call("2", "task.git", map[string]any{"taskId": task.ID}, &tg)
	if tg.Actions["createPr"].Code != forge.CodeUnauthenticated {
		t.Fatalf("createPr before login = %+v, want forge_unauthenticated", tg.Actions["createPr"])
	}

	var res json.RawMessage
	ec.call("3", "forge.setToken", map[string]any{"token": authSentinel}, &res)

	// The next read: no unauthenticated blocker (the recompute runs in the
	// background, so allow it to land).
	deadline := time.Now().Add(5 * time.Second)
	for {
		ec.call("4", "task.git", map[string]any{"taskId": task.ID}, &tg)
		if tg.Actions["createPr"].Code != forge.CodeUnauthenticated {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("createPr still blocked after setToken: %+v", tg.Actions["createPr"])
		}
		time.Sleep(20 * time.Millisecond)
	}
	// And the poller really calls the forge again at once: a refresh works.
	var g2 json.RawMessage
	sendRequest(t, ec.ws, "5", "task.gitRefresh", map[string]any{"taskId": task.ID})
	resp := ec.nextResponse("5", 5*time.Second)
	if resp.Error != nil && resp.Error.Code == forge.CodeUnauthenticated {
		t.Fatalf("refresh after login still unauthenticated: %+v", resp.Error)
	}
	_ = g2
}

// --- the git credential helper hook -------------------------------------

func TestGitNetwork_CredentialHelperOnlyForHttpsAndAuth(t *testing.T) {
	// Not parallel: a user's global url.<x>.insteadOf (https -> ssh is common)
	// legitimately changes the answer, so the test pins an empty git config.
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	g := &ghFake{authed: true}
	api := newAuthAPI(t, g)
	hook := api.NetworkGitConfig()
	repo := func(origin string) string {
		dir := newTestRepo(t)
		if origin != "" {
			runGitT(t, dir, "remote", "add", "origin", origin)
		}
		return dir
	}
	helper := forge.CredentialHelperArgs()
	if len(helper) != 4 || helper[1] != "credential.helper=" || helper[3] != "credential.helper=!gh auth git-credential" {
		t.Fatalf("helper args = %v", helper)
	}
	ctx := context.Background()
	for origin, want := range map[string]bool{
		"https://github.com/o/r.git":            true,
		"HTTPS://GitHub.com/o/r":                true,
		"https://user@github.com/o/r.git":       true,
		"git@github.com:o/r.git":                false,
		"ssh://git@github.com/o/r.git":          false,
		"git://github.com/o/r.git":              false,
		"/some/local/path.git":                  false,
		"https://elsewhere.example.com/o/r.git": false, // not a host gh is signed in to
		"":                                      false,
	} {
		got := hook(ctx, repo(origin), "origin")
		if (len(got) > 0) != want {
			t.Errorf("origin %q: hook = %v, want helper=%v", origin, got, want)
		}
		if want && strings.Join(got, "|") != strings.Join(helper, "|") {
			t.Errorf("origin %q: args = %v", origin, got)
		}
	}
	if got := hook(ctx, repo("https://github.com/o/r.git"), "upstream"); got != nil {
		t.Errorf("a remote that does not exist got %v", got)
	}
	if got := hook(ctx, repo("https://github.com/o/r.git"), "--upload-pack=evil"); got != nil {
		t.Errorf("an option-shaped remote got %v", got)
	}

	// Not authenticated: nothing, even for https github.com.
	g.mu.Lock()
	g.authed = false
	g.mu.Unlock()
	api.forgeAuth.invalidate()
	if got := hook(ctx, repo("https://github.com/o/r.git"), "origin"); got != nil {
		t.Errorf("unauthenticated forge got %v", got)
	}
	// And gh missing.
	g.mu.Lock()
	g.versionErr = errors.New(`exec: "gh": executable file not found in $PATH`)
	g.mu.Unlock()
	api.forgeAuth.invalidate()
	if got := hook(ctx, repo("https://github.com/o/r.git"), "origin"); got != nil {
		t.Errorf("missing gh got %v", got)
	}
}

// --- sinks ---------------------------------------------------------------

func TestForgeNoWsMethodForAgentsOnlyTable(t *testing.T) {
	t.Parallel()
	api := newAuthAPI(t, &ghFake{})
	for _, m := range []string{"forge.status", "forge.setToken", "forge.logout"} {
		if api.hs[m] == nil {
			t.Errorf("%s not registered", m)
		}
	}
	if !knownTopics[TopicForgeStatus] || TopicForgeStatus != "forge.status" {
		t.Fatal("forge.status is not a known topic")
	}
}
