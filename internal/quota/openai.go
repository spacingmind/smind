package quota

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// openaiUsageURL is ChatGPT's plan usage endpoint for OAuth (Codex) logins.
const openaiUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// openaiUserAgent mirrors the Codex CLI's browser-like UA; the endpoint
// rejects plain client UAs.
const openaiUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"

// openaiAuthClaim is the JWT claim namespace OpenAI scopes auth metadata to.
const openaiAuthClaim = "https://api.openai.com/auth"

// openaiSource fetches plan usage from ChatGPT's usage endpoint with an
// OAuth access token. The token is used for one request and never logged,
// returned or stored; the ChatGPT account id from its JWT payload is used
// as a request header only.
type openaiSource struct {
	client *http.Client
	url    string
	now    func() time.Time
}

var _ usageSource = (*openaiSource)(nil)

// newOpenAISource builds the OpenAI usage source. The client wraps base
// (same transport, 15 s timeout, redirects not followed).
func newOpenAISource(base *http.Client) *openaiSource {
	return &openaiSource{
		client: newUsageClient(base),
		url:    openaiUsageURL,
		now:    time.Now,
	}
}

// fetch implements usageSource: one GET of the usage endpoint, an
// unavailable/rejected report, or a transient failure as *FetchError.
func (s *openaiSource) fetch(ctx context.Context, accessToken string) (Report, error) {
	now := s.now()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+accessToken)
	header.Set("Accept", "application/json")
	header.Set("User-Agent", openaiUserAgent)
	if id := openaiAccountID(accessToken); id != "" {
		header.Set("ChatGPT-Account-Id", id)
	}

	body, rejected, err := getBody(ctx, s.client, s.url, header, s.now)
	if err != nil {
		if ctx.Err() != nil {
			return Report{}, ctx.Err()
		}
		var fe *FetchError
		if errors.As(err, &fe) {
			return failureResult(fe, now)
		}
		return failureResult(&FetchError{msg: errNetwork}, now)
	}
	if rejected != 0 {
		return unavailableReport(Problem{Kind: ProblemRejected, Status: rejected, Detail: detailRejected}, now), nil
	}

	r, ok := parseOpenAIUsage(body, now)
	if !ok {
		return malformedResult(now)
	}
	return r, nil
}

// openaiAccountID extracts chatgpt_account_id from the token's JWT payload,
// preferring the https://api.openai.com/auth claim object over a top-level
// claim. The payload is decoded locally (no verification); any garbage or
// missing claim gives "".
func openaiAccountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims map[string]json.RawMessage
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	if auth, ok := claims[openaiAuthClaim]; ok {
		var authObj map[string]json.RawMessage
		if json.Unmarshal(auth, &authObj) == nil {
			if id := openaiClaimString(authObj, "chatgpt_account_id"); id != "" {
				return id
			}
		}
	}
	return openaiClaimString(claims, "chatgpt_account_id")
}

func openaiClaimString(claims map[string]json.RawMessage, name string) string {
	raw, ok := claims[name]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" {
		return ""
	}
	return s
}

// openaiWindowJSON is one slot (primary/secondary) of a rate limit group.
// Unknown fields (including email) are dropped by not being decoded.
type openaiWindowJSON struct {
	UsedPercent        lenientFloat    `json:"used_percent"`
	ResetAt            json.RawMessage `json:"reset_at"`
	LimitWindowSeconds lenientFloat    `json:"limit_window_seconds"`
}

// openaiRateLimitJSON is one rate limit group; each window is kept raw so a
// malformed one is skipped without losing its sibling.
type openaiRateLimitJSON struct {
	Primary   *json.RawMessage `json:"primary_window"`
	Secondary *json.RawMessage `json:"secondary_window"`
}

// parseOpenAIUsage parses a usage response body into an available Report.
// ok is false when the body is not a usable usage response: HTML, not a
// JSON object, or no windows at all.
func parseOpenAIUsage(body []byte, now time.Time) (Report, bool) {
	if looksLikeHTML(body) {
		return Report{}, false
	}
	var top struct {
		PlanType            string            `json:"plan_type"`
		RateLimit           json.RawMessage   `json:"rate_limit"`
		AdditionalRateLimit []json.RawMessage `json:"additional_rate_limits"`
		CodeReviewRateLimit json.RawMessage   `json:"code_review_rate_limit"`
		Credits             json.RawMessage   `json:"credits"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return Report{}, false
	}

	r := Report{
		Version:   ReportVersion,
		Status:    StatusAvailable,
		PlanLabel: strings.TrimSpace(top.PlanType),
		FetchedAt: now.UTC(),
	}

	openaiAppendRateLimit(&r, top.RateLimit, nil, true)
	for _, entry := range top.AdditionalRateLimit {
		var e struct {
			LimitName      string          `json:"limit_name"`
			MeteredFeature string          `json:"metered_feature"`
			RateLimit      json.RawMessage `json:"rate_limit"`
		}
		if json.Unmarshal(entry, &e) != nil {
			continue // malformed entry: skipped, siblings survive
		}
		identity := e.MeteredFeature
		if identity == "" {
			identity = e.LimitName
		}
		if identity == "" {
			continue // an unnamed limit has no stable quota identity
		}
		name := e.LimitName
		if name == "" {
			name = identity
		}
		scope := &Scope{
			Kind: ScopeFeature,
			ID:   "limit:" + openaiEscapeIdentity(identity),
			Name: name,
		}
		openaiAppendRateLimit(&r, e.RateLimit, scope, false)
	}
	openaiAppendRateLimit(&r, top.CodeReviewRateLimit, &Scope{
		Kind: ScopeFeature,
		ID:   "code_review",
		Name: "Code review",
	}, false)

	if len(r.Windows) == 0 {
		return Report{}, false
	}

	if len(top.Credits) > 0 {
		var credits struct {
			Balance lenientFloat `json:"balance"`
		}
		if json.Unmarshal(top.Credits, &credits) == nil {
			if v := credits.Balance.ptr(); v != nil {
				r.Balances = append(r.Balances, Balance{
					ID:        "credits",
					Label:     "Credits",
					Remaining: v,
					Unit:      "credits",
				})
			}
		}
	}
	return r, true
}

// openaiAppendRateLimit decodes one rate limit group into windows on r. A
// malformed group or window is skipped; account-level windows (scope nil)
// are summary windows.
func openaiAppendRateLimit(r *Report, raw json.RawMessage, scope *Scope, summary bool) {
	if len(raw) == 0 {
		return
	}
	var group openaiRateLimitJSON
	if json.Unmarshal(raw, &group) != nil {
		return
	}
	for i, slot := range []*json.RawMessage{group.Primary, group.Secondary} {
		if slot == nil || len(*slot) == 0 {
			continue
		}
		var wj openaiWindowJSON
		if json.Unmarshal(*slot, &wj) != nil {
			continue
		}
		nameID, label, short := "unknown_secondary", "Secondary limit", ""
		if i == 0 {
			nameID, label = "unknown_primary", "Primary limit"
		}
		var durationSeconds *int64
		if d := wj.LimitWindowSeconds.ptr(); d != nil && *d > 0 {
			secs := int64(*d)
			nameID, label, short = openaiDurationName(secs)
			durationSeconds = &secs
		}
		id, winLabel, winShort := nameID, label, short
		if scope != nil {
			id = scope.ID + ":" + nameID
			winLabel = scope.Name + " · " + label
			winShort = strings.TrimSpace(scope.Name + " " + short)
		}
		w := newWindow(id, winLabel, winShort, summary, wj.UsedPercent.ptr(), openaiEpochTime(wj.ResetAt))
		w.Scope = scope
		w.DurationSeconds = durationSeconds
		r.Windows = append(r.Windows, w)
	}
}

// openaiDurationName names a window from its length in seconds: the two
// durations Codex actually reports get stable ids, anything else is
// "<n>s" with a label from the largest unit that divides it evenly.
func openaiDurationName(seconds int64) (id, label, short string) {
	switch seconds {
	case 604800:
		return "weekly", "Weekly", "wk"
	case 18000:
		return "five_hour", "Session", "5h"
	default:
		id = strconv.FormatInt(seconds, 10) + "s"
	}
	units := []struct {
		size   int64
		name   string
		letter string
	}{
		{86400, "day", "d"},
		{3600, "hour", "h"},
		{60, "minute", "m"},
		{1, "second", "s"},
	}
	unit := units[len(units)-1]
	for _, candidate := range units {
		if seconds%candidate.size == 0 {
			unit = candidate
			break
		}
	}
	amount := seconds / unit.size
	return id, fmt.Sprintf("%d-%s", amount, unit.name), fmt.Sprintf("%d%s", amount, unit.letter)
}

// openaiEpochTime converts reset_at — epoch seconds as a number or a
// numeric string — to UTC; anything else gives nil.
func openaiEpochTime(raw json.RawMessage) *time.Time {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return nil
		}
		s = strings.TrimSpace(str)
	}
	secs, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	t := time.Unix(int64(secs), 0).UTC()
	return &t
}

// openaiEscapeIdentity matches JS encodeURIComponent for the scope id:
// net/url's QueryEscape, except spaces become %20 instead of +.
func openaiEscapeIdentity(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
