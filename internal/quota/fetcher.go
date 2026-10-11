package quota

import (
	"context"
	"errors"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/oauth2"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/transport"
)

// errRefresh is the fixed Report.Error for a transient token-refresh failure.
const errRefresh = "token refresh failed"

// ReportFetcher retrieves an account's plan usage from its provider.
//
// Fetch returns a Report for every explainable outcome: available, or
// unavailable with a Problem (expired, rejected, no_quota). For a transient
// failure (network, timeout, 5xx, 429, unparseable body) it returns the
// matching error-status Report and a non-nil *FetchError (check with
// errors.As); a 429's error carries RetryAfter. Only a cancelled context
// returns a zero Report and ctx.Err(). Fetch performs no retry of its own.
//
// It replaces the legacy Fetcher/Usage pair, which PR-A2 removes together
// with the Poller rewrite.
type ReportFetcher interface {
	Fetch(ctx context.Context, account accounts.Account) (Report, error)
}

// CredentialSource hands out a fresh OAuth credential for an account,
// refreshing and persisting it if it is expired. *accounts.Registry
// implements it.
type CredentialSource interface {
	EnsureFresh(ctx context.Context, id int64, refresher accounts.OAuthRefresher) (accounts.Account, error)
}

// usageSource calls one provider's usage endpoint with an access token and
// parses the answer. It has the same return contract as ReportFetcher.Fetch,
// minus the credential handling. Implementations never log or return the
// token.
type usageSource interface {
	fetch(ctx context.Context, accessToken string) (Report, error)
}

// providerSpec is what the dispatcher needs for one provider's OAuth
// accounts: the usage endpoint and the refresher for an expired token.
type providerSpec struct {
	source    usageSource
	refresher accounts.OAuthRefresher
}

// expiryBuffer mirrors accounts' own 30 s refresh buffer: a token this close
// to expiry is refreshed before use rather than risking a mid-request expiry.
const expiryBuffer = 30 * time.Second

// AccountFetcher is the ReportFetcher for smind accounts. It dispatches on
// provider and credential type:
//
//   - anthropic/oauth and openai/oauth call the provider's usage endpoint,
//   - any api_key account, any other provider, or an account with no usable
//     credential is unavailable/no_quota and makes zero HTTP requests.
//
// It uses only smind's own stored credential, only the two fixed provider
// hosts, and refreshes an expired token once via CredentialSource.EnsureFresh.
// A 401/403 is never retried with a forced refresh.
type AccountFetcher struct {
	creds     CredentialSource
	providers map[string]providerSpec
	now       func() time.Time
}

var _ ReportFetcher = (*AccountFetcher)(nil)

// newAccountFetcher is the test seam: specs and clock are injectable here,
// and only here (neither is reachable from config).
func newAccountFetcher(creds CredentialSource, providers map[string]providerSpec, now func() time.Time) *AccountFetcher {
	if now == nil {
		now = time.Now
	}
	return &AccountFetcher{creds: creds, providers: providers, now: now}
}

// NewAccountFetcher returns the production fetcher: the two fixed provider
// endpoints over the same uTLS clients the proxy uses (Firefox for Anthropic,
// Chrome for OpenAI, both behind bot detection) and the providers' own
// refreshers. It makes no request until Fetch is called.
func NewAccountFetcher(creds CredentialSource) *AccountFetcher {
	return newAccountFetcher(creds, map[string]providerSpec{
		"anthropic": {
			source:    newAnthropicSource(transport.Client(utls.HelloFirefox_Auto)),
			refresher: accounts.NewAnthropicRefresher(),
		},
		"openai": {
			source:    newOpenAISource(transport.Client(utls.HelloChrome_Auto)),
			refresher: accounts.NewOpenAIRefresher(),
		},
	}, nil)
}

// Fetch implements ReportFetcher.
func (f *AccountFetcher) Fetch(ctx context.Context, account accounts.Account) (Report, error) {
	now := f.now()

	if account.CredentialType == accounts.CredentialTypeAPIKey {
		return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailAPIKey}, now), nil
	}
	spec, ok := f.providers[account.Provider]
	if !ok {
		return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailUnsupport}, now), nil
	}
	if account.CredentialType != accounts.CredentialTypeOAuth || account.OAuth == nil {
		return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailNoCredData}, now), nil
	}

	cred := account.OAuth
	// An unknown expiry (zero) is used as is: refreshing on every fetch would
	// burn a rotating refresh token, and a dead token answers 401 anyway.
	if !cred.ExpiresAt.IsZero() && !now.Add(expiryBuffer).Before(cred.ExpiresAt) {
		expiredAt := cred.ExpiresAt
		fresh, err := f.creds.EnsureFresh(ctx, account.ID, spec.refresher)
		switch {
		case err == nil:
			if fresh.OAuth == nil {
				return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailNoCredData}, now), nil
			}
			cred = fresh.OAuth
		case ctx.Err() != nil:
			return Report{}, ctx.Err()
		case now.Before(expiredAt):
			// Only inside the pre-expiry buffer: the current token still
			// works, so use it rather than reporting the login as expired.
		case isTerminalRefreshError(err):
			// The error text is not copied: it can carry provider response
			// text. A terminal refusal is "login expired".
			t := expiredAt.UTC()
			return unavailableReport(Problem{Kind: ProblemExpired, ExpiresAt: &t, Detail: detailExpired}, now), nil
		default:
			// Network, timeout, 5xx, 429: transient. The poller backs off.
			return errorReport(errRefresh, now), &FetchError{msg: errRefresh}
		}
	}
	if cred.AccessToken == "" {
		return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailNoCredData}, now), nil
	}
	return spec.source.fetch(ctx, cred.AccessToken)
}

// terminalRefreshMarkers are substrings of a refresher's error text that mean
// the refresh token itself is dead (the hand-written refreshers embed the
// provider's response body in the error). Used for classification only; the
// text is never copied out.
var terminalRefreshMarkers = []string{
	"invalid_grant",
	"refresh_token_expired",
	"refresh_token_reused",
	"refresh_token_invalidated",
}

// isTerminalRefreshError reports whether err is the provider refusing the
// refresh token for good, as opposed to a transient failure.
func isTerminalRefreshError(err error) bool {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
		return true
	}
	msg := err.Error()
	for _, m := range terminalRefreshMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
