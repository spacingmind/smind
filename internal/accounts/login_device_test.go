package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDeviceLogin is a Login that also implements DeviceLogin, for coordinator
// tests that need no vendor on the wire. pollFn decides each poll's answer
// (n is 1-based); the zero value polls pending forever.
type fakeDeviceLogin struct {
	fakeLogin

	startErr  error
	challenge DeviceChallenge
	pollFn    func(n int) (DeviceGrant, error)
	exchErr   error

	mu       sync.Mutex
	started  int
	polls    int
	exchange []DeviceGrant
}

func (f *fakeDeviceLogin) StartDevice(context.Context) (DeviceChallenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
	if f.startErr != nil {
		return DeviceChallenge{}, f.startErr
	}
	ch := f.challenge
	if ch.UserCode == "" {
		ch = DeviceChallenge{DeviceAuthID: "dev-auth-id", UserCode: "ABCD-1234", VerificationURL: "https://example.invalid/device", Interval: 2 * time.Millisecond}
	}
	return ch, nil
}

func (f *fakeDeviceLogin) PollDevice(ctx context.Context, _ DeviceChallenge) (DeviceGrant, error) {
	if err := ctx.Err(); err != nil {
		return DeviceGrant{}, err
	}
	f.mu.Lock()
	f.polls++
	n := f.polls
	fn := f.pollFn
	f.mu.Unlock()
	if fn == nil {
		return DeviceGrant{}, ErrDevicePending
	}
	return fn(n)
}

func (f *fakeDeviceLogin) ExchangeDevice(_ context.Context, g DeviceGrant) (ExchangeResult, error) {
	f.mu.Lock()
	f.exchange = append(f.exchange, g)
	f.mu.Unlock()
	if f.exchErr != nil {
		return ExchangeResult{}, f.exchErr
	}
	return ExchangeResult{
		Credential: OAuthCredential{
			AccessToken:  "access-for-" + g.AuthorizationCode,
			RefreshToken: "refresh-for-" + g.CodeVerifier,
			ExpiresAt:    time.Now().Add(time.Hour),
		},
		Subject: f.subject,
	}, nil
}

func (f *fakeDeviceLogin) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

func newDeviceCoordinator(t *testing.T) (*LoginCoordinator, *Registry, *fakeDeviceLogin) {
	t.Helper()
	registry := newTestRegistry(t)
	dev := &fakeDeviceLogin{fakeLogin: fakeLogin{path: "/callback"}}
	coord := NewLoginCoordinator(registry, map[string]Login{"fake": dev})
	coord.timeout = 10 * time.Second
	coord.minPoll = time.Millisecond
	return coord, registry, dev
}

// startedDevice is a device login that has issued its deviceCode event.
type startedDevice struct {
	event  LoginEvent
	done   chan loginOutcome
	cancel context.CancelFunc
}

func startDeviceLogin(t *testing.T, coord *LoginCoordinator, provider, label string, opts LoginOptions) startedDevice {
	t.Helper()
	opts.Method = LoginMethodDevice
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := make(chan LoginEvent, 4)
	done := make(chan loginOutcome, 1)
	go func() {
		res, err := coord.Login(ctx, provider, label, opts, func(e LoginEvent) { events <- e })
		done <- loginOutcome{res, err}
	}()
	select {
	case e := <-events:
		return startedDevice{event: e, done: done, cancel: cancel}
	case o := <-done:
		t.Fatalf("Login() ended before issuing a deviceCode event: %v", o.err)
	case <-time.After(5 * time.Second):
		t.Fatal("Login() did not issue a deviceCode event")
	}
	return startedDevice{}
}

func (s startedDevice) wait(t *testing.T) loginOutcome {
	t.Helper()
	select {
	case o := <-s.done:
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("Login() did not finish")
	}
	return loginOutcome{}
}

func TestLoginCoordinator_Device(t *testing.T) {
	t.Parallel()

	t.Run("device login polls until approved then stores account", func(t *testing.T) {
		t.Parallel()
		coord, registry, dev := newDeviceCoordinator(t)
		dev.subject = "sub-1"
		dev.pollFn = func(n int) (DeviceGrant, error) {
			if n <= 3 {
				return DeviceGrant{}, ErrDevicePending
			}
			return DeviceGrant{AuthorizationCode: "authz", CodeVerifier: "vendor-verifier"}, nil
		}

		s := startDeviceLogin(t, coord, "fake", "dev", LoginOptions{})
		e := s.event
		if e.Kind != LoginEventDeviceCode || e.UserCode != "ABCD-1234" || e.VerificationURL != "https://example.invalid/device" || len(e.LoginID) != 22 {
			t.Fatalf("event = %+v, want a deviceCode event with the code, URL and a loginId", e)
		}
		if e.URL != "" || e.PasteOnly {
			t.Errorf("event = %+v, want no authorize URL fields on a device event", e)
		}
		if remaining := time.Until(e.ExpiresAt); remaining <= 0 || remaining > deviceLoginTimeout {
			t.Errorf("ExpiresAt in %v, want within the %v device cap", remaining, deviceLoginTimeout)
		}
		if e.IntervalSeconds < 1 {
			t.Errorf("IntervalSeconds = %d, want at least 1", e.IntervalSeconds)
		}
		// A device login takes no pasted redirect, and binds no listener.
		requireLoginCode(t, coord.Complete(e.LoginID, pasteURL("c", "s")), LoginCodeWrongMethod)

		o := s.wait(t)
		if o.err != nil {
			t.Fatalf("Login() error = %v", o.err)
		}
		if !o.res.Created || !o.res.Account.HasIdentity {
			t.Errorf("result = %+v, want a created account with an identity", o.res)
		}
		stored, err := registry.Get(o.res.Account.ID)
		if err != nil || stored.OAuth == nil || stored.OAuth.AccessToken != "access-for-authz" || stored.OAuth.RefreshToken != "refresh-for-vendor-verifier" {
			t.Fatalf("stored = %+v, %v, want the exchanged credential persisted", stored, err)
		}
		if len(dev.exchange) != 1 || dev.exchange[0].CodeVerifier != "vendor-verifier" {
			t.Errorf("exchanged grants = %+v, want the vendor's grant exactly once", dev.exchange)
		}
		if pendingCount(coord) != 0 {
			t.Errorf("pending logins = %d, want none after the login", pendingCount(coord))
		}
		requireLoginCode(t, coord.Complete(e.LoginID, pasteURL("c", "s")), LoginCodeUnknown)

		// A second device login for the same vendor account replaces in place.
		// (The fake keeps approving: its poll count is already past 3.)
		s2 := startDeviceLogin(t, coord, "fake", "again", LoginOptions{})
		o2 := s2.wait(t)
		if o2.err != nil || o2.res.Created || o2.res.Account.ID != o.res.Account.ID {
			t.Fatalf("second Login() = %+v, %v, want the same account updated in place", o2.res, o2.err)
		}
		if n := accountCount(t, registry); n != 1 {
			t.Errorf("accounts = %d, want 1", n)
		}
	})

	t.Run("device login stops at expiry and on cancel", func(t *testing.T) {
		t.Parallel()
		coord, registry, dev := newDeviceCoordinator(t)

		// Expiry: never approved.
		coord.deviceTimeout = 60 * time.Millisecond
		s := startDeviceLogin(t, coord, "fake", "expiring", LoginOptions{})
		o := s.wait(t)
		requireLoginCode(t, o.err, LoginCodeDeviceExpired)
		if dev.pollCount() == 0 {
			t.Error("the login expired without polling")
		}
		if pendingCount(coord) != 0 || accountCount(t, registry) != 0 {
			t.Errorf("pending = %d, accounts = %d, want neither after expiry", pendingCount(coord), accountCount(t, registry))
		}

		// The poll stops with the login.
		polls := dev.pollCount()
		time.Sleep(30 * time.Millisecond)
		if dev.pollCount() != polls {
			t.Errorf("polls went %d -> %d after the login ended, want the poller stopped", polls, dev.pollCount())
		}

		// Cancel: the claim and the loginId are freed.
		coord.deviceTimeout = time.Minute
		c := startDeviceLogin(t, coord, "fake", "cancelled", LoginOptions{})
		c.cancel()
		co := c.wait(t)
		if !errors.Is(co.err, context.Canceled) {
			t.Fatalf("Login() error = %v, want context.Canceled", co.err)
		}
		requireLoginCode(t, coord.Complete(c.event.LoginID, pasteURL("c", "s")), LoginCodeUnknown)

		again := startDeviceLogin(t, coord, "fake", "second", LoginOptions{})
		if again.event.LoginID == c.event.LoginID {
			t.Errorf("a new login reused loginId %q", c.event.LoginID)
		}
		again.cancel()
		again.wait(t)
	})

	t.Run("device login 404 is device_unsupported", func(t *testing.T) {
		t.Parallel()
		coord, _, dev := newDeviceCoordinator(t)
		dev.startErr = ErrDeviceUnsupported

		_, err := coord.Login(context.Background(), "fake", "x", LoginOptions{Method: LoginMethodDevice}, func(e LoginEvent) {
			t.Errorf("event %+v emitted for an unsupported device login", e)
		})
		requireLoginCode(t, err, LoginCodeDeviceUnsupported)
		if pendingCount(coord) != 0 {
			t.Errorf("pending logins = %d, want none", pendingCount(coord))
		}

		// The claim was released: another login starts.
		dev.startErr = nil
		s := startDeviceLogin(t, coord, "fake", "y", LoginOptions{})
		s.cancel()
		s.wait(t)
	})

	t.Run("device method is rejected for anthropic", func(t *testing.T) {
		t.Parallel()
		// A provider whose login has no device flow (anthropic's, here a plain
		// fakeLogin and the real AnthropicLogin alike) never gets as far as a
		// claim, a listener or an event.
		for name, login := range map[string]Login{
			"plain fake": &fakeLogin{addr: freeAddr(t), path: "/callback"},
			"real":       NewAnthropicLogin(),
		} {
			registry := newTestRegistry(t)
			coord := NewLoginCoordinator(registry, map[string]Login{"anthropic": login})
			_, err := coord.Login(context.Background(), "anthropic", "x", LoginOptions{Method: LoginMethodDevice}, func(e LoginEvent) {
				t.Errorf("%s: event %+v emitted for a rejected device login", name, e)
			})
			requireLoginCode(t, err, LoginCodeDeviceUnsupported)
			if !coord.claim("anthropic") {
				t.Errorf("%s: the provider claim is still held after the rejection", name)
			}
		}
		if _, ok := Login(NewAnthropicLogin()).(DeviceLogin); ok {
			t.Error("AnthropicLogin implements DeviceLogin; ADR-0033 has no Claude device flow")
		}
	})

	t.Run("unknown method is login_bad_method", func(t *testing.T) {
		t.Parallel()
		coord, _, _ := newDeviceCoordinator(t)
		_, err := coord.Login(context.Background(), "fake", "x", LoginOptions{Method: "carrier-pigeon"}, func(LoginEvent) {})
		requireLoginCode(t, err, LoginCodeBadMethod)
		if strings.Contains(err.Error(), "carrier-pigeon") {
			t.Errorf("error %q echoes the requested method", err.Error())
		}
	})

	t.Run("device login one login per provider", func(t *testing.T) {
		t.Parallel()
		coord, _, _ := newDeviceCoordinator(t)
		s := startDeviceLogin(t, coord, "fake", "first", LoginOptions{})
		_, err := coord.Login(context.Background(), "fake", "second", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		requireLoginCode(t, err, LoginCodeInProgress)
		s.cancel()
		s.wait(t)
	})

	t.Run("device login retries a poll that times out", func(t *testing.T) {
		t.Parallel()
		// A DeviceLogin that reports the per-poll deadline as the raw context
		// error (as a hung HTTP request does) once, then approves.
		coord, _, dev := newDeviceCoordinator(t)
		dev.pollFn = func(n int) (DeviceGrant, error) {
			if n == 1 {
				return DeviceGrant{}, context.DeadlineExceeded
			}
			return DeviceGrant{AuthorizationCode: "c", CodeVerifier: "v"}, nil
		}
		res, err := coord.Login(context.Background(), "fake", "slow", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		if err != nil || !res.Created {
			t.Fatalf("Login() = %+v, %v, want the timed-out poll retried", res, err)
		}

		// Four in a row still ends the login (the same cap as other transient
		// errors), and a deadline of the caller's own ctx is not retried.
		coord, _, dev = newDeviceCoordinator(t)
		dev.pollFn = func(int) (DeviceGrant, error) { return DeviceGrant{}, context.DeadlineExceeded }
		_, err = coord.Login(context.Background(), "fake", "slow", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		requireLoginCode(t, err, LoginCodeDeviceFailed)
		if dev.pollCount() != maxDeviceRetries+1 {
			t.Errorf("polls = %d, want %d", dev.pollCount(), maxDeviceRetries+1)
		}

		coord, _, dev = newDeviceCoordinator(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err = coord.Login(ctx, "fake", "slow", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Login() with an expiring caller ctx error = %v, want that deadline", err)
		}
	})

	t.Run("device login failures", func(t *testing.T) {
		t.Parallel()
		approve := func(int) (DeviceGrant, error) { return DeviceGrant{AuthorizationCode: "c", CodeVerifier: "v"}, nil }
		transient := fmt.Errorf("poll: %w", ErrDeviceTransient)

		cases := []struct {
			name     string
			setup    func(d *fakeDeviceLogin)
			wantCode string // "" = success
		}{
			{"start error", func(d *fakeDeviceLogin) { d.startErr = errors.New("boom") }, LoginCodeDeviceFailed},
			{"poll error that is not pending", func(d *fakeDeviceLogin) {
				d.pollFn = func(int) (DeviceGrant, error) { return DeviceGrant{}, errors.New("status 500") }
			}, LoginCodeDeviceFailed},
			{"three transient errors are retried", func(d *fakeDeviceLogin) {
				d.pollFn = func(n int) (DeviceGrant, error) {
					if n <= maxDeviceRetries {
						return DeviceGrant{}, transient
					}
					return approve(n)
				}
			}, ""},
			{"a pending answer resets the transient count", func(d *fakeDeviceLogin) {
				d.pollFn = func(n int) (DeviceGrant, error) {
					switch {
					case n == 4 || n == 8:
						return DeviceGrant{}, ErrDevicePending
					case n <= 11:
						return DeviceGrant{}, transient
					}
					return approve(n)
				}
			}, ""},
			{"too many transient errors in a row", func(d *fakeDeviceLogin) {
				d.pollFn = func(int) (DeviceGrant, error) { return DeviceGrant{}, transient }
			}, LoginCodeDeviceFailed},
			{"exchange error", func(d *fakeDeviceLogin) { d.pollFn = approve; d.exchErr = errors.New("exchange status 400") }, LoginCodeDeviceFailed},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				coord, registry, dev := newDeviceCoordinator(t)
				tc.setup(dev)
				res, err := coord.Login(context.Background(), "fake", "f", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
				if tc.wantCode == "" {
					if err != nil || !res.Created {
						t.Fatalf("Login() = %+v, %v, want success", res, err)
					}
					return
				}
				requireLoginCode(t, err, tc.wantCode)
				if strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "status") {
					t.Errorf("error %q carries the underlying error text", err.Error())
				}
				if accountCount(t, registry) != 0 || pendingCount(coord) != 0 {
					t.Errorf("accounts = %d, pending = %d, want neither after a failure", accountCount(t, registry), pendingCount(coord))
				}
			})
		}
	})

	t.Run("device relogin keeps identity semantics", func(t *testing.T) {
		t.Parallel()
		coord, registry, dev := newDeviceCoordinator(t)
		dev.pollFn = func(int) (DeviceGrant, error) { return DeviceGrant{AuthorizationCode: "c", CodeVerifier: "v"}, nil }

		dev.subject = "sub-a"
		first, err := coord.Login(context.Background(), "fake", "a", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		if err != nil || !first.Created {
			t.Fatalf("first Login() = %+v, %v", first, err)
		}
		// A different vendor account against the first row is refused.
		dev.subject = "sub-b"
		_, err = coord.Login(context.Background(), "fake", "a", LoginOptions{Method: LoginMethodDevice, AccountID: first.Account.ID}, func(LoginEvent) {})
		requireLoginCode(t, err, LoginCodeIdentityMismatch)
		// The same one replaces its credential in place.
		dev.subject = "sub-a"
		again, err := coord.Login(context.Background(), "fake", "ignored", LoginOptions{Method: LoginMethodDevice, AccountID: first.Account.ID}, func(LoginEvent) {})
		if err != nil || again.Created || again.Account.ID != first.Account.ID {
			t.Fatalf("relogin = %+v, %v, want an in-place update", again, err)
		}
		if n := accountCount(t, registry); n != 1 {
			t.Errorf("accounts = %d, want 1", n)
		}
	})
}

// deviceVendor is a fake auth.openai.com for the real OpenAILogin: the
// usercode, deviceauth/token and oauth/token endpoints behind one test server
// (testHTTPClient rewrites every host to it, keeping the path).
type deviceVendor struct {
	t *testing.T

	userCodeStatus int
	userCodeBody   string // overrides the default 200 body
	pendingPolls   int32  // polls answered 403 (then 404) before the grant
	pollStatus     int    // after pending: 200 grant, or this status
	pollBody       string
	hangPolls      int32 // first polls that hang until the client gives up
	flakyPolls     int32 // then polls answered flakyStatus
	flakyStatus    int
	tokenStatus    int // 200 by default
	tokenBody      string
	subject        string

	polls  atomic.Int32
	mu     sync.Mutex
	forms  []url.Values
	bodies []map[string]string
}

const (
	vendorDeviceAuthID  = "DEVAUTHID-7f3a"
	vendorUserCode      = "WXYZ-9876"
	vendorAuthzCode     = "AUTHZCODE-c41d"
	vendorCodeVerifier  = "VERIFIER-b82e"
	vendorAccessMarker  = "ACCESS-MARK"
	vendorRefreshMarker = "REFRESH-MARK-5e60"
)

func (v *deviceVendor) client(t *testing.T) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(v.serve))
	t.Cleanup(srv.Close)
	return testHTTPClient(t, srv)
}

func (v *deviceVendor) newLogin(t *testing.T) *OpenAILogin {
	t.Helper()
	l := NewOpenAILogin(WithOpenAILoginHTTPClient(v.client(t)))
	l.deviceMinInterval = time.Millisecond
	return l
}

func (v *deviceVendor) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/accounts/deviceauth/usercode":
		v.record(r)
		if v.userCodeStatus != 0 && v.userCodeStatus != http.StatusOK {
			w.WriteHeader(v.userCodeStatus)
			_, _ = w.Write([]byte(v.userCodeBody))
			return
		}
		body := v.userCodeBody
		if body == "" {
			body = fmt.Sprintf(`{"device_auth_id":%q,"user_code":%q,"interval":"0.002"}`, vendorDeviceAuthID, vendorUserCode)
		}
		_, _ = w.Write([]byte(body))
	case "/api/accounts/deviceauth/token":
		v.record(r)
		n := v.polls.Add(1)
		if n <= v.hangPolls {
			// A slow vendor: no answer until the client gives up.
			<-r.Context().Done()
			return
		}
		n -= v.hangPolls
		if n <= v.flakyPolls {
			w.WriteHeader(v.flakyStatus)
			_, _ = w.Write([]byte(`{"error":"FLAKY-BODY"}`))
			return
		}
		n -= v.flakyPolls
		if n <= v.pendingPolls {
			if n%2 == 0 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		if v.pollBody != "" || (v.pollStatus != 0 && v.pollStatus != http.StatusOK) {
			if v.pollStatus != 0 {
				w.WriteHeader(v.pollStatus)
			}
			_, _ = w.Write([]byte(v.pollBody))
			return
		}
		_, _ = fmt.Fprintf(w, `{"authorization_code":%q,"code_verifier":%q,"code_challenge":"challenge"}`, vendorAuthzCode, vendorCodeVerifier)
	case "/oauth/token":
		if err := r.ParseForm(); err != nil {
			v.t.Errorf("parse token form: %v", err)
		}
		v.mu.Lock()
		v.forms = append(v.forms, r.PostForm)
		v.mu.Unlock()
		if v.tokenBody != "" || (v.tokenStatus != 0 && v.tokenStatus != http.StatusOK) {
			if v.tokenStatus != 0 {
				w.WriteHeader(v.tokenStatus)
			}
			_, _ = w.Write([]byte(v.tokenBody))
			return
		}
		access := jwtWithExpiry(v.t, time.Now().Add(time.Hour))
		resp := map[string]string{"access_token": access, "refresh_token": vendorRefreshMarker}
		if v.subject != "" {
			resp["id_token"] = idTokenWithClaims(v.t, map[string]any{
				"sub":                         v.subject,
				"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "acct-" + v.subject},
			})
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		http.NotFound(w, r)
	}
}

func (v *deviceVendor) record(r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		v.t.Errorf("decode %s body: %v", r.URL.Path, err)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		v.t.Errorf("%s Content-Type = %q, want application/json", r.URL.Path, ct)
	}
	v.mu.Lock()
	v.bodies = append(v.bodies, body)
	v.mu.Unlock()
}

func TestOpenAIDeviceLogin_Vendor(t *testing.T) {
	t.Parallel()

	t.Run("device login polls until approved then stores account", func(t *testing.T) {
		t.Parallel()
		vendor := &deviceVendor{t: t, pendingPolls: 3, subject: "user-1"}
		registry := newTestRegistry(t)
		coord := NewLoginCoordinator(registry, map[string]Login{"openai": vendor.newLogin(t)})
		coord.minPoll = time.Millisecond

		s := startDeviceLogin(t, coord, "openai", "codex", LoginOptions{})
		if s.event.UserCode != vendorUserCode || s.event.VerificationURL != "https://auth.openai.com/codex/device" {
			t.Fatalf("event = %+v, want the vendor's code and the Codex verification URL", s.event)
		}
		o := s.wait(t)
		if o.err != nil || !o.res.Created || !o.res.Account.HasIdentity {
			t.Fatalf("Login() = %+v, %v, want a created account with an identity", o.res, o.err)
		}
		stored, err := registry.Get(o.res.Account.ID)
		if err != nil || stored.OAuth == nil || stored.OAuth.RefreshToken != vendorRefreshMarker {
			t.Fatalf("stored = %+v, %v, want the exchanged tokens", stored, err)
		}
		if got := vendor.polls.Load(); got != 4 {
			t.Errorf("polls = %d, want 3 pending then the grant", got)
		}

		// The poll carried the vendor's id and code; the exchange used the
		// device redirect_uri and the vendor's verifier, not one of ours.
		vendor.mu.Lock()
		defer vendor.mu.Unlock()
		if first := vendor.bodies[0]; first["client_id"] != openaiClientID {
			t.Errorf("usercode body = %v, want client_id", first)
		}
		for _, b := range vendor.bodies[1:] {
			if b["device_auth_id"] != vendorDeviceAuthID || b["user_code"] != vendorUserCode {
				t.Errorf("poll body = %v, want the issued device_auth_id and user_code", b)
			}
		}
		if len(vendor.forms) != 1 {
			t.Fatalf("token exchanges = %d, want 1", len(vendor.forms))
		}
		f := vendor.forms[0]
		if f.Get("grant_type") != "authorization_code" || f.Get("client_id") != openaiClientID || f.Get("code") != vendorAuthzCode ||
			f.Get("code_verifier") != vendorCodeVerifier || f.Get("redirect_uri") != "https://auth.openai.com/deviceauth/callback" {
			t.Errorf("exchange form = %v, want the device redirect_uri and the vendor's code and verifier", f)
		}
	})

	t.Run("device login 404 is device_unsupported", func(t *testing.T) {
		t.Parallel()
		vendor := &deviceVendor{t: t, userCodeStatus: http.StatusNotFound, userCodeBody: "VENDOR-404-BODY"}
		l := vendor.newLogin(t)
		if _, err := l.StartDevice(context.Background()); !errors.Is(err, ErrDeviceUnsupported) {
			t.Fatalf("StartDevice() error = %v, want ErrDeviceUnsupported", err)
		}

		coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"openai": l})
		_, err := coord.Login(context.Background(), "openai", "x", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		requireLoginCode(t, err, LoginCodeDeviceUnsupported)
		if strings.Contains(err.Error(), "VENDOR-404-BODY") {
			t.Errorf("error %q carries the vendor body", err.Error())
		}
	})

	t.Run("start parses user_code or usercode and interval as string or number", func(t *testing.T) {
		t.Parallel()
		for name, tc := range map[string]struct {
			body string
			want time.Duration
		}{
			"user_code, int":         {`{"device_auth_id":"d","user_code":"U","interval":7}`, 7 * time.Second},
			"usercode, string":       {`{"device_auth_id":"d","usercode":"U","interval":"9"}`, 9 * time.Second},
			"missing interval":       {`{"device_auth_id":"d","user_code":"U"}`, 5 * time.Second},
			"null interval":          {`{"device_auth_id":"d","user_code":"U","interval":null}`, 5 * time.Second},
			"garbage interval":       {`{"device_auth_id":"d","user_code":"U","interval":"soon"}`, 5 * time.Second},
			"zero interval":          {`{"device_auth_id":"d","user_code":"U","interval":0}`, 5 * time.Second},
			"negative interval":      {`{"device_auth_id":"d","user_code":"U","interval":-3}`, 5 * time.Second},
			"too long is clamped":    {`{"device_auth_id":"d","user_code":"U","interval":600}`, 30 * time.Second},
			"too short is clamped":   {`{"device_auth_id":"d","user_code":"U","interval":0.1}`, time.Second},
			"huge string is clamped": {`{"device_auth_id":"d","user_code":"U","interval":"1e300"}`, 30 * time.Second},
		} {
			vendor := &deviceVendor{t: t, userCodeBody: tc.body}
			// A login with the production 1s floor.
			l := NewOpenAILogin(WithOpenAILoginHTTPClient(vendor.client(t)))
			ch, err := l.StartDevice(context.Background())
			if err != nil {
				t.Errorf("%s: StartDevice() error = %v", name, err)
				continue
			}
			if ch.Interval != tc.want || ch.UserCode != "U" || ch.DeviceAuthID != "d" {
				t.Errorf("%s: challenge = %+v, want interval %v", name, ch, tc.want)
			}
		}

		for name, body := range map[string]string{
			"no code":    `{"device_auth_id":"d"}`,
			"no id":      `{"user_code":"U"}`,
			"not json":   `nope`,
			"empty code": `{"device_auth_id":"d","user_code":" "}`,
		} {
			vendor := &deviceVendor{t: t, userCodeBody: body}
			if _, err := vendor.newLogin(t).StartDevice(context.Background()); err == nil || errors.Is(err, ErrDeviceUnsupported) {
				t.Errorf("%s: StartDevice() error = %v, want a plain failure", name, err)
			}
		}
	})

	t.Run("poll maps 403 and 404 to pending and others to failure", func(t *testing.T) {
		t.Parallel()
		ch := DeviceChallenge{DeviceAuthID: "d", UserCode: "U"}
		// The fake answers 403 to the first pending poll and 404 to the second.
		pending := &deviceVendor{t: t, pendingPolls: 2}
		l := pending.newLogin(t)
		for i, status := range []int{http.StatusForbidden, http.StatusNotFound} {
			if _, err := l.PollDevice(context.Background(), ch); !errors.Is(err, ErrDevicePending) {
				t.Errorf("poll %d (vendor status %d) error = %v, want ErrDevicePending", i+1, status, err)
			}
		}
		if grant, err := l.PollDevice(context.Background(), ch); err != nil || grant.AuthorizationCode != vendorAuthzCode || grant.CodeVerifier != vendorCodeVerifier {
			t.Errorf("approved poll = %+v, %v, want the vendor's grant", grant, err)
		}

		// Other 4xx are hard failures; 429 and 5xx are transient. Neither
		// carries the body.
		for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusGone, http.StatusUnprocessableEntity} {
			failure := &deviceVendor{t: t, pollStatus: status, pollBody: "VENDOR-BODY"}
			_, err := failure.newLogin(t).PollDevice(context.Background(), ch)
			if err == nil || errors.Is(err, ErrDevicePending) || errors.Is(err, ErrDeviceTransient) || strings.Contains(err.Error(), "VENDOR-BODY") {
				t.Errorf("status %d: PollDevice() error = %v, want a non-pending, non-transient failure without the body", status, err)
			}
		}
		for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
			flaky := &deviceVendor{t: t, pollStatus: status, pollBody: "VENDOR-BODY"}
			_, err := flaky.newLogin(t).PollDevice(context.Background(), ch)
			if !errors.Is(err, ErrDeviceTransient) || strings.Contains(err.Error(), "VENDOR-BODY") {
				t.Errorf("status %d: PollDevice() error = %v, want ErrDeviceTransient without the body", status, err)
			}
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"authorization_code":"c"}`))
		}))
		defer srv.Close()
		l = NewOpenAILogin(WithOpenAILoginHTTPClient(testHTTPClient(t, srv)))
		if _, err := l.PollDevice(context.Background(), ch); err == nil || errors.Is(err, ErrDevicePending) {
			t.Errorf("PollDevice() with no verifier error = %v, want a failure", err)
		}

		// A request that does not complete is transient.
		dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		deadClient := testHTTPClient(t, dead)
		dead.Close()
		l = NewOpenAILogin(WithOpenAILoginHTTPClient(deadClient))
		if _, err := l.PollDevice(context.Background(), ch); !errors.Is(err, ErrDeviceTransient) {
			t.Errorf("PollDevice() against a dead server error = %v, want ErrDeviceTransient", err)
		}
		// ...unless the caller gave up, which is the caller's own error.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := l.PollDevice(ctx, ch); !errors.Is(err, context.Canceled) {
			t.Errorf("PollDevice() with a cancelled ctx error = %v, want context.Canceled", err)
		}
	})

	t.Run("device login retries 429 and 5xx polls", func(t *testing.T) {
		t.Parallel()
		for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
			// Up to maxDeviceRetries failures in a row are absorbed.
			vendor := &deviceVendor{t: t, flakyPolls: maxDeviceRetries, flakyStatus: status, pendingPolls: 1}
			coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"openai": vendor.newLogin(t)})
			coord.minPoll = time.Millisecond
			if _, err := coord.Login(context.Background(), "openai", "x", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {}); err != nil {
				t.Errorf("status %d: Login() error = %v, want the retries to absorb %d failures", status, err, maxDeviceRetries)
			}

			// One more in a row ends the login.
			vendor = &deviceVendor{t: t, flakyPolls: maxDeviceRetries + 1, flakyStatus: status}
			coord = NewLoginCoordinator(newTestRegistry(t), map[string]Login{"openai": vendor.newLogin(t)})
			coord.minPoll = time.Millisecond
			_, err := coord.Login(context.Background(), "openai", "x", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
			requireLoginCode(t, err, LoginCodeDeviceFailed)
			if got := vendor.polls.Load(); got != maxDeviceRetries+1 {
				t.Errorf("status %d: polls = %d, want %d (the first plus three retries)", status, got, maxDeviceRetries+1)
			}
		}

		// Other 4xx fail on the first poll.
		for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusGone} {
			vendor := &deviceVendor{t: t, pollStatus: status}
			coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"openai": vendor.newLogin(t)})
			coord.minPoll = time.Millisecond
			_, err := coord.Login(context.Background(), "openai", "x", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
			requireLoginCode(t, err, LoginCodeDeviceFailed)
			if got := vendor.polls.Load(); got != 1 {
				t.Errorf("status %d: polls = %d, want the login to stop at the first", status, got)
			}
		}
	})

	t.Run("device login retries a poll that times out", func(t *testing.T) {
		t.Parallel()

		// The vendor hangs on the first two polls; each is cut off by the
		// per-poll timeout and retried, then the third is approved.
		vendor := &deviceVendor{t: t, hangPolls: 2}
		coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"openai": vendor.newLogin(t)})
		coord.minPoll = time.Millisecond
		coord.pollTimeout = 40 * time.Millisecond
		res, err := coord.Login(context.Background(), "openai", "x", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		if err != nil || !res.Created {
			t.Fatalf("Login() = %+v, %v, want the slow polls retried", res, err)
		}
		if got := vendor.polls.Load(); got != 3 {
			t.Errorf("polls = %d, want 3 (two timeouts then the grant)", got)
		}

		// A vendor that always hangs exhausts the retries instead of looping.
		vendor = &deviceVendor{t: t, hangPolls: 1 << 20}
		coord = NewLoginCoordinator(newTestRegistry(t), map[string]Login{"openai": vendor.newLogin(t)})
		coord.minPoll = time.Millisecond
		coord.pollTimeout = 20 * time.Millisecond
		_, err = coord.Login(context.Background(), "openai", "x", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		requireLoginCode(t, err, LoginCodeDeviceFailed)
		if got := vendor.polls.Load(); got != maxDeviceRetries+1 {
			t.Errorf("polls = %d, want %d", got, maxDeviceRetries+1)
		}
	})

	t.Run("device login exchange yields the browser path's result", func(t *testing.T) {
		t.Parallel()
		vendor := &deviceVendor{t: t, subject: "user-9"}
		l := vendor.newLogin(t)
		res, err := l.ExchangeDevice(context.Background(), DeviceGrant{AuthorizationCode: "c", CodeVerifier: "v"})
		if err != nil {
			t.Fatalf("ExchangeDevice() error = %v", err)
		}
		if res.Credential.RefreshToken != vendorRefreshMarker || res.Credential.ExpiresAt.Before(time.Now()) {
			t.Errorf("credential = %+v, want the tokens with a future expiry", res.Credential)
		}
		want := openAISubject(idTokenWithClaims(t, map[string]any{
			"sub":                         "user-9",
			"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "acct-user-9"},
		}))
		if res.Subject == "" || res.Subject != want {
			t.Errorf("subject mismatch: got %q, want the browser path's subject derivation", res.Subject)
		}
	})
}

// TestDeviceLoginSecretsNeverAppearInLogsOrErrors is "device login secrets
// never appear in logs or errors": across a successful login and every failure
// path (non-2xx at each of the three endpoints with a distinctive vendor body,
// an unreadable response, a cancelled login), the device code, device_auth_id,
// authorization code, verifier, tokens and vendor bodies stay out of every
// error and everything logged. Not parallel: it swaps the process-wide log and
// slog destinations.
func TestDeviceLoginSecretsNeverAppearInLogsOrErrors(t *testing.T) {
	var logs bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	prevSlog := slog.Default()
	log.SetOutput(&logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
		slog.SetDefault(prevSlog)
	})

	const bodySecret = "VENDORBODY-SECRET-0a1b2c"
	leaky := fmt.Sprintf(`{"error":%q,"device_auth_id":%q,"user_code":%q,"authorization_code":%q,"code_verifier":%q,"access_token":%q}`,
		bodySecret, vendorDeviceAuthID, vendorUserCode, vendorAuthzCode, vendorCodeVerifier, vendorAccessMarker)

	var errs []string
	run := func(name string, vendor *deviceVendor, cancelAfter time.Duration) {
		coord := NewLoginCoordinator(newTestRegistry(t), map[string]Login{"openai": vendor.newLogin(t)})
		coord.minPoll = time.Millisecond
		ctx := context.Background()
		if cancelAfter > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, cancelAfter)
			defer cancel()
		}
		res, err := coord.Login(ctx, "openai", "secrets", LoginOptions{Method: LoginMethodDevice}, func(LoginEvent) {})
		if err == nil {
			if name != "success" {
				t.Errorf("%s: Login() error = nil, want a failure", name)
			}
			return
		}
		if name == "success" {
			t.Errorf("success: Login() error = %v (%+v)", err, res)
		}
		errs = append(errs, err.Error())
	}

	run("success", &deviceVendor{t: t, pendingPolls: 1, subject: "user-s"}, 0)
	run("usercode 500", &deviceVendor{t: t, userCodeStatus: 500, userCodeBody: leaky}, 0)
	run("usercode 404", &deviceVendor{t: t, userCodeStatus: 404, userCodeBody: leaky}, 0)
	run("usercode garbage", &deviceVendor{t: t, userCodeBody: bodySecret}, 0)
	run("poll 500", &deviceVendor{t: t, pollStatus: 500, pollBody: leaky}, 0)
	run("poll garbage", &deviceVendor{t: t, pollStatus: 200, pollBody: bodySecret, pendingPolls: 0}, 0)
	run("exchange 400", &deviceVendor{t: t, tokenStatus: 400, tokenBody: leaky}, 0)
	run("exchange garbage", &deviceVendor{t: t, tokenStatus: 200, tokenBody: bodySecret}, 0)
	run("cancelled", &deviceVendor{t: t, pendingPolls: 1 << 20}, 40*time.Millisecond)

	secrets := []string{bodySecret, vendorDeviceAuthID, vendorUserCode, vendorAuthzCode, vendorCodeVerifier, vendorAccessMarker, vendorRefreshMarker}
	for _, secret := range secrets {
		for _, msg := range errs {
			if strings.Contains(msg, secret) {
				t.Errorf("an error leaks %q: %q", secret, msg)
			}
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logs leak %q: %q", secret, logs.String())
		}
	}
	if len(errs) != 8 {
		t.Errorf("collected %d errors %q, want one per failure path", len(errs), errs)
	}
}
