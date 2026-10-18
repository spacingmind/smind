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
)

// requireLoginCode fails t unless err is a *LoginError carrying code.
func requireLoginCode(t *testing.T, err error, code string) {
	t.Helper()
	var le *LoginError
	if !errors.As(err, &le) {
		t.Fatalf("error = %v, want a *LoginError with code %q", err, code)
	}
	if le.Code != code {
		t.Fatalf("error code = %q (%v), want %q", le.Code, err, code)
	}
	if !strings.HasPrefix(err.Error(), code+": ") {
		t.Fatalf("error message = %q, want it to start with %q", err.Error(), code+": ")
	}
}

type loginOutcome struct {
	res LoginResult
	err error
}

// startedLogin is a Login that has issued its authorize URL.
type startedLogin struct {
	loginID      string
	authorizeURL string
	state        string
	event        LoginEvent
	done         chan loginOutcome
	cancel       context.CancelFunc
}

// startFakeLogin runs coord.Login for provider "fake" against login on a
// fresh callback address and returns once the authorize URL is issued. The
// login is cancelled when the test ends.
func startFakeLogin(t *testing.T, coord *LoginCoordinator, login *fakeLogin, label string) startedLogin {
	t.Helper()
	login.addr = freeAddr(t)
	return launchFakeLogin(t, coord, label)
}

// launchFakeLogin is startFakeLogin on whatever address the fake login
// already carries.
func launchFakeLogin(t *testing.T, coord *LoginCoordinator, label string) startedLogin {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	issuedCh := make(chan LoginEvent, 1)
	done := make(chan loginOutcome, 1)
	go func() {
		res, err := coord.Login(ctx, "fake", label, LoginOptions{}, func(e LoginEvent) { issuedCh <- e })
		done <- loginOutcome{res, err}
	}()

	select {
	case i := <-issuedCh:
		return startedLogin{loginID: i.LoginID, authorizeURL: i.URL, state: stateFromAuthorizeURL(t, i.URL), event: i, done: done, cancel: cancel}
	case o := <-done:
		t.Fatalf("Login() ended before issuing an authorize URL: %v", o.err)
	case <-time.After(5 * time.Second):
		t.Fatal("Login() did not issue an authorize URL")
	}
	return startedLogin{}
}

func (s startedLogin) wait(t *testing.T) loginOutcome {
	t.Helper()
	select {
	case o := <-s.done:
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("Login() did not finish")
	}
	return loginOutcome{}
}

func newCompleteCoordinator(t *testing.T) (*LoginCoordinator, *Registry, *fakeLogin) {
	t.Helper()
	registry := newTestRegistry(t)
	login := &fakeLogin{path: "/callback"}
	coord := NewLoginCoordinator(registry, map[string]Login{"fake": login})
	coord.timeout = 10 * time.Second
	return coord, registry, login
}

// pasteURL is a vendor redirect as a user would paste it.
func pasteURL(code, state string) string {
	return "http://localhost:54545/callback?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
}

// getCallback plays the browser: it GETs the listener's callback address.
func getCallback(t *testing.T, addr, code, state string) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=%s&state=%s", addr, url.QueryEscape(code), url.QueryEscape(state)))
	if err != nil {
		t.Errorf("GET callback: %v", err)
		return
	}
	resp.Body.Close()
}

// getCallbackBody GETs the listener's callback address and returns the body,
// failing t unless the status is wantStatus.
func getCallbackBody(t *testing.T, addr, code, state string, wantStatus int) string {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=%s&state=%s", addr, url.QueryEscape(code), url.QueryEscape(state)))
	if err != nil {
		t.Errorf("GET callback: %v", err)
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Errorf("callback status = %d, want %d", resp.StatusCode, wantStatus)
	}
	return string(body)
}

// tryCallback is getCallback for a request that may legitimately lose a race:
// when the paste wins, Login closes the listener, so the GET can fail with a
// refused or reset connection. Transport errors are ignored.
func tryCallback(addr, code, state string) {
	resp, err := http.Get(fmt.Sprintf("http://%s/callback?code=%s&state=%s", addr, url.QueryEscape(code), url.QueryEscape(state)))
	if err == nil {
		resp.Body.Close()
	}
}

// waitResolved blocks until the login named id has been resolved (its
// outcome decided), so a test can observe "already completed" without racing
// the wake-up of the Login goroutine.
func waitResolved(t *testing.T, coord *LoginCoordinator, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		coord.mu.Lock()
		pl, ok := coord.pending[id]
		resolved := ok && pl.resolved
		coord.mu.Unlock()
		if resolved {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("login was never resolved")
}

func pendingCount(coord *LoginCoordinator) int {
	coord.mu.Lock()
	defer coord.mu.Unlock()
	return len(coord.pending)
}

func accountCount(t *testing.T, registry *Registry) int {
	t.Helper()
	list, err := registry.List()
	if err != nil {
		t.Fatalf("registry.List() error = %v", err)
	}
	return len(list)
}

func TestLoginCoordinator_Complete(t *testing.T) {
	t.Parallel()

	t.Run("oauthStart emits authorizeUrl with a loginId", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)

		first := startFakeLogin(t, coord, login, "a")
		if len(first.loginID) != 22 || strings.Trim(first.loginID, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			t.Errorf("loginId = %q, want 16 random bytes as 22 base64url characters", first.loginID)
		}
		if !strings.HasPrefix(first.authorizeURL, "https://example.invalid/authorize?state=") {
			t.Errorf("authorize URL = %q, want the login's authorize URL", first.authorizeURL)
		}
		if pendingCount(coord) != 1 {
			t.Errorf("pending logins = %d, want 1 while the login waits", pendingCount(coord))
		}
		if first.event.Kind != LoginEventAuthorizeURL || first.event.PasteOnly {
			t.Errorf("event = %+v, want a plain authorizeUrl event (not paste-only)", first.event)
		}
		first.cancel()
		first.wait(t)

		second := startFakeLogin(t, coord, login, "b")
		if second.loginID == first.loginID {
			t.Errorf("two logins share loginId %q", first.loginID)
		}
	})

	t.Run("oauthComplete finishes the login from a pasted redirect", func(t *testing.T) {
		t.Parallel()
		coord, registry, login := newCompleteCoordinator(t)
		s := startFakeLogin(t, coord, login, "pasted")

		if err := coord.Complete(s.loginID, pasteURL("pasted-code", s.state)); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		o := s.wait(t)
		if o.err != nil {
			t.Fatalf("Login() error = %v", o.err)
		}
		if !o.res.Created || o.res.Account.Label != "pasted" || o.res.Account.OAuth == nil || o.res.Account.OAuth.AccessToken != "access-for-pasted-code" {
			t.Errorf("Login() result = %+v, want a created account exchanged from the pasted code", o.res)
		}
		stored, err := registry.Get(o.res.Account.ID)
		if err != nil || stored.OAuth == nil || stored.OAuth.RefreshToken != "refresh-for-pasted-code" {
			t.Errorf("stored account = %+v, %v, want the pasted code's credential persisted", stored, err)
		}
		// The login is over: its id is gone and its listener released.
		requireLoginCode(t, coord.Complete(s.loginID, pasteURL("pasted-code", s.state)), LoginCodeUnknown)
		assertPortReleased(t, login.addr)
	})

	t.Run("oauthComplete accepts bare query and code-hash forms (end to end)", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)

		forms := []struct {
			name string
			form func(code, state string) string
		}{
			{"bare query", func(c, s string) string { return "code=" + c + "&state=" + s }},
			{"leading question mark", func(c, s string) string { return "?code=" + c + "&state=" + s }},
			{"code hash", func(c, s string) string { return c + "#" + s }},
			{"fragment", func(c, s string) string { return "http://localhost:1455/auth/callback#code=" + c + "&state=" + s }},
			{"padded url", func(c, s string) string { return "  " + pasteURL(c, s) + "\n" }},
		}
		for i, f := range forms {
			code := fmt.Sprintf("form%d", i)
			s := startFakeLogin(t, coord, login, f.name)
			if err := coord.Complete(s.loginID, f.form(code, s.state)); err != nil {
				t.Fatalf("%s: Complete() error = %v", f.name, err)
			}
			o := s.wait(t)
			if o.err != nil || o.res.Account.OAuth == nil || o.res.Account.OAuth.AccessToken != "access-for-"+code {
				t.Fatalf("%s: Login() = %+v, %v, want the exchange of %q", f.name, o.res, o.err, code)
			}
		}
	})

	t.Run("oauthComplete wrong state errors and leaves login pending", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)
		s := startFakeLogin(t, coord, login, "w")

		err := coord.Complete(s.loginID, pasteURL("the-code", "someone-elses-state"))
		requireLoginCode(t, err, LoginCodeStateMismatch)
		if strings.Contains(err.Error(), "the-code") || strings.Contains(err.Error(), "someone-elses-state") {
			t.Errorf("error %q echoes the pasted code or state", err.Error())
		}
		select {
		case o := <-s.done:
			t.Fatalf("Login() ended after a wrong-state paste: %+v", o)
		case <-time.After(100 * time.Millisecond):
		}
		if pendingCount(coord) != 1 {
			t.Fatalf("pending logins = %d, want the login still pending", pendingCount(coord))
		}

		// The right paste still works afterwards.
		if err := coord.Complete(s.loginID, pasteURL("good-code", s.state)); err != nil {
			t.Fatalf("Complete() after a wrong state error = %v", err)
		}
		if o := s.wait(t); o.err != nil {
			t.Fatalf("Login() error = %v", o.err)
		}
	})

	t.Run("busy callback port falls back to paste only", func(t *testing.T) {
		t.Parallel()
		coord, registry, login := newCompleteCoordinator(t)

		// Someone else owns the vendor's callback port.
		occupied, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("net.Listen() error = %v", err)
		}
		defer occupied.Close()
		login.addr = occupied.Addr().String()

		s := launchFakeLogin(t, coord, "busy")
		if !s.event.PasteOnly || s.event.Kind != LoginEventAuthorizeURL || s.event.URL == "" {
			t.Fatalf("event = %+v, want an authorizeUrl event with PasteOnly", s.event)
		}
		if err := coord.Complete(s.loginID, pasteURL("pasted", s.state)); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		o := s.wait(t)
		if o.err != nil || !o.res.Created || o.res.Account.OAuth == nil || o.res.Account.OAuth.AccessToken != "access-for-pasted" {
			t.Fatalf("Login() = %+v, %v, want an account created from the paste", o.res, o.err)
		}
		if n := accountCount(t, registry); n != 1 {
			t.Fatalf("accounts = %d, want 1", n)
		}

		// The other process's listener is untouched by the login's teardown.
		conn, err := net.Dial("tcp", login.addr)
		if err != nil {
			t.Fatalf("dial the occupied port: %v", err)
		}
		conn.Close()
	})

	t.Run("a bind error other than address in use still fails the login", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)

		for _, addr := range []string{"127.0.0.1:99999", "0.0.0.0:0", "192.0.2.1:1455"} {
			login.addr = addr
			_, err := coord.Login(context.Background(), "fake", "bad", LoginOptions{}, func(LoginEvent) {
				t.Errorf("a login on %s issued an event", addr)
			})
			if err == nil {
				t.Errorf("Login(%s) error = nil, want the bind/loopback failure", addr)
			}
			var le *LoginError
			if errors.As(err, &le) {
				t.Errorf("Login(%s) error = %v, want a plain error rather than a login code", addr, err)
			}
		}
		if pendingCount(coord) != 0 {
			t.Errorf("pending logins = %d, want none", pendingCount(coord))
		}
	})

	t.Run("junk callback does not end the login", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)
		s := startFakeLogin(t, coord, login, "junk")

		junk := []string{
			"/callback",
			"/callback?code=stolen",
			"/callback?code=stolen&state=wrong",
			"/callback?error=access_denied",
			"/callback?error=access_denied&state=wrong",
			"/callback?code=stolen&state=",
			"/favicon.ico",
			"/",
		}
		for _, path := range junk {
			resp, err := http.Get("http://" + login.addr + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			resp.Body.Close()
			if resp.StatusCode < 400 {
				t.Errorf("GET %s status = %d, want an error status", path, resp.StatusCode)
			}
		}
		select {
		case o := <-s.done:
			t.Fatalf("Login() ended after junk requests: %+v", o)
		case <-time.After(100 * time.Millisecond):
		}
		if pendingCount(coord) != 1 {
			t.Fatalf("pending logins = %d, want the login still pending", pendingCount(coord))
		}

		// A matching callback still completes it.
		getCallback(t, login.addr, "real-code", s.state)
		o := s.wait(t)
		if o.err != nil || o.res.Account.OAuth == nil || o.res.Account.OAuth.AccessToken != "access-for-real-code" {
			t.Fatalf("Login() = %+v, %v, want the matching callback's code", o.res, o.err)
		}
	})

	t.Run("callback with matching state still completes the login", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)

		s := startFakeLogin(t, coord, login, "ok")
		getCallback(t, login.addr, "good-code", s.state)
		if o := s.wait(t); o.err != nil || o.res.Account.OAuth.AccessToken != "access-for-good-code" {
			t.Fatalf("Login() = %+v, %v, want the callback's code", o.res, o.err)
		}

		// The error= form with the matching state is delivered and denies.
		s = startFakeLogin(t, coord, login, "denied")
		resp, err := http.Get("http://" + login.addr + "/callback?error=access_denied&state=" + url.QueryEscape(s.state))
		if err != nil {
			t.Fatalf("GET callback: %v", err)
		}
		resp.Body.Close()
		requireLoginCode(t, s.wait(t).err, LoginCodeDenied)
	})

	t.Run("oauthComplete unknown loginId errors", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)

		requireLoginCode(t, coord.Complete("nope", pasteURL("c", "s")), LoginCodeUnknown)

		// A real login's id works only while that login is pending.
		s := startFakeLogin(t, coord, login, "u")
		requireLoginCode(t, coord.Complete(s.loginID+"x", pasteURL("c", s.state)), LoginCodeUnknown)
		s.cancel()
		s.wait(t)
		requireLoginCode(t, coord.Complete(s.loginID, pasteURL("c", s.state)), LoginCodeUnknown)
	})

	t.Run("oauthComplete wrong method errors", func(t *testing.T) {
		t.Parallel()
		coord, _, _ := newCompleteCoordinator(t)
		coord.register("dev", &pendingLogin{provider: "fake", state: "s", method: "device", paste: make(chan callbackResult, 1)})
		requireLoginCode(t, coord.Complete("dev", pasteURL("c", "s")), LoginCodeWrongMethod)
	})

	t.Run("oauthComplete pasted error fails the login as denied", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)
		s := startFakeLogin(t, coord, login, "d")

		if err := coord.Complete(s.loginID, "http://localhost:54545/callback?error=access_denied&state="+s.state); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		o := s.wait(t)
		requireLoginCode(t, o.err, LoginCodeDenied)
		if !strings.Contains(o.err.Error(), "access_denied") {
			t.Errorf("error %q, want the vendor's error code", o.err.Error())
		}
	})

	t.Run("oauthComplete error paste without matching state does not fail the login", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)
		s := startFakeLogin(t, coord, login, "e")

		requireLoginCode(t, coord.Complete(s.loginID, "http://localhost:54545/callback?error=access_denied"), LoginCodeStateMismatch)
		requireLoginCode(t, coord.Complete(s.loginID, "http://localhost:54545/callback?error=access_denied&state=wrong"), LoginCodeStateMismatch)
		select {
		case o := <-s.done:
			t.Fatalf("Login() ended after an unproven error paste: %+v", o)
		case <-time.After(100 * time.Millisecond):
		}
		if pendingCount(coord) != 1 {
			t.Fatalf("pending logins = %d, want the login still pending", pendingCount(coord))
		}

		// With the right state the error does end the login.
		if err := coord.Complete(s.loginID, "error=access_denied&state="+s.state); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		requireLoginCode(t, s.wait(t).err, LoginCodeDenied)
	})

	t.Run("oauthComplete rejects after five attempts", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)
		s := startFakeLogin(t, coord, login, "five")

		// Malformed pastes and wrong states both count.
		for i := 0; i < 3; i++ {
			requireLoginCode(t, coord.Complete(s.loginID, "garbage"), LoginCodeBadRedirect)
		}
		for i := 0; i < 2; i++ {
			requireLoginCode(t, coord.Complete(s.loginID, pasteURL("c", "wrong")), LoginCodeStateMismatch)
		}
		// The sixth is refused even though it is correct, and the login
		// stays pending (it just stops listening to pastes).
		requireLoginCode(t, coord.Complete(s.loginID, pasteURL("c", s.state)), LoginCodeAttemptsExceeded)
		select {
		case o := <-s.done:
			t.Fatalf("Login() ended after the attempt limit: %+v", o)
		case <-time.After(100 * time.Millisecond):
		}
		if pendingCount(coord) != 1 {
			t.Fatalf("pending logins = %d, want the login still pending", pendingCount(coord))
		}

		// The listener still finishes it.
		getCallback(t, login.addr, "via-listener", s.state)
		o := s.wait(t)
		if o.err != nil || o.res.Account.OAuth.AccessToken != "access-for-via-listener" {
			t.Fatalf("Login() = %+v, %v, want the listener to finish the login", o.res, o.err)
		}
	})

	t.Run("first of listener callback and paste wins", func(t *testing.T) {
		t.Parallel()

		t.Run("listener then paste", func(t *testing.T) {
			t.Parallel()
			coord, registry, login := newCompleteCoordinator(t)
			login.gate = make(chan struct{})
			s := startFakeLogin(t, coord, login, "lp")

			getCallback(t, login.addr, "from-listener", s.state)
			waitResolved(t, coord, s.loginID)
			// Login is blocked in Exchange: the id is still registered, but
			// the outcome is already decided.
			requireLoginCode(t, coord.Complete(s.loginID, pasteURL("from-paste", s.state)), LoginCodeAlreadyCompleted)

			close(login.gate)
			o := s.wait(t)
			if o.err != nil || o.res.Account.OAuth.AccessToken != "access-for-from-listener" {
				t.Fatalf("Login() = %+v, %v, want the listener's code", o.res, o.err)
			}
			if n := accountCount(t, registry); n != 1 {
				t.Fatalf("accounts = %d, want exactly one", n)
			}
		})

		t.Run("paste then listener", func(t *testing.T) {
			t.Parallel()
			coord, registry, login := newCompleteCoordinator(t)
			login.gate = make(chan struct{})
			s := startFakeLogin(t, coord, login, "pl")

			if err := coord.Complete(s.loginID, pasteURL("from-paste", s.state)); err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			getCallback(t, login.addr, "from-listener", s.state)
			requireLoginCode(t, coord.Complete(s.loginID, pasteURL("second-paste", s.state)), LoginCodeAlreadyCompleted)

			close(login.gate)
			o := s.wait(t)
			if o.err != nil || o.res.Account.OAuth.AccessToken != "access-for-from-paste" {
				t.Fatalf("Login() = %+v, %v, want the paste's code", o.res, o.err)
			}
			if n := accountCount(t, registry); n != 1 {
				t.Fatalf("accounts = %d, want exactly one", n)
			}
		})

		t.Run("simultaneous", func(t *testing.T) {
			t.Parallel()
			for i := 0; i < 5; i++ {
				coord, registry, login := newCompleteCoordinator(t)
				s := startFakeLogin(t, coord, login, "sim")

				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					tryCallback(login.addr, "from-listener", s.state)
				}()
				go func() {
					defer wg.Done()
					_ = coord.Complete(s.loginID, pasteURL("from-paste", s.state))
				}()
				wg.Wait()

				o := s.wait(t)
				token := ""
				if o.err == nil {
					token = o.res.Account.OAuth.AccessToken
				}
				if token != "access-for-from-listener" && token != "access-for-from-paste" {
					t.Fatalf("Login() = %+v, %v, want one of the two codes", o.res, o.err)
				}
				if n := accountCount(t, registry); n != 1 {
					t.Fatalf("accounts = %d, want exactly one", n)
				}
			}
		})
	})

	t.Run("a paste acknowledged by oauthComplete is never dropped", func(t *testing.T) {
		t.Parallel()

		// The paste is acknowledged (Complete returned nil) before await
		// observes any other event; await's select then sees the paste and a
		// competing event ready together and picks at random. Whichever it
		// picks, the acknowledged paste must be the delivered result -- the
		// re-check of pl.resolved under the mutex is what guarantees it. Many
		// iterations make both select branches near-certain to be exercised.
		cases := map[string]func(t *testing.T, c *LoginCoordinator, srv *callbackServer) context.Context{
			"listener result": func(t *testing.T, c *LoginCoordinator, srv *callbackServer) context.Context {
				srv.result <- callbackResult{code: "from-listener", state: "st"}
				return context.Background()
			},
			"ctx cancel": func(t *testing.T, c *LoginCoordinator, srv *callbackServer) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			"timeout": func(t *testing.T, c *LoginCoordinator, srv *callbackServer) context.Context {
				c.timeout = time.Nanosecond
				return context.Background()
			},
		}
		for name, competing := range cases {
			coord, _, _ := newCompleteCoordinator(t)
			for i := 0; i < 200; i++ {
				pl := &pendingLogin{provider: "fake", state: "st", method: loginMethodBrowser, paste: make(chan callbackResult, 1)}
				coord.register("id", pl)
				srv := &callbackServer{result: make(chan callbackResult, 1)}

				if err := coord.Complete("id", pasteURL("from-paste", "st")); err != nil {
					t.Fatalf("%s: Complete() error = %v", name, err)
				}
				ctx := competing(t, coord, srv)
				res, err := coord.await(ctx, "id", srv, pl)
				if err != nil || res.code != "from-paste" {
					t.Fatalf("%s (iteration %d): await() = %+v, %v, want the acknowledged paste", name, i, res, err)
				}
			}
		}
	})

	t.Run("oauthComplete after cancel is login_unknown", func(t *testing.T) {
		t.Parallel()

		// await ends the wait but, unlike Login, does not unregister: this
		// is the window between the wait ending and Login returning, where a
		// paste must not be told "already completed" about a login that
		// never completed.
		for name, end := range map[string]func(c *LoginCoordinator) context.Context{
			"ctx cancel": func(c *LoginCoordinator) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			"timeout": func(c *LoginCoordinator) context.Context {
				c.timeout = time.Millisecond
				return context.Background()
			},
		} {
			coord, _, _ := newCompleteCoordinator(t)
			pl := &pendingLogin{provider: "fake", state: "st", method: loginMethodBrowser, paste: make(chan callbackResult, 1)}
			coord.register("id", pl)
			ctx := end(coord)

			_, err := coord.await(ctx, "id", &callbackServer{result: make(chan callbackResult, 1)}, pl)
			if err == nil {
				t.Fatalf("%s: await() error = nil, want the cancel/timeout error", name)
			}
			requireLoginCode(t, coord.Complete("id", pasteURL("c", "st")), LoginCodeUnknown)
		}
	})

	t.Run("cancel frees listener and provider claim", func(t *testing.T) {
		t.Parallel()
		coord, _, login := newCompleteCoordinator(t)
		s := startFakeLogin(t, coord, login, "c")
		addr := login.addr

		s.cancel()
		o := s.wait(t)
		if !errors.Is(o.err, context.Canceled) {
			t.Fatalf("Login() error = %v, want context.Canceled", o.err)
		}
		requireLoginCode(t, coord.Complete(s.loginID, pasteURL("c", s.state)), LoginCodeUnknown)
		assertPortReleased(t, addr)

		// The provider claim is free again: a second login starts.
		again := startFakeLogin(t, coord, login, "c2")
		if again.loginID == s.loginID {
			t.Errorf("a new login reused loginId %q", s.loginID)
		}
	})
}
