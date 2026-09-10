package accounts

import (
	"context"
	"fmt"
	"sync"
	"time"
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

// Login runs one provider's OAuth login flow to completion: it generates
// PKCE codes and a state token, binds that provider's vendor-fixed
// callback listener, invokes onAuthorizeURL with the URL a browser should
// be sent to (only once the listener is actually bound -- see
// newCallbackServer's doc comment for why that ordering matters), blocks
// until the vendor's redirect lands or loginCallbackTimeout elapses,
// validates state, exchanges the code for tokens, and stores the result via
// registry.AddOAuth. The listener and this provider's in-flight flag are
// released on every exit path, including ctx being cancelled mid-flow.
func (c *LoginCoordinator) Login(ctx context.Context, provider, label string, onAuthorizeURL func(url string)) (Account, error) {
	login, ok := c.logins[provider]
	if !ok {
		return Account{}, fmt.Errorf("oauth login: unsupported provider %q", provider)
	}

	if !c.claim(provider) {
		return Account{}, fmt.Errorf("oauth login: a login for provider %q is already in progress", provider)
	}
	defer c.release(provider)

	pkce, err := GeneratePKCECodes()
	if err != nil {
		return Account{}, fmt.Errorf("oauth login: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return Account{}, fmt.Errorf("oauth login: %w", err)
	}

	server, err := newCallbackServer(login.CallbackAddr(), login.CallbackPath(), providerDisplayLabel(provider))
	if err != nil {
		return Account{}, fmt.Errorf("oauth login: %w", err)
	}
	defer server.close()

	onAuthorizeURL(login.AuthorizeURL(state, pkce))

	result, err := server.waitForCallback(ctx, c.timeout)
	if err != nil {
		return Account{}, fmt.Errorf("oauth login: %w", err)
	}
	if result.errCode != "" {
		return Account{}, fmt.Errorf("oauth login: provider %q denied or errored: %s", provider, result.errCode)
	}
	if result.code == "" {
		return Account{}, fmt.Errorf("oauth login: callback carried no authorization code")
	}
	if result.state != state {
		return Account{}, fmt.Errorf("oauth login: callback state mismatch")
	}

	cred, err := login.Exchange(ctx, result.code, result.state, pkce)
	if err != nil {
		return Account{}, fmt.Errorf("oauth login: %w", err)
	}

	created, err := c.registry.AddOAuth(provider, label, cred)
	if err != nil {
		return Account{}, fmt.Errorf("oauth login: %w", err)
	}
	account, err := c.registry.Get(created.ID)
	if err != nil {
		return Account{}, fmt.Errorf("oauth login: %w", err)
	}
	return account, nil
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
