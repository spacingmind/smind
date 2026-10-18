package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// The Codex CLI's device-code endpoints and redirect_uri (ADR-0033 D2).
	openaiDeviceUserCodeURL     = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	openaiDeviceTokenURL        = "https://auth.openai.com/api/accounts/deviceauth/token"
	openaiDeviceVerificationURL = "https://auth.openai.com/codex/device"
	openaiDeviceRedirectURI     = "https://auth.openai.com/deviceauth/callback"

	// deviceLoginTimeout is how long a device login polls before giving up
	// (the Codex CLI's own cap).
	deviceLoginTimeout = 15 * time.Minute

	deviceDefaultInterval = 5 * time.Second
	deviceMinInterval     = time.Second
	deviceMaxInterval     = 30 * time.Second

	// maxDeviceResponseBytes bounds how much of a vendor response is read.
	maxDeviceResponseBytes = 1 << 20
)

// DeviceChallenge is what a vendor's device-code endpoint issued: the code the
// user types at VerificationURL, and the opaque id the daemon polls with.
// DeviceAuthID and UserCode are secrets of the login: never logged, never in an
// error, and DeviceAuthID never leaves the daemon.
type DeviceChallenge struct {
	DeviceAuthID    string
	UserCode        string
	VerificationURL string
	// Interval is how long to wait between polls, as the vendor asked.
	Interval time.Duration
}

// DeviceGrant is the result of an approved device login: the authorization
// code and the PKCE verifier the vendor itself chose (not one we generated).
type DeviceGrant struct {
	AuthorizationCode string
	CodeVerifier      string
}

var (
	// ErrDeviceUnsupported: the vendor does not offer the device flow (to this
	// client or at all).
	ErrDeviceUnsupported = errors.New("device login is not supported")
	// ErrDevicePending: the user has not approved the code yet; poll again.
	ErrDevicePending = errors.New("device login is pending")
	// ErrDeviceTransient: a poll failed in a way worth retrying (the request
	// did not complete). A vendor's non-pending HTTP error is not transient.
	ErrDeviceTransient = errors.New("device login request did not complete")
)

// DeviceLogin is a provider's device-code login (RFC 8628 shaped): StartDevice
// obtains a code for the user, PollDevice checks whether it was approved, and
// ExchangeDevice trades the grant for the same ExchangeResult a browser login
// yields. Error text from every method is fixed: vendor response bodies, codes
// and ids never appear in it.
type DeviceLogin interface {
	// StartDevice requests a code. ErrDeviceUnsupported when the vendor says
	// the flow is not enabled.
	StartDevice(ctx context.Context) (DeviceChallenge, error)
	// PollDevice checks one time. ErrDevicePending while the user has not
	// approved; ErrDeviceTransient (wrapped) for a retryable failure (a request
	// that did not complete, 429, 5xx).
	PollDevice(ctx context.Context, ch DeviceChallenge) (DeviceGrant, error)
	// ExchangeDevice trades an approved grant for tokens.
	ExchangeDevice(ctx context.Context, g DeviceGrant) (ExchangeResult, error)
}

var _ DeviceLogin = (*OpenAILogin)(nil)

const (
	// maxDeviceRetries is how many times in a row a transient poll failure is
	// retried before the login fails.
	maxDeviceRetries = 3
	// devicePollRequestTimeout bounds a single poll request.
	devicePollRequestTimeout = 30 * time.Second
)

// loginDevice runs a device-code login for a provider the caller has already
// claimed: it registers the login (method device, so Complete answers
// login_wrong_method), asks the vendor for a code, reports it with a
// deviceCode event, then polls until the user approves, the login expires
// (device_expired) or ctx is done, and persists the exchanged tokens exactly
// like a browser login. It binds no listener. Every error is fixed text: no
// code, id, verifier or vendor body is ever put in one.
func (c *LoginCoordinator) loginDevice(ctx context.Context, provider, label string, opts LoginOptions, dev DeviceLogin, onEvent func(LoginEvent)) (LoginResult, error) {
	loginID, err := newLoginID()
	if err != nil {
		return LoginResult{}, fmt.Errorf("oauth login: %w", err)
	}
	pl := &pendingLogin{provider: provider, method: loginMethodDevice, paste: make(chan callbackResult, 1)}
	c.register(loginID, pl)
	defer c.unregister(loginID)

	failed := func() error {
		return &LoginError{Code: LoginCodeDeviceFailed, Detail: fmt.Sprintf("the %s device login failed", providerDisplayLabel(provider))}
	}

	ch, err := dev.StartDevice(ctx)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return LoginResult{}, fmt.Errorf("oauth login: %w", ctx.Err())
	case errors.Is(err, ErrDeviceUnsupported):
		return LoginResult{}, &LoginError{Code: LoginCodeDeviceUnsupported, Detail: fmt.Sprintf("%s did not enable its device-code login for this client; use the browser login", providerDisplayLabel(provider))}
	default:
		return LoginResult{}, failed()
	}

	expiresAt := time.Now().Add(c.deviceTimeout)
	interval := max(ch.Interval, c.minPoll)
	onEvent(LoginEvent{
		Kind:            LoginEventDeviceCode,
		LoginID:         loginID,
		VerificationURL: ch.VerificationURL,
		UserCode:        ch.UserCode,
		ExpiresAt:       expiresAt,
		IntervalSeconds: int(math.Ceil(interval.Seconds())),
	})

	grant, err := c.pollDevice(ctx, dev, ch, interval, expiresAt, failed)
	if err != nil {
		return LoginResult{}, err
	}
	exchanged, err := dev.ExchangeDevice(ctx, grant)
	if err != nil {
		if ctx.Err() != nil {
			return LoginResult{}, fmt.Errorf("oauth login: %w", ctx.Err())
		}
		return LoginResult{}, failed()
	}
	return c.persist(provider, label, opts, exchanged)
}

// pollDevice waits interval between polls until a poll is approved. A pending
// answer keeps polling; a transient failure is retried up to maxDeviceRetries
// times in a row; any other failure ends the login (failed), and so does
// expiresAt (device_expired) or ctx being done.
func (c *LoginCoordinator) pollDevice(ctx context.Context, dev DeviceLogin, ch DeviceChallenge, interval time.Duration, expiresAt time.Time, failed func() error) (DeviceGrant, error) {
	expiry := time.NewTimer(time.Until(expiresAt))
	defer expiry.Stop()
	expired := func() error {
		return &LoginError{Code: LoginCodeDeviceExpired, Detail: "the device code expired before it was approved"}
	}

	transient := 0
	for {
		wait := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			wait.Stop()
			return DeviceGrant{}, fmt.Errorf("oauth login: %w", ctx.Err())
		case <-expiry.C:
			wait.Stop()
			return DeviceGrant{}, expired()
		case <-wait.C:
		}

		remaining := time.Until(expiresAt)
		if remaining <= 0 {
			return DeviceGrant{}, expired()
		}
		pollCtx, cancel := context.WithTimeout(ctx, min(c.pollTimeout, remaining))
		grant, err := dev.PollDevice(pollCtx, ch)
		cancel()

		switch {
		case err == nil:
			return grant, nil
		case ctx.Err() != nil:
			return DeviceGrant{}, fmt.Errorf("oauth login: %w", ctx.Err())
		case !time.Now().Before(expiresAt):
			return DeviceGrant{}, expired()
		case errors.Is(err, ErrDevicePending):
			transient = 0
		case errors.Is(err, ErrDeviceTransient), errors.Is(err, context.DeadlineExceeded):
			// A deadline here is the per-poll timeout: the caller's own ctx
			// and the login's expiry were ruled out above.
			if transient++; transient > maxDeviceRetries {
				return DeviceGrant{}, failed()
			}
		default:
			return DeviceGrant{}, failed()
		}
	}
}

// postDeviceJSON POSTs payload as JSON to endpoint and returns the status and
// a size-bounded body. A transport failure is reported as ErrDeviceTransient
// (or ctx's error when the caller gave up); the underlying error is dropped.
func (l *OpenAILogin) postDeviceJSON(ctx context.Context, endpoint string, payload any) (int, []byte, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, errors.New("openai device login: encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return 0, nil, errors.New("openai device login: build request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		return 0, nil, fmt.Errorf("openai device login: %w", ErrDeviceTransient)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDeviceResponseBytes))
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		return 0, nil, fmt.Errorf("openai device login: %w", ErrDeviceTransient)
	}
	return resp.StatusCode, body, nil
}

func statusOK(status int) bool { return status >= 200 && status < 300 }

// StartDevice implements DeviceLogin.
func (l *OpenAILogin) StartDevice(ctx context.Context) (DeviceChallenge, error) {
	status, body, err := l.postDeviceJSON(ctx, openaiDeviceUserCodeURL, map[string]string{"client_id": openaiClientID})
	if err != nil {
		return DeviceChallenge{}, err
	}
	if status == http.StatusNotFound {
		return DeviceChallenge{}, ErrDeviceUnsupported
	}
	if !statusOK(status) {
		return DeviceChallenge{}, fmt.Errorf("openai device login: the code request returned status %d", status)
	}

	var resp struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return DeviceChallenge{}, errors.New("openai device login: the code response was not understood")
	}
	userCode := strings.TrimSpace(resp.UserCode)
	if userCode == "" {
		userCode = strings.TrimSpace(resp.UserCodeAlt)
	}
	deviceAuthID := strings.TrimSpace(resp.DeviceAuthID)
	if userCode == "" || deviceAuthID == "" {
		return DeviceChallenge{}, errors.New("openai device login: the code response was incomplete")
	}
	return DeviceChallenge{
		DeviceAuthID:    deviceAuthID,
		UserCode:        userCode,
		VerificationURL: openaiDeviceVerificationURL,
		Interval:        parseDeviceInterval(resp.Interval, l.minDeviceInterval()),
	}, nil
}

// parseDeviceInterval reads the vendor's poll interval, a number of seconds
// sent either as a JSON number or a string. A missing, unparsable or
// non-positive value is the 5s default; anything else is clamped to
// [floor, 30s], the floor being 1s outside tests.
func parseDeviceInterval(raw json.RawMessage, floor time.Duration) time.Duration {
	var seconds float64
	var asString string
	switch {
	case len(raw) == 0:
		return deviceDefaultInterval
	case json.Unmarshal(raw, &seconds) == nil:
	case json.Unmarshal(raw, &asString) == nil:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(asString), 64)
		if err != nil {
			return deviceDefaultInterval
		}
		seconds = parsed
	default:
		return deviceDefaultInterval
	}
	switch {
	case math.IsNaN(seconds) || seconds <= 0:
		return deviceDefaultInterval
	case seconds >= deviceMaxInterval.Seconds():
		return deviceMaxInterval
	}
	return max(time.Duration(seconds*float64(time.Second)), floor)
}

// minDeviceInterval is the floor of the vendor's poll interval: deviceMinInterval
// unless a test lowered it on the login.
func (l *OpenAILogin) minDeviceInterval() time.Duration {
	if l.deviceMinInterval > 0 {
		return l.deviceMinInterval
	}
	return deviceMinInterval
}

// PollDevice implements DeviceLogin. The vendor answers 403 or 404 until the
// user approves the code. A 429 or 5xx is ErrDeviceTransient; any other
// non-2xx is a hard failure. A request cut off by ctx's own deadline returns
// that context error; the coordinator, which owns the per-poll deadline,
// treats it as transient.
func (l *OpenAILogin) PollDevice(ctx context.Context, ch DeviceChallenge) (DeviceGrant, error) {
	status, body, err := l.postDeviceJSON(ctx, openaiDeviceTokenURL, map[string]string{
		"device_auth_id": ch.DeviceAuthID,
		"user_code":      ch.UserCode,
	})
	if err != nil {
		return DeviceGrant{}, err
	}
	if status == http.StatusForbidden || status == http.StatusNotFound {
		return DeviceGrant{}, ErrDevicePending
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		// The vendor is rate limiting or unwell, not refusing the login.
		return DeviceGrant{}, fmt.Errorf("openai device login: the approval check returned status %d: %w", status, ErrDeviceTransient)
	}
	if !statusOK(status) {
		return DeviceGrant{}, fmt.Errorf("openai device login: the approval check returned status %d", status)
	}

	var resp struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return DeviceGrant{}, errors.New("openai device login: the approval response was not understood")
	}
	grant := DeviceGrant{
		AuthorizationCode: strings.TrimSpace(resp.AuthorizationCode),
		CodeVerifier:      strings.TrimSpace(resp.CodeVerifier),
	}
	if grant.AuthorizationCode == "" || grant.CodeVerifier == "" {
		return DeviceGrant{}, errors.New("openai device login: the approval response was incomplete")
	}
	return grant, nil
}

// ExchangeDevice implements DeviceLogin: the browser path's token exchange with
// the device redirect_uri and the vendor-provided verifier.
func (l *OpenAILogin) ExchangeDevice(ctx context.Context, g DeviceGrant) (ExchangeResult, error) {
	status, body, err := l.postTokenForm(ctx, g.AuthorizationCode, openaiDeviceRedirectURI, g.CodeVerifier)
	if err != nil {
		if ctx.Err() != nil {
			return ExchangeResult{}, ctx.Err()
		}
		return ExchangeResult{}, errors.New("openai device login: the token exchange request failed")
	}
	if status != http.StatusOK {
		return ExchangeResult{}, fmt.Errorf("openai device login: the token exchange returned status %d", status)
	}
	res, err := parseOpenAIExchange(body)
	if err != nil {
		return ExchangeResult{}, errors.New("openai device login: the token exchange response was not understood")
	}
	return res, nil
}
