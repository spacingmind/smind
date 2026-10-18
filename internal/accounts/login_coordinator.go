package accounts

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// loginCallbackTimeout bounds how long LoginCoordinator.Login waits for the
// login to be completed -- the vendor's redirect landing on the local
// callback listener, or the user pasting it into Complete -- before giving
// up. It is the lifetime of a loginId (ADR-0033 D3).
const loginCallbackTimeout = 10 * time.Minute

// maxCompleteAttempts is how many Complete calls one login accepts; a wrong
// paste counts. The login stays pending after the limit is hit (it only stops
// answering pastes), until it times out or is cancelled.
const maxCompleteAttempts = 5

// Login methods (ADR-0033 D2). Browser is the PKCE authorization-code flow
// with a callback listener and/or a pasted redirect; device is the vendor's
// device-code flow, which binds no listener.
const (
	LoginMethodBrowser = "browser"
	LoginMethodDevice  = "device"

	loginMethodBrowser = LoginMethodBrowser
	loginMethodDevice  = LoginMethodDevice
)

// NormalizeLoginMethod maps a requested method to a known one: "" is the
// default, browser. Anything else unknown is a login_bad_method error whose
// text does not echo the value.
func NormalizeLoginMethod(method string) (string, error) {
	switch method {
	case "", LoginMethodBrowser:
		return LoginMethodBrowser, nil
	case LoginMethodDevice:
		return LoginMethodDevice, nil
	}
	return "", &LoginError{Code: LoginCodeBadMethod, Detail: `the login method must be "browser" or "device"`}
}

// LoginCoordinator drives the browser-based OAuth login flow for the
// providers in logins: generating PKCE/state, standing up the vendor's
// fixed-address local callback listener, handing the caller an authorize
// URL to open, waiting for the callback, exchanging the code, and
// persisting the result via registry. One coordinator is shared by every
// caller (wsapi's account.oauthStart handler and, indirectly, every
// connection that calls it), so it also guards against two logins for the
// same provider racing each other over the one listener address that
// provider's vendor allows.
type LoginCoordinator struct {
	registry *Registry
	logins   map[string]Login

	// timeout defaults to loginCallbackTimeout; it's a field rather than
	// using the const directly so this package's own tests can shorten it
	// instead of a real 10-minute wait (see login_coordinator_test.go) --
	// not exposed as a constructor option, since there's no legitimate
	// reason for a real caller to want a different value.
	timeout time.Duration

	// deviceTimeout (default deviceLoginTimeout) is how long a device login
	// polls before device_expired, and minPoll (default deviceMinInterval)
	// floors the interval between polls; both are fields so tests can shorten
	// them, like timeout.
	deviceTimeout time.Duration
	minPoll       time.Duration
	// pollTimeout (default devicePollRequestTimeout) bounds one poll request;
	// a poll that hits it is retried like any transient failure.
	pollTimeout time.Duration

	mu       sync.Mutex
	inFlight map[string]bool
	// pending maps a loginId to the login it names, for the life of one Login
	// call. Guarded by mu.
	pending map[string]*pendingLogin
}

// pendingLogin is the part of a running Login that Complete may touch. All
// fields are guarded by LoginCoordinator.mu except paste, which is a channel.
type pendingLogin struct {
	provider string
	state    string
	method   string
	// paste carries the one accepted pasted redirect to Login. Capacity 1;
	// only the goroutine that flips resolved to true sends on it.
	paste chan callbackResult
	// attempts counts Complete calls, accepted or not.
	attempts int
	// resolved is the single arbiter of "first of listener callback and paste
	// wins": whoever sets it under mu owns the login's outcome.
	resolved bool
	// ended is set with resolved when the login ended without a delivered
	// result (ctx cancelled or timed out).
	ended bool
}

// NewLoginCoordinator returns a LoginCoordinator backed by registry, using
// logins as the provider -> Login lookup. Exported (rather than folded into
// NewDefaultLoginCoordinator) so tests can inject a fake Login without a
// real vendor endpoint.
func NewLoginCoordinator(registry *Registry, logins map[string]Login) *LoginCoordinator {
	return &LoginCoordinator{
		registry: registry,
		logins:   logins,
		timeout:  loginCallbackTimeout,

		deviceTimeout: deviceLoginTimeout,
		minPoll:       deviceMinInterval,
		pollTimeout:   devicePollRequestTimeout,
		inFlight:      make(map[string]bool),
		pending:       make(map[string]*pendingLogin),
	}
}

// NewDefaultLoginCoordinator returns a LoginCoordinator wired to the two
// real providers with a login flow: anthropic and openai (see the plan's
// Decisions section for why the scope stops there this pass).
func NewDefaultLoginCoordinator(registry *Registry) *LoginCoordinator {
	return NewLoginCoordinator(registry, map[string]Login{
		"anthropic": NewAnthropicLogin(),
		"openai":    NewOpenAILogin(),
	})
}

// LoginEvent is what Login reports while a login is under way, through the
// onEvent callback. Kind selects which fields are set.
type LoginEvent struct {
	// Kind is "authorizeUrl" (browser login: open URL) or "deviceCode" (device
	// login: show UserCode and have the user enter it at VerificationURL).
	Kind    string
	LoginID string

	// authorizeUrl.
	URL string
	// PasteOnly: no callback listener could be bound (its address is in use),
	// so the login can only finish by Complete; a UI shows the paste field
	// immediately.
	PasteOnly bool

	// deviceCode.
	VerificationURL string
	UserCode        string
	// ExpiresAt is when the daemon stops polling.
	ExpiresAt       time.Time
	IntervalSeconds int
}

// Login event kinds.
const (
	LoginEventAuthorizeURL = "authorizeUrl"
	LoginEventDeviceCode   = "deviceCode"
)

// LoginOptions are the optional inputs of Login.
type LoginOptions struct {
	// Method is "", "browser" (the default) or "device"; see NormalizeLoginMethod.
	Method string
	// AccountID, when non-zero, is the row a row-level Re-login targets
	// (ADR-0033 D6): its credential is replaced, never a new row created.
	// 0 = none.
	AccountID int64
}

// LoginResult is a finished login: the account it produced or updated, and
// which of ADR-0033 D6's outcomes happened.
type LoginResult struct {
	Account Account
	// Created: a new row was inserted. False means an existing row's
	// credential was replaced in place.
	Created bool
	// Adopted: the credential of a legacy row (no identity) was replaced and
	// the login's identity adopted onto it.
	Adopted bool
}

// Stable error codes (ADR-0033 login contract). They are the message prefix
// of the error Login returns; LoginError carries them for errors.As.
const (
	LoginCodeInProgress         = "login_in_progress"
	LoginCodeIdentityMismatch   = "identity_mismatch"
	LoginCodeIdentityInUse      = "identity_in_use"
	LoginCodeUnknownAccount     = "login_unknown_account"
	LoginCodeIdentityUnverified = "identity_unverified"
	LoginCodeNotOAuthAccount    = "login_not_oauth_account"

	// Codes of the paste-the-redirect flow and the listener path
	// (ADR-0033 "Login contract").
	LoginCodeUnknown          = "login_unknown"
	LoginCodeWrongMethod      = "login_wrong_method"
	LoginCodeAttemptsExceeded = "login_attempts_exceeded"
	LoginCodeBadRedirect      = "login_bad_redirect"
	LoginCodeAlreadyCompleted = "login_already_completed"
	LoginCodeStateMismatch    = "state_mismatch"
	LoginCodeDenied           = "denied"
	LoginCodeTimeout          = "timeout"

	// Codes of the method choice and the device-code flow.
	LoginCodeBadMethod         = "login_bad_method"
	LoginCodeDeviceUnsupported = "device_unsupported"
	LoginCodeDeviceExpired     = "device_expired"
	LoginCodeDeviceFailed      = "device_failed"
)

// LoginError is an error with a stable machine-readable code. Detail never
// carries an identity, subject, token or code.
type LoginError struct {
	Code   string
	Detail string
}

func (e *LoginError) Error() string { return e.Code + ": " + e.Detail }

// Login runs one provider's OAuth login flow to completion: it generates
// PKCE codes and a state token, binds that provider's vendor-fixed
// callback listener, registers a loginId for the login, invokes
// onAuthorizeURL with that id and the URL a browser should be sent to (only
// once the listener is actually bound -- see newCallbackServer's doc comment
// for why that ordering matters), blocks until the login is completed or
// timeout elapses, validates state, exchanges the code for tokens, and
// stores the result (see persist for the re-login semantics of ADR-0033 D6).
//
// The login is completed by whichever comes first: the vendor's redirect
// landing on the listener, or a redirect pasted into Complete under the
// loginId; the loser is ignored. The listener, the loginId and this
// provider's in-flight flag are released on every exit path, including ctx
// being cancelled mid-flow.
func (c *LoginCoordinator) Login(ctx context.Context, provider, label string, opts LoginOptions, onEvent func(LoginEvent)) (LoginResult, error) {
	login, ok := c.logins[provider]
	if !ok {
		return LoginResult{}, fmt.Errorf("oauth login: unsupported provider %q", provider)
	}

	method, err := NormalizeLoginMethod(opts.Method)
	if err != nil {
		return LoginResult{}, err
	}
	var device DeviceLogin
	if method == loginMethodDevice {
		// Rejected before any claim, listener or request: only providers with
		// a device flow take this method.
		if device, ok = login.(DeviceLogin); !ok {
			return LoginResult{}, &LoginError{Code: LoginCodeDeviceUnsupported, Detail: fmt.Sprintf("%s does not offer a device-code login", providerDisplayLabel(provider))}
		}
	}

	// Fail fast on a Re-login aimed at a row that is gone or belongs to
	// another provider, before a listener is bound or the user is sent to a
	// browser. persist re-checks after the exchange.
	if opts.AccountID != 0 {
		if _, err := c.reloginTarget(provider, opts.AccountID); err != nil {
			return LoginResult{}, err
		}
	}

	if !c.claim(provider) {
		return LoginResult{}, &LoginError{Code: LoginCodeInProgress, Detail: fmt.Sprintf("a login for provider %q is already in progress", provider)}
	}
	defer c.release(provider)

	if device != nil {
		return c.loginDevice(ctx, provider, label, opts, device, onEvent)
	}

	pkce, err := GeneratePKCECodes()
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	loginID, err := newLoginID()
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}

	// A busy callback port (another login tool, a second daemon) is not fatal:
	// the login continues without a listener and can only be finished by
	// Complete (paste-only). Any other bind failure, the loopback guards
	// included, still fails the login.
	server, err := newCallbackServer(login.CallbackAddr(), login.CallbackPath(), providerDisplayLabel(provider), state)
	pasteOnly := false
	switch {
	case isAddrInUse(err):
		server, pasteOnly = nil, true
	case err != nil:
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	default:
		defer server.close()
	}

	pl := &pendingLogin{provider: provider, state: state, method: loginMethodBrowser, paste: make(chan callbackResult, 1)}
	c.register(loginID, pl)
	defer c.unregister(loginID)

	onEvent(LoginEvent{Kind: LoginEventAuthorizeURL, LoginID: loginID, URL: login.AuthorizeURL(state, pkce), PasteOnly: pasteOnly})

	result, err := c.await(ctx, loginID, server, pl)
	if err != nil {
		var le *LoginError
		if errors.As(err, &le) {
			return LoginResult{}, err
		}
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	if result.errCode != "" {
		return LoginResult{}, &LoginError{Code: LoginCodeDenied, Detail: fmt.Sprintf("provider %q denied or errored: %s", provider, safeErrCode(result.errCode))}
	}
	if result.code == "" {
		return LoginResult{}, &LoginError{Code: LoginCodeBadRedirect, Detail: "the login carried no authorization code"}
	}
	if subtle.ConstantTimeCompare([]byte(result.state), []byte(state)) != 1 {
		return LoginResult{}, &LoginError{Code: LoginCodeStateMismatch, Detail: "the login's state did not match; start the login again"}
	}

	exchanged, err := login.Exchange(ctx, result.code, result.state, pkce)
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}

	return c.persist(provider, label, opts, exchanged)
}

// await blocks until the login resolves: the listener's callback, a pasted
// redirect accepted by Complete, ctx being done, or the timeout. Exactly one
// of them wins. pl.resolved, set under c.mu, is the arbiter, so a Complete
// that already returned nil is never discarded in favour of a later event.
func (c *LoginCoordinator) await(ctx context.Context, loginID string, server *callbackServer, pl *pendingLogin) (callbackResult, error) {
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()

	// A paste-only login has no listener: its nil channel never fires.
	var listenerResult <-chan callbackResult
	if server != nil {
		listenerResult = server.result
	}

	var res callbackResult
	var werr error
	select {
	case res = <-pl.paste:
		// Complete flipped resolved before sending.
		return res, nil
	case res = <-listenerResult:
	case <-ctx.Done():
		werr = ctx.Err()
	case <-timer.C:
		werr = &LoginError{Code: LoginCodeTimeout, Detail: "timed out waiting for the login to complete"}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if pl.resolved {
		// A Complete won the race for the same instant; its paste is
		// already buffered (sent under this mutex, together with resolved).
		return <-pl.paste, nil
	}
	pl.resolved = true
	if werr != nil {
		// Ended without a delivered result (ctx or timeout): the login is
		// over, so a Complete racing with Login's return is told the id is
		// unknown rather than "already completed".
		pl.ended = true
		delete(c.pending, loginID)
	}
	return res, werr
}

// Complete finishes a browser login from a redirect the user pasted (the
// vendor's redirect could not reach the daemon's listener -- ADR-0033 D3). It
// returns as soon as the paste is accepted and never waits for the token
// exchange: the Login call that issued loginID carries the final result.
//
// Errors are LoginErrors: login_unknown (no such, finished or expired
// loginId), login_wrong_method, login_attempts_exceeded (the login stays
// pending), login_bad_redirect (counts as an attempt), state_mismatch (the
// login stays pending) and login_already_completed (the listener or an
// earlier paste got there first). A pasted error= must carry the matching
// state like any paste, and is then delivered like the listener's, failing
// the login with denied. No error text carries the
// pasted text, code or state.
func (c *LoginCoordinator) Complete(loginID, redirectURL string) error {
	c.mu.Lock()
	pl, ok := c.pending[loginID]
	if !ok {
		c.mu.Unlock()
		return &LoginError{Code: LoginCodeUnknown, Detail: "no login with that id is pending (it finished, expired, or was cancelled)"}
	}
	if pl.method != loginMethodBrowser {
		c.mu.Unlock()
		return &LoginError{Code: LoginCodeWrongMethod, Detail: "this login does not take a pasted redirect"}
	}
	if pl.attempts >= maxCompleteAttempts {
		c.mu.Unlock()
		return &LoginError{Code: LoginCodeAttemptsExceeded, Detail: "too many attempts for this login; wait for it to expire or start a new one"}
	}
	pl.attempts++
	state := pl.state
	c.mu.Unlock()

	res, err := parsePastedRedirect(redirectURL)
	if err != nil {
		return err
	}
	// An error= paste must prove the state too, or anyone holding the loginId
	// could end the login.
	if subtle.ConstantTimeCompare([]byte(res.state), []byte(state)) != 1 {
		return &LoginError{Code: LoginCodeStateMismatch, Detail: "the pasted redirect belongs to a different login; paste the one from this login's page"}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if pl.ended {
		return &LoginError{Code: LoginCodeUnknown, Detail: "no login with that id is pending (it finished, expired, or was cancelled)"}
	}
	if pl.resolved {
		return &LoginError{Code: LoginCodeAlreadyCompleted, Detail: "this login was already completed"}
	}
	pl.resolved = true
	pl.paste <- res // capacity 1, and resolved gates it to one send
	return nil
}

// newLoginID returns 16 random bytes, base64url-encoded.
func newLoginID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate login id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (c *LoginCoordinator) register(loginID string, pl *pendingLogin) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[loginID] = pl
}

func (c *LoginCoordinator) unregister(loginID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, loginID)
}

// reloginTarget loads the row a Re-login names, as a LoginCodeUnknownAccount
// error when it does not exist or belongs to a different provider, and as a
// LoginCodeNotOAuthAccount error when it is not an OAuth row.
func (c *LoginCoordinator) reloginTarget(provider string, id int64) (store.Account, error) {
	target, err := c.registry.stored(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Account{}, &LoginError{Code: LoginCodeUnknownAccount, Detail: fmt.Sprintf("account %d does not exist", id)}
	}
	if err != nil {
		return store.Account{}, fmt.Errorf("oauth login: %w", err)
	}
	if target.Provider != provider {
		return store.Account{}, &LoginError{Code: LoginCodeUnknownAccount, Detail: fmt.Sprintf("account %d belongs to a different provider", id)}
	}
	// A pasted API-key row is not a vendor login: re-logging it in would
	// silently convert it to OAuth (and adopt an identity it never had).
	if target.CredentialType != CredentialTypeOAuth {
		return store.Account{}, &LoginError{Code: LoginCodeNotOAuthAccount, Detail: fmt.Sprintf("account %d does not use an OAuth login", id)}
	}
	return target, nil
}

// persist stores a finished exchange per ADR-0033 D6. The identity is derived
// from the exchange's subject and never logged or put in an error.
//
//  1. A named row (AccountID) with a different identity: identity_mismatch.
//  2. A named row with no identity: its credential is replaced and the
//     identity adopted; identity_in_use if another row owns it.
//  3. An existing row of the provider owns the identity (and is the named
//     row, or none was named): its credential is replaced in place.
//  4. Otherwise a new row is created with the identity.
//
// A login without a subject has no identity: it creates a row without one,
// or replaces the credential of a named legacy row (identity stays unset). A
// named row that already has an identity is never overwritten by a login
// that cannot prove who it is: identity_unverified.
func (c *LoginCoordinator) persist(provider, label string, opts LoginOptions, exchanged ExchangeResult) (LoginResult, error) {
	identity := AccountIdentity(provider, exchanged.Subject)

	var target store.Account
	if opts.AccountID != 0 {
		var err error
		if target, err = c.reloginTarget(provider, opts.AccountID); err != nil {
			return LoginResult{}, err
		}
	}

	var owner store.Account
	var owned bool
	if identity != "" {
		var err error
		if owner, owned, err = c.registry.findByIdentity(provider, identity); err != nil {
			return LoginResult{}, fmt.Errorf("oauth login: %w", err)
		}
	}

	switch {
	case opts.AccountID != 0 && identity == "" && target.Identity != "":
		// Nothing proves this login is the vendor account the row holds, so
		// the identity-bearing row's credential is not overwritten.
		return LoginResult{}, &LoginError{Code: LoginCodeIdentityUnverified, Detail: fmt.Sprintf("this login did not identify its %s account, so account %d was not changed", providerDisplayLabel(provider), target.ID)}
	case opts.AccountID != 0 && identity == "":
		// A legacy row (no identity) cannot be verified either, but there is
		// nothing to protect: replace its credential, identity untouched
		// (and, as it may be another vendor account, its cooldowns cleared).
		return c.replaced(provider, target.ID, exchanged.Credential, "", false, false)
	case opts.AccountID != 0 && owned && owner.ID == target.ID:
		return c.replaced(provider, target.ID, exchanged.Credential, "", false, true)
	case opts.AccountID != 0 && target.Identity != "":
		return LoginResult{}, &LoginError{Code: LoginCodeIdentityMismatch, Detail: fmt.Sprintf("this login is a different %s account than account %d", providerDisplayLabel(provider), target.ID)}
	case opts.AccountID != 0 && owned:
		return LoginResult{}, identityInUse(owner.ID)
	case opts.AccountID != 0:
		return c.replaced(provider, target.ID, exchanged.Credential, identity, true, false)
	case owned:
		return c.replaced(provider, owner.ID, exchanged.Credential, "", false, true)
	}

	created, err := c.registry.AddOAuthWithIdentity(provider, label, exchanged.Credential, identity)
	if errors.Is(err, store.ErrIdentityInUse) {
		return LoginResult{}, c.identityInUseNow(provider, identity)
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	account, err := c.registry.Get(created.ID)
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	return LoginResult{Account: account, Created: true}, nil
}

// replaced swaps id's credential (see Registry.ReplaceOAuth for adoptIdentity
// and sameAccount) and returns the refreshed row.
func (c *LoginCoordinator) replaced(provider string, id int64, cred OAuthCredential, adoptIdentity string, adopted, sameAccount bool) (LoginResult, error) {
	if _, err := c.registry.ReplaceOAuth(id, cred, adoptIdentity, sameAccount); err != nil {
		if errors.Is(err, store.ErrIdentityInUse) {
			return LoginResult{}, c.identityInUseNow(provider, adoptIdentity)
		}
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	account, err := c.registry.Get(id)
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	return LoginResult{Account: account, Adopted: adopted}, nil
}

func identityInUse(ownerID int64) error {
	return &LoginError{Code: LoginCodeIdentityInUse, Detail: fmt.Sprintf("this login is already connected as account %d", ownerID)}
}

// identityInUseNow builds the identity_in_use error after losing a race for
// an identity at write time, naming the owner when it can be found.
func (c *LoginCoordinator) identityInUseNow(provider, identity string) error {
	if owner, owned, err := c.registry.findByIdentity(provider, identity); err == nil && owned {
		return identityInUse(owner.ID)
	}
	return &LoginError{Code: LoginCodeIdentityInUse, Detail: "this login is already connected to another account"}
}

// claim reports whether provider was not already in flight, atomically
// marking it in flight if so.
func (c *LoginCoordinator) claim(provider string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inFlight[provider] {
		return false
	}
	c.inFlight[provider] = true
	return true
}

func (c *LoginCoordinator) release(provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inFlight, provider)
}

// providerDisplayLabels are the human-facing names the browser-facing
// callback page (oauth_callback_page.go) shows -- purely cosmetic, distinct
// from and not a source of truth for the provider id itself.
var providerDisplayLabels = map[string]string{
	"anthropic": "Anthropic",
	"openai":    "OpenAI",
}

// providerDisplayLabel returns provider's display name, falling back to the
// raw id for anything not in providerDisplayLabels (defensive only --
// LoginCoordinator.Login already rejects unknown providers before this is
// ever called).
func providerDisplayLabel(provider string) string {
	if label, ok := providerDisplayLabels[provider]; ok {
		return label
	}
	return provider
}
