package accounts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
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
	// subject is the vendor account subject Exchange reports ("" = none).
	subject string
}

func (f *fakeLogin) CallbackAddr() string { return f.addr }
func (f *fakeLogin) CallbackPath() string { return f.path }

func (f *fakeLogin) AuthorizeURL(state string, pkce PKCECodes) string {
	return "https://example.invalid/authorize?state=" + url.QueryEscape(state) +
		"&challenge=" + url.QueryEscape(pkce.CodeChallenge)
}

func (f *fakeLogin) Exchange(_ context.Context, code, state string, pkce PKCECodes) (ExchangeResult, error) {
	if f.exchangeErr != nil {
		return ExchangeResult{}, f.exchangeErr
	}
	return ExchangeResult{
		Credential: OAuthCredential{
			AccessToken:  "access-for-" + code,
			RefreshToken: "refresh-for-" + code,
			ExpiresAt:    time.Now().Add(time.Hour),
		},
		Subject: f.subject,
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
	_, err := coord.Login(context.Background(), "bogus", "label", LoginOptions{}, func(string) {})
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
		account, err := coord.Login(context.Background(), "fake", "my-label", LoginOptions{}, func(u string) { urlCh <- u })
		doneCh <- loginResult{account.Account, err}
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
		_, err := coord.Login(context.Background(), "fake", "label", LoginOptions{}, func(u string) { urlCh <- u })
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

	_, err := coord.Login(context.Background(), "fake", "label", LoginOptions{}, func(string) {})
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
		_, err := coord.Login(ctx, "fake", "first", LoginOptions{}, func(u string) { urlCh <- u })
		doneCh <- err
	}()

	<-urlCh // the first login's listener is bound and blocking on its callback

	_, err := coord.Login(context.Background(), "fake", "second", LoginOptions{}, func(string) {})
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
		_, err := coord.Login(ctx, "fake", "label", LoginOptions{}, func(u string) { urlCh <- u })
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
		_, err := coord.Login(context.Background(), "fake", "label", LoginOptions{}, func(u string) { urlCh <- u })
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

// relogin drives one full fake login (authorize URL -> callback -> exchange)
// against coord, with the fake vendor reporting subject, and returns the
// result. A fresh callback address per call keeps sequential logins from
// racing each other for a just-released port.
func relogin(t *testing.T, coord *LoginCoordinator, login *fakeLogin, label string, opts LoginOptions, subject string) (LoginResult, error) {
	t.Helper()
	login.addr = freeAddr(t)
	login.subject = subject

	urlCh := make(chan string, 1)
	type outcome struct {
		res LoginResult
		err error
	}
	doneCh := make(chan outcome, 1)
	go func() {
		res, err := coord.Login(context.Background(), "fake", label, opts, func(u string) { urlCh <- u })
		doneCh <- outcome{res, err}
	}()

	select {
	case u := <-urlCh:
		state := stateFromAuthorizeURL(t, u)
		resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=code-%s&state=%s", login.addr, label, url.QueryEscape(state)))
		if err != nil {
			t.Fatalf("GET callback: %v", err)
		}
		resp.Body.Close()
	case o := <-doneCh:
		// Login failed before the authorize URL was issued.
		return o.res, o.err
	}
	o := <-doneCh
	return o.res, o.err
}

type captureNotifier struct {
	mu      sync.Mutex
	updated []store.Account
}

func (n *captureNotifier) NotifyAccountUpdated(a store.Account) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.updated = append(n.updated, a)
}
func (n *captureNotifier) NotifyAccountRemoved(int64) {}

func (n *captureNotifier) updates() []store.Account {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]store.Account(nil), n.updated...)
}

func newReloginCoordinator(t *testing.T) (*LoginCoordinator, *Registry, *fakeLogin) {
	t.Helper()
	registry := newTestRegistry(t)
	login := &fakeLogin{path: "/callback"}
	coord := NewLoginCoordinator(registry, map[string]Login{"fake": login})
	coord.timeout = 10 * time.Second
	return coord, registry, login
}

func TestLoginCoordinator_Relogin(t *testing.T) {
	t.Parallel()

	t.Run("relogin same identity replaces credential in place and keeps routing links", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		notifier := &captureNotifier{}
		registry.SetNotifier(notifier)
		var hooked []int64
		registry.SetCredentialReplacedHook(func(id int64) { hooked = append(hooked, id) })

		first, err := relogin(t, coord, login, "work", LoginOptions{}, "subject-1")
		if err != nil {
			t.Fatalf("first Login() error = %v", err)
		}
		if !first.Created || first.Adopted || !first.Account.HasIdentity {
			t.Fatalf("first result = %+v, want created with identity", first)
		}
		id := first.Account.ID

		// Rows that must survive a same-identity re-login: routing affinity,
		// quota snapshot, the passive cooldown (the same vendor account is
		// still quota-exhausted) and the models list.
		now := time.Now().UTC()
		if _, err := registry.SetModels(id, []string{"claude-*"}); err != nil {
			t.Fatalf("SetModels() error = %v", err)
		}
		decision, err := registry.store.CreateRoutingDecision(store.RoutingDecision{SessionKey: "s1", AccountID: id, Policy: "p", DecidedAt: now, ExpiresAt: now.Add(time.Hour)})
		if err != nil {
			t.Fatalf("CreateRoutingDecision() error = %v", err)
		}
		snap, err := registry.store.CreateQuotaSnapshot(store.QuotaSnapshot{AccountID: id, UsageData: `{}`, PolledAt: now, ExpiresAt: now.Add(time.Hour)})
		if err != nil {
			t.Fatalf("CreateQuotaSnapshot() error = %v", err)
		}
		if err := registry.store.SaveQuotaCooldown(store.QuotaCooldown{AccountID: id, Until: now.Add(time.Hour), Reason: "r", ObservedAt: now}); err != nil {
			t.Fatalf("SaveQuotaCooldown() error = %v", err)
		}

		// Same vendor account logs in again, asking for a different label.
		second, err := relogin(t, coord, login, "renamed-by-mistake", LoginOptions{}, "subject-1")
		if err != nil {
			t.Fatalf("second Login() error = %v", err)
		}
		if second.Created || second.Adopted || second.Account.ID != id {
			t.Fatalf("second result = %+v, want in-place replace of account %d", second, id)
		}
		if second.Account.Label != "work" {
			t.Errorf("label = %q, want the existing %q (requested label ignored)", second.Account.Label, "work")
		}
		if second.Account.OAuth == nil || second.Account.OAuth.AccessToken != "access-for-code-renamed-by-mistake" {
			t.Errorf("credential = %+v, want the new login's tokens", second.Account.OAuth)
		}
		if len(second.Account.Models) != 1 || second.Account.Models[0] != "claude-*" {
			t.Errorf("models = %v, want kept", second.Account.Models)
		}

		accts, _ := registry.List()
		if len(accts) != 1 {
			t.Fatalf("accounts = %d, want still 1", len(accts))
		}
		if got, err := registry.store.GetRoutingDecision(decision.ID); err != nil || got.AccountID != id {
			t.Errorf("routing decision = %+v, %v, want kept on account %d", got, err, id)
		}
		if got, err := registry.store.GetQuotaSnapshot(snap.ID); err != nil || got.AccountID != id {
			t.Errorf("quota snapshot = %+v, %v, want kept on account %d", got, err, id)
		}
		if cds, _ := registry.store.ListQuotaCooldowns(id); len(cds) != 1 {
			t.Errorf("cooldowns = %v, want the cooldown kept (same vendor account)", cds)
		}
		if len(hooked) != 0 {
			t.Errorf("credential-replaced hook calls = %v, want none (the passive cooldown stays)", hooked)
		}
		if ups := notifier.updates(); len(ups) != 2 || ups[1].ID != id {
			t.Errorf("account.updated notifications = %d, want the models change plus the re-login", len(ups))
		}
	})

	t.Run("relogin with accountId and different identity is identity_mismatch", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		work, err := relogin(t, coord, login, "work", LoginOptions{}, "subject-1")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		before, _ := registry.Get(work.Account.ID)

		_, err = relogin(t, coord, login, "other", LoginOptions{AccountID: work.Account.ID}, "subject-2")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != LoginCodeIdentityMismatch || !strings.HasPrefix(err.Error(), "identity_mismatch") {
			t.Fatalf("Login() error = %v, want identity_mismatch", err)
		}
		if strings.Contains(err.Error(), "subject-") {
			t.Errorf("error %q leaks the subject", err)
		}
		after, _ := registry.Get(work.Account.ID)
		if after.OAuth.AccessToken != before.OAuth.AccessToken {
			t.Error("a mismatched login changed the account's credential")
		}
		if accts, _ := registry.List(); len(accts) != 1 {
			t.Errorf("accounts = %d, want no new row", len(accts))
		}
	})

	t.Run("relogin adopt clears cooldowns because the row may be a different vendor account", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		var hooked []int64
		registry.SetCredentialReplacedHook(func(id int64) { hooked = append(hooked, id) })
		legacy, err := registry.AddOAuth("fake", "legacy", OAuthCredential{AccessToken: "old", RefreshToken: "old-r", ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}
		now := time.Now().UTC()
		if err := registry.store.SaveQuotaCooldown(store.QuotaCooldown{AccountID: legacy.ID, Until: now.Add(time.Hour), Reason: "r", ObservedAt: now}); err != nil {
			t.Fatalf("SaveQuotaCooldown() error = %v", err)
		}

		if _, err := relogin(t, coord, login, "x", LoginOptions{AccountID: legacy.ID}, "subject-1"); err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		if cds, _ := registry.store.ListQuotaCooldowns(legacy.ID); len(cds) != 0 {
			t.Errorf("cooldowns = %v, want cleared by the adopt", cds)
		}
		if len(hooked) != 1 || hooked[0] != legacy.ID {
			t.Errorf("credential-replaced hook calls = %v, want [%d]", hooked, legacy.ID)
		}
	})

	t.Run("relogin adopts identity for a legacy account", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		legacy, err := registry.AddOAuth("fake", "legacy", OAuthCredential{AccessToken: "old", RefreshToken: "old-r", ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}
		if got, _ := registry.Get(legacy.ID); got.HasIdentity {
			t.Fatal("legacy account already has an identity")
		}

		res, err := relogin(t, coord, login, "ignored", LoginOptions{AccountID: legacy.ID}, "subject-1")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		if res.Created || !res.Adopted || res.Account.ID != legacy.ID || !res.Account.HasIdentity || res.Account.Label != "legacy" {
			t.Fatalf("result = %+v, want adopt of account %d keeping its label", res, legacy.ID)
		}
		if res.Account.OAuth == nil || res.Account.OAuth.AccessToken != "access-for-code-ignored" {
			t.Errorf("credential = %+v, want the new login's tokens", res.Account.OAuth)
		}
		// The adopted identity now matches a plain login of the same account.
		again, err := relogin(t, coord, login, "again", LoginOptions{}, "subject-1")
		if err != nil || again.Created || again.Account.ID != legacy.ID {
			t.Fatalf("plain re-login after adopt = %+v, %v, want in-place on %d", again, err, legacy.ID)
		}
	})

	t.Run("relogin adopt is identity_in_use when another row owns it", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		owner, err := relogin(t, coord, login, "owner", LoginOptions{}, "subject-1")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		legacy, err := registry.AddOAuth("fake", "legacy", OAuthCredential{AccessToken: "old", RefreshToken: "old-r", ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}

		_, err = relogin(t, coord, login, "x", LoginOptions{AccountID: legacy.ID}, "subject-1")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != LoginCodeIdentityInUse || !strings.HasPrefix(err.Error(), "identity_in_use") {
			t.Fatalf("Login() error = %v, want identity_in_use", err)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("account %d", owner.Account.ID)) {
			t.Errorf("error %q does not name the owning row %d", err, owner.Account.ID)
		}
		got, _ := registry.Get(legacy.ID)
		if got.HasIdentity || got.OAuth.AccessToken != "old" {
			t.Errorf("legacy row changed despite identity_in_use: %+v", got)
		}
	})

	t.Run("relogin of an unknown or foreign account fails before the flow starts", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		_, err := relogin(t, coord, login, "x", LoginOptions{AccountID: 4242}, "subject-1")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != LoginCodeUnknownAccount {
			t.Fatalf("Login(unknown id) error = %v, want login_unknown_account", err)
		}
		other, err := registry.AddOAuth("anthropic", "a", OAuthCredential{AccessToken: "t", RefreshToken: "r"})
		if err != nil {
			t.Fatalf("AddOAuth() error = %v", err)
		}
		_, err = relogin(t, coord, login, "x", LoginOptions{AccountID: other.ID}, "subject-1")
		if !errors.As(err, &le) || le.Code != LoginCodeUnknownAccount {
			t.Fatalf("Login(foreign provider row) error = %v, want login_unknown_account", err)
		}
	})

	t.Run("relogin without a subject cannot overwrite an identity-bearing row", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		work, err := relogin(t, coord, login, "work", LoginOptions{}, "subject-1")
		if err != nil || !work.Account.HasIdentity {
			t.Fatalf("Login() = %+v, %v, want an account with identity", work, err)
		}
		before, _ := registry.Get(work.Account.ID)

		_, err = relogin(t, coord, login, "x", LoginOptions{AccountID: work.Account.ID}, "")
		var le *LoginError
		if !errors.As(err, &le) || le.Code != LoginCodeIdentityUnverified || !strings.HasPrefix(err.Error(), "identity_unverified") {
			t.Fatalf("subject-less re-login error = %v, want identity_unverified", err)
		}
		after, _ := registry.Get(work.Account.ID)
		if after.OAuth.AccessToken != before.OAuth.AccessToken || !after.HasIdentity {
			t.Errorf("an unverifiable login changed the identity-bearing row: %+v", after)
		}
	})

	t.Run("login without a subject creates and replaces without an identity", func(t *testing.T) {
		t.Parallel()

		coord, registry, login := newReloginCoordinator(t)
		first, err := relogin(t, coord, login, "a", LoginOptions{}, "")
		if err != nil || !first.Created || first.Account.HasIdentity {
			t.Fatalf("login without subject = %+v, %v, want created without identity", first, err)
		}
		// Two subject-less logins are two rows (nothing to match on).
		second, err := relogin(t, coord, login, "b", LoginOptions{}, "")
		if err != nil || !second.Created || second.Account.ID == first.Account.ID {
			t.Fatalf("second subject-less login = %+v, %v, want a new row", second, err)
		}
		// A row-level re-login without a subject replaces the credential only.
		third, err := relogin(t, coord, login, "c", LoginOptions{AccountID: first.Account.ID}, "")
		if err != nil || third.Created || third.Adopted || third.Account.ID != first.Account.ID || third.Account.HasIdentity {
			t.Fatalf("row re-login without subject = %+v, %v", third, err)
		}
		if accts, _ := registry.List(); len(accts) != 2 {
			t.Errorf("accounts = %d, want 2", len(accts))
		}
	})
}
