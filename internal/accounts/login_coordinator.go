package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// loginCallbackTimeout bounds how long LoginCoordinator.Login waits for the
// vendor's browser redirect to land on the local callback listener before
// giving up.
const loginCallbackTimeout = 5 * time.Minute

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
	// instead of a real 5-minute wait (see login_coordinator_test.go) --
	// not exposed as a constructor option, since there's no legitimate
	// reason for a real caller to want a different value.
	timeout time.Duration

	mu       sync.Mutex
	inFlight map[string]bool
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
		inFlight: make(map[string]bool),
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

// LoginOptions are the optional inputs of Login.
type LoginOptions struct {
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
	LoginCodeIdentityMismatch   = "identity_mismatch"
	LoginCodeIdentityInUse      = "identity_in_use"
	LoginCodeUnknownAccount     = "login_unknown_account"
	LoginCodeIdentityUnverified = "identity_unverified"
	LoginCodeNotOAuthAccount    = "login_not_oauth_account"
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
// callback listener, invokes onAuthorizeURL with the URL a browser should
// be sent to (only once the listener is actually bound -- see
// newCallbackServer's doc comment for why that ordering matters), blocks
// until the vendor's redirect lands or loginCallbackTimeout elapses,
// validates state, exchanges the code for tokens, and stores the result
// (see persist for the re-login semantics of ADR-0033 D6). The listener and
// this provider's in-flight flag are released on every exit path,
// including ctx being cancelled mid-flow.
func (c *LoginCoordinator) Login(ctx context.Context, provider, label string, opts LoginOptions, onAuthorizeURL func(url string)) (LoginResult, error) {
	login, ok := c.logins[provider]
	if !ok {
		return LoginResult{}, fmt.Errorf("oauth login: unsupported provider %q", provider)
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
		return LoginResult{}, fmt.Errorf("oauth login: a login for provider %q is already in progress", provider)
	}
	defer c.release(provider)

	pkce, err := GeneratePKCECodes()
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}

	server, err := newCallbackServer(login.CallbackAddr(), login.CallbackPath(), providerDisplayLabel(provider))
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	defer server.close()

	onAuthorizeURL(login.AuthorizeURL(state, pkce))

	result, err := server.waitForCallback(ctx, c.timeout)
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	if result.errCode != "" {
		return LoginResult{}, fmt.Errorf("oauth login: provider %q denied or errored: %s", provider, result.errCode)
	}
	if result.code == "" {
		return LoginResult{}, fmt.Errorf("oauth login: callback carried no authorization code")
	}
	if result.state != state {
		return LoginResult{}, fmt.Errorf("oauth login: callback state mismatch")
	}

	exchanged, err := login.Exchange(ctx, result.code, result.state, pkce)
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}

	return c.persist(provider, label, opts, exchanged)
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
