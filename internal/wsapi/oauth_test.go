package wsapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/terminal"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeOAuthLogin is a minimal accounts.Login standing in for a real vendor,
// so account.oauthStart's happy path can be exercised end-to-end over a
// real WebSocket connection without a real browser or a real provider on
// the other end -- see accounts/login_coordinator_test.go's fakeLogin for
// the same idea one layer down.
type fakeOAuthLogin struct {
	addr string
	path string
	// subject is the vendor account subject Exchange reports ("" = none).
	subject string
}

func (f *fakeOAuthLogin) CallbackAddr() string { return f.addr }
func (f *fakeOAuthLogin) CallbackPath() string { return f.path }

func (f *fakeOAuthLogin) AuthorizeURL(state string, _ accounts.PKCECodes) string {
	return "https://example.invalid/authorize?state=" + url.QueryEscape(state)
}

func (f *fakeOAuthLogin) Exchange(_ context.Context, code, _ string, _ accounts.PKCECodes) (accounts.ExchangeResult, error) {
	return accounts.ExchangeResult{
		Credential: accounts.OAuthCredential{
			AccessToken:  "access-for-" + code,
			RefreshToken: "refresh-for-" + code,
			ExpiresAt:    time.Now().Add(time.Hour),
		},
		Subject: f.subject,
	}, nil
}

// freeAddr returns a "127.0.0.1:port" address that's free at the moment
// it's returned, for handing a fixed callback address to a fakeOAuthLogin
// before the login flow itself binds it.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("Listener.Close() error = %v", err)
	}
	return addr
}

// newTestWSServerWithCoordinator builds the same /ws handler wsapi.New
// does, except with coord (rather than New's own
// accounts.NewDefaultLoginCoordinator) wired into methodHandlers -- the
// seam a real daemon never needs (see server.go's New, which always uses
// the default coordinator) but this package's own tests do, to drive
// account.oauthStart's happy path against a fake Login instead of a real
// vendor.
func newTestWSServerWithCoordinator(t *testing.T, wm *workspace.Manager, acctReg *accounts.Registry, coord *accounts.LoginCoordinator, db *store.Store, token string) *httptest.Server {
	t.Helper()
	reg, err := runs.New(db)
	if err != nil {
		t.Fatalf("runs.New() error = %v", err)
	}
	treg, err := terminal.New(db)
	if err != nil {
		t.Fatalf("terminal.New() error = %v", err)
	}
	profReg := profiles.New(db)
	mcpReg := mcpservers.New(db)

	hs := methodHandlers(wm, acctReg, nil, reg, treg, profReg, mcpReg, coord, db)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		c := newConn(wsTransport{ws}, hs)
		c.serve(r.Context())
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestServer_AccountOAuthStart_InvalidParams(t *testing.T) {
	t.Parallel()

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	coord := accounts.NewLoginCoordinator(registry, map[string]accounts.Login{})
	srv := newTestWSServerWithCoordinator(t, wm, registry, coord, s, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "account.oauthStart", map[string]any{"provider": "anthropic"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("account.oauthStart with no label: error = nil, want error")
	}
}

func TestServer_AccountOAuthStart_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	coord := accounts.NewLoginCoordinator(registry, map[string]accounts.Login{})
	srv := newTestWSServerWithCoordinator(t, wm, registry, coord, s, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "account.oauthStart", map[string]any{"provider": "bogus", "label": "l"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("account.oauthStart for an unsupported provider: error = nil, want error")
	}
}

// TestServer_AccountOAuthStart_HappyPath drives account.oauthStart
// end-to-end over a real WebSocket connection: the request streams an
// "authorizeUrl" event, this test plays the vendor's role by GETting that
// URL's callback address with a matching code+state, and the request's
// terminal response is the new account in the same credential-free shape
// account.add/account.list return.
func TestServer_AccountOAuthStart_HappyPath(t *testing.T) {
	t.Parallel()

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	login := &fakeOAuthLogin{addr: freeAddr(t), path: "/callback"}
	coord := accounts.NewLoginCoordinator(registry, map[string]accounts.Login{"fake": login})
	srv := newTestWSServerWithCoordinator(t, wm, registry, coord, s, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "account.oauthStart", map[string]any{"provider": "fake", "label": "my-label"})

	event := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if event.Event != "authorizeUrl" {
		t.Fatalf("first message = %+v, want an authorizeUrl event", event)
	}
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(event.Params, &params); err != nil {
		t.Fatalf("decode authorizeUrl params: %v", err)
	}
	u, err := url.Parse(params.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", params.URL, err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatalf("authorizeUrl %q carries no state", params.URL)
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=test-code&state=%s", login.addr, url.QueryEscape(state)))
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()

	term := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if term.Error != nil {
		t.Fatalf("account.oauthStart error = %v", term.Error.Message)
	}
	if bytes.Contains(term.Result, []byte("access-for-test-code")) || bytes.Contains(term.Result, []byte("refresh-for-test-code")) {
		t.Fatalf("account.oauthStart returned credential material: %s", term.Result)
	}
	var created accountResult
	if err := json.Unmarshal(term.Result, &created); err != nil {
		t.Fatalf("decode account.oauthStart result: %v", err)
	}
	if created.Provider != "fake" || created.Label != "my-label" {
		t.Fatalf("created account = %+v, want provider=fake label=my-label", created)
	}
	if created.CredentialType != accounts.CredentialTypeOAuth {
		t.Fatalf("credential type = %q, want %q", created.CredentialType, accounts.CredentialTypeOAuth)
	}
	// ADR-0033 D6: the result says a row was created, omits adopted when
	// false, and the fake vendor sent no subject so there is no identity.
	if !bytes.Contains(term.Result, []byte(`"created":true`)) || bytes.Contains(term.Result, []byte(`"adopted"`)) || !bytes.Contains(term.Result, []byte(`"hasIdentity":false`)) {
		t.Fatalf("account.oauthStart result = %s, want created:true, hasIdentity:false and no adopted key", term.Result)
	}

	stored, err := registry.Get(created.ID)
	if err != nil {
		t.Fatalf("registry.Get() error = %v", err)
	}
	if stored.OAuth == nil || stored.OAuth.RefreshToken != "refresh-for-test-code" {
		t.Fatalf("stored OAuth = %+v, want refresh-for-test-code", stored.OAuth)
	}
}

// oauthStartOverWS runs one account.oauthStart request over ws against
// login's fake vendor, playing the browser's part (GET the callback with the
// state from the streamed authorizeUrl event), and returns the terminal
// envelope. A fresh callback address per call keeps sequential logins apart.
func oauthStartOverWS(t *testing.T, ws *websocket.Conn, reqID string, login *fakeOAuthLogin, subject string, params map[string]any) envelope {
	t.Helper()
	login.addr = freeAddr(t)
	login.subject = subject

	sendRequest(t, ws, reqID, "account.oauthStart", params)
	event := readEnvelopeFor(t, ws, reqID, 5*time.Second)
	if event.Event != "authorizeUrl" {
		// The login failed before an authorize URL was issued.
		return event
	}
	var ev struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(event.Params, &ev); err != nil {
		t.Fatalf("decode authorizeUrl params: %v", err)
	}
	u, err := url.Parse(ev.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", ev.URL, err)
	}
	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=test-code&state=%s", login.addr, url.QueryEscape(u.Query().Get("state"))))
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()
	return readEnvelopeFor(t, ws, reqID, 5*time.Second)
}

// TestServer_AccountOAuthStart_Relogin covers ADR-0033 D6 over the wire: the
// result carries created/adopted, account.list carries hasIdentity (never the
// identity itself), and accountId + a different identity is identity_mismatch.
func TestServer_AccountOAuthStart_Relogin(t *testing.T) {
	t.Parallel()

	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	login := &fakeOAuthLogin{path: "/callback"}
	coord := accounts.NewLoginCoordinator(registry, map[string]accounts.Login{"fake": login})
	srv := newTestWSServerWithCoordinator(t, wm, registry, coord, s, "tok")
	ws := dialWS(t, srv, "tok")

	// First login: created, with an identity, adopted omitted.
	first := oauthStartOverWS(t, ws, "1", login, "subject-1", map[string]any{"provider": "fake", "label": "work"})
	if first.Error != nil {
		t.Fatalf("first account.oauthStart error = %v", first.Error.Message)
	}
	var created struct {
		accountResult
		Created bool  `json:"created"`
		Adopted *bool `json:"adopted"`
	}
	mustDecode(t, first.Result, &created)
	if !created.Created || created.Adopted != nil || !created.HasIdentity {
		t.Fatalf("first result = %s, want created:true, no adopted key, hasIdentity:true", first.Result)
	}

	// Same vendor account again: replaced in place, created:false, label kept.
	again := oauthStartOverWS(t, ws, "2", login, "subject-1", map[string]any{"provider": "fake", "label": "ignored"})
	if again.Error != nil {
		t.Fatalf("second account.oauthStart error = %v", again.Error.Message)
	}
	var updated struct {
		accountResult
		Created bool `json:"created"`
	}
	mustDecode(t, again.Result, &updated)
	if updated.Created || updated.ID != created.ID || updated.Label != "work" {
		t.Fatalf("second result = %s, want created:false on account %d labelled work", again.Result, created.ID)
	}

	// A row-level Re-login that lands on a different vendor account.
	mismatch := oauthStartOverWS(t, ws, "3", login, "subject-2", map[string]any{"provider": "fake", "label": "x", "accountId": created.ID})
	if mismatch.Error == nil || !strings.Contains(mismatch.Error.Message, "identity_mismatch") {
		t.Fatalf("mismatched re-login = %+v, want an identity_mismatch error", mismatch)
	}
	if strings.Contains(mismatch.Error.Message, "subject-") {
		t.Fatalf("error %q leaks the subject", mismatch.Error.Message)
	}

	// An unknown accountId fails before any authorizeUrl is issued.
	sendRequest(t, ws, "4", "account.oauthStart", map[string]any{"provider": "fake", "label": "x", "accountId": 9999})
	unknown := readEnvelopeFor(t, ws, "4", 5*time.Second)
	if unknown.Error == nil || !strings.Contains(unknown.Error.Message, "login_unknown_account") {
		t.Fatalf("unknown accountId = %+v, want a login_unknown_account error", unknown)
	}

	// A legacy row (no identity) adopts on a row-level Re-login.
	legacy, err := registry.AddOAuth("fake", "legacy", accounts.OAuthCredential{AccessToken: "old", RefreshToken: "old-r", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("AddOAuth() error = %v", err)
	}
	adopt := oauthStartOverWS(t, ws, "5", login, "subject-3", map[string]any{"provider": "fake", "label": "x", "accountId": legacy.ID})
	if adopt.Error != nil {
		t.Fatalf("adopting account.oauthStart error = %v", adopt.Error.Message)
	}
	var adopted struct {
		accountResult
		Created bool `json:"created"`
		Adopted bool `json:"adopted"`
	}
	mustDecode(t, adopt.Result, &adopted)
	if adopted.Created || !adopted.Adopted || adopted.ID != legacy.ID || !adopted.HasIdentity {
		t.Fatalf("adopt result = %s, want adopted:true created:false on account %d", adopt.Result, legacy.ID)
	}
}
