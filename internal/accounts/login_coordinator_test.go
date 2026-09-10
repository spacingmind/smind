package accounts

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeLogin is a Login implementation standing in for a real vendor, for
// LoginCoordinator tests that shouldn't need a real browser or a real
// OAuth provider on the other end of the wire. AuthorizeURL embeds state
// directly in its query string, which is all a test needs to build a
// matching (or deliberately mismatched) callback request.
type fakeLogin struct {
	addr string
	path string

	// exchangeErr, if set, is returned by Exchange instead of a credential.
	exchangeErr error
}

func (f *fakeLogin) CallbackAddr() string { return f.addr }
func (f *fakeLogin) CallbackPath() string { return f.path }

func (f *fakeLogin) AuthorizeURL(state string, pkce PKCECodes) string {
	return "https://example.invalid/authorize?state=" + url.QueryEscape(state) +
		"&challenge=" + url.QueryEscape(pkce.CodeChallenge)
}

func (f *fakeLogin) Exchange(_ context.Context, code, state string, pkce PKCECodes) (OAuthCredential, error) {
	if f.exchangeErr != nil {
		return OAuthCredential{}, f.exchangeErr
	}
	return OAuthCredential{
		AccessToken:  "access-for-" + code,
		RefreshToken: "refresh-for-" + code,
		ExpiresAt:    time.Now().Add(time.Hour),
	}, nil
}

// freeAddr returns a "127.0.0.1:port" address that is free at the moment
// it's returned (briefly bound, then released), for tests that need to
// hand a fixed callback address to a fakeLogin before the coordinator
// itself binds it.
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

// assertPortReleased fails t if addr cannot be rebound within a couple of
// seconds. A single net.Listen attempt taken immediately after a
// coordinator releases its callback listener can spuriously collide with
// an unrelated parallel subtest's own freeAddr() call landing on the exact
// same ephemeral port in that same instant -- this file runs many
// t.Parallel() subtests that all briefly bind-then-close 127.0.0.1:0 to
// reserve an address, and under CI's heavier parallelism that TOCTOU
// window collides often enough to fail a single-shot check. That's a
// shared-fixture race between subtests, not a real leak in the listener
// this assertion is actually trying to verify, so a short bounded retry
// tells the two apart without weakening the guarantee.
func assertPortReleased(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		l, err := net.Listen("tcp", addr)
		if err == nil {
			l.Close()
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("net.Listen(%q) error = %v, want the callback listener released", addr, lastErr)
}

// stateFromAuthorizeURL extracts the state fakeLogin.AuthorizeURL embedded,
// so a test can build a matching callback request.
func stateFromAuthorizeURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatalf("authorize URL %q carries no state", raw)
	}
	return state
}

func TestLoginCoordinator_Login_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{})
	_, err := coord.Login(context.Background(), "bogus", "label", func(string) {})
	if err == nil {
		t.Fatal("Login() error = nil, want error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("Login() error = %q, want it to name the provider", err.Error())
	}
}

func TestLoginCoordinator_Login_HappyPath(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	login := &fakeLogin{addr: freeAddr(t), path: "/callback"}
	coord := NewLoginCoordinator(registry, map[string]Login{"fake": login})
	coord.timeout = 10 * time.Second

	urlCh := make(chan string, 1)
	type loginResult struct {
		account Account
		err     error
	}
	doneCh := make(chan loginResult, 1)
	go func() {
		account, err := coord.Login(context.Background(), "fake", "my-label", func(u string) { urlCh <- u })
		doneCh <- loginResult{account, err}
	}()

	authorizeURL := <-urlCh
	state := stateFromAuthorizeURL(t, authorizeURL)

	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=test-code&state=%s", login.addr, url.QueryEscape(state)))
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read callback response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("callback response status = %d, want 200", resp.StatusCode)
	}
	// providerDisplayLabel falls back to the raw provider id ("fake" here,
	// since it's not a known real provider) -- confirms the label actually
	// reaches newCallbackServer, not just that some page renders.
	if !strings.Contains(string(body), "fake") {
		t.Errorf("callback success page = %q, want it to mention the provider", body)
	}

	res := <-doneCh
	if res.err != nil {
		t.Fatalf("Login() error = %v", res.err)
	}
	if res.account.Provider != "fake" || res.account.Label != "my-label" {
		t.Errorf("account = %+v, want provider=fake label=my-label", res.account)
	}
	// Account (the internal type) legitimately carries the full credential
	// -- it's only the wsapi layer's accountResult that must be
	// credential-free (see internal/wsapi/handlers.go's accountResultFrom).
	if res.account.OAuth == nil || res.account.OAuth.AccessToken != "access-for-test-code" {
		t.Errorf("account.OAuth = %+v, want access-for-test-code", res.account.OAuth)
	}

	stored, err := registry.Get(res.account.ID)
	if err != nil {
		t.Fatalf("registry.Get() error = %v", err)
	}
	if stored.OAuth == nil || stored.OAuth.RefreshToken != "refresh-for-test-code" {
		t.Errorf("stored account = %+v, want the exchanged refresh token persisted", stored)
	}
}

func TestLoginCoordinator_Login_StateMismatch(t *testing.T) {
	t.Parallel()

	login := &fakeLogin{addr: freeAddr(t), path: "/callback"}
	coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"fake": login})
	coord.timeout = 10 * time.Second

	urlCh := make(chan string, 1)
	doneCh := make(chan error, 1)
	go func() {
		_, err := coord.Login(context.Background(), "fake", "label", func(u string) { urlCh <- u })
		doneCh <- err
	}()

	<-urlCh // wait for the listener to be up before hitting it

	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=test-code&state=totally-wrong", login.addr))
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()

	err = <-doneCh
	if err == nil {
		t.Fatal("Login() error = nil, want error for a mismatched state")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("Login() error = %q, want it to mention a state mismatch", err.Error())
	}
}

func TestLoginCoordinator_Login_Timeout(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	login := &fakeLogin{addr: addr, path: "/callback"}
	coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"fake": login})
	coord.timeout = 100 * time.Millisecond

	_, err := coord.Login(context.Background(), "fake", "label", func(string) {})
	if err == nil {
		t.Fatal("Login() error = nil, want a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Login() error = %q, want it to mention a timeout", err.Error())
	}

	// The listener must be torn down: rebinding the same address should
	// now succeed.
	assertPortReleased(t, addr)
}

func TestLoginCoordinator_Login_ConcurrentSameProvider(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	login := &fakeLogin{addr: addr, path: "/callback"}
	coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"fake": login})
	coord.timeout = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	urlCh := make(chan string, 1)
	doneCh := make(chan error, 1)
	go func() {
		_, err := coord.Login(ctx, "fake", "first", func(u string) { urlCh <- u })
		doneCh <- err
	}()

	<-urlCh // the first login's listener is bound and blocking on its callback

	_, err := coord.Login(context.Background(), "fake", "second", func(string) {})
	if err == nil {
		t.Fatal("second Login() error = nil, want \"already in progress\"")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("second Login() error = %q, want it to say a login is already in progress", err.Error())
	}

	// Release the first login rather than leaking it past this test.
	cancel()
	if err := <-doneCh; err == nil {
		t.Fatal("first Login() error = nil, want a context-cancellation error")
	}

	// Only one listener was ever opened: the address is free again now
	// that both attempts have finished.
	assertPortReleased(t, addr)
}

// TestLoginCoordinator_Login_ContextCancelled confirms that cancelling the
// caller's context mid-flow (e.g. the wsapi connection closing) both
// unblocks Login and tears down its callback listener -- verified under
// `go test -race` like the rest of this file.
func TestLoginCoordinator_Login_ContextCancelled(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	login := &fakeLogin{addr: addr, path: "/callback"}
	coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"fake": login})
	coord.timeout = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())

	urlCh := make(chan string, 1)
	doneCh := make(chan error, 1)
	go func() {
		_, err := coord.Login(ctx, "fake", "label", func(u string) { urlCh <- u })
		doneCh <- err
	}()

	<-urlCh
	cancel()

	if err := <-doneCh; err == nil {
		t.Fatal("Login() error = nil, want a context-cancellation error")
	}

	assertPortReleased(t, addr)
}

func TestLoginCoordinator_Login_ExchangeError(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	login := &fakeLogin{addr: addr, path: "/callback", exchangeErr: fmt.Errorf("token endpoint rejected the code")}
	coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"fake": login})
	coord.timeout = 10 * time.Second

	urlCh := make(chan string, 1)
	doneCh := make(chan error, 1)
	go func() {
		_, err := coord.Login(context.Background(), "fake", "label", func(u string) { urlCh <- u })
		doneCh <- err
	}()

	authorizeURL := <-urlCh
	state := stateFromAuthorizeURL(t, authorizeURL)
	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=test-code&state=%s", addr, url.QueryEscape(state)))
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()

	err = <-doneCh
	if err == nil || !strings.Contains(err.Error(), "token endpoint rejected the code") {
		t.Errorf("Login() error = %v, want it to wrap the exchange error", err)
	}
}
