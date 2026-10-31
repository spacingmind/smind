package quota

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// detailNoPlan is the Problem.Detail for a Z.AI account without a coding plan.
const detailNoPlan = "no coding plan"

// zaiQuotaPath is the Z.AI plan-usage endpoint path, the same on every host.
const zaiQuotaPath = "/api/monitor/usage/quota/limit"

// zaiHosts are the only hosts a quota URL may be derived for.
var zaiHosts = map[string]bool{
	"api.z.ai":         true,
	"open.bigmodel.cn": true,
	"bigmodel.cn":      true,
}

// ZaiQuotaURL derives the quota endpoint from a Z.AI Anthropic-compatible base
// URL. Only the known Z.AI hosts over https are accepted, so an account's
// configured base URL can never point the usage call (and the API key it
// carries) at another host.
func ZaiQuotaURL(anthropicBaseURL string) (string, bool) {
	u, err := url.Parse(anthropicBaseURL)
	if err != nil || u.Scheme != "https" || !zaiHosts[u.Host] {
		return "", false
	}
	return "https://" + u.Host + zaiQuotaPath, true
}

// zaiSource fetches plan usage from Z.AI's quota/limit endpoint.
type zaiSource struct {
	client *http.Client
	url    string
	now    func() time.Time
}

var _ usageSource = (*zaiSource)(nil)

// newZaiSource derives a usage client from base (same transport, 15 s timeout,
// redirects never followed).
func newZaiSource(base *http.Client, quotaURL string) *zaiSource {
	return &zaiSource{client: newUsageClient(base), url: quotaURL, now: time.Now}
}

// FetchZai fetches one Z.AI plan-usage report with apiKey.
func FetchZai(ctx context.Context, base *http.Client, quotaURL, apiKey string) (Report, error) {
	return newZaiSource(base, quotaURL).fetch(ctx, apiKey)
}

// fetch implements usageSource: one GET with the API key as the raw
// Authorization value (Z.AI takes no "Bearer " prefix). The key never reaches
// an error, a Report or a log.
func (s *zaiSource) fetch(ctx context.Context, apiKey string) (Report, error) {
	now := s.now()

	header := http.Header{}
	header.Set("Authorization", apiKey)
	header.Set("Accept", "application/json")

	body, rejected, err := getBody(ctx, s.client, s.url, header, s.now)
	if err != nil {
		var fetchErr *FetchError
		if errors.As(err, &fetchErr) {
			return failureResult(fetchErr, now)
		}
		return Report{}, err // cancelled context
	}
	if rejected != 0 {
		return unavailableReport(Problem{Kind: ProblemRejected, Status: rejected, Detail: detailRejected}, now), nil
	}

	report, ok := parseZaiUsage(body, now)
	if !ok {
		return malformedResult(now)
	}
	return report, nil
}

// zaiLimit is one entry of data.limits; every field is optional so one odd
// entry never drops the rest.
type zaiLimit struct {
	Type          string   `json:"type"`
	Unit          *int     `json:"unit"`
	Number        *int     `json:"number"`
	Percentage    *float64 `json:"percentage"`
	NextResetTime *int64   `json:"nextResetTime"`
	Usage         *float64 `json:"usage"`
	CurrentValue  *float64 `json:"currentValue"`
	Remaining     *float64 `json:"remaining"`
}

func (l zaiLimit) is(typ string, unit, number int) bool {
	return l.Type == typ && l.Unit != nil && *l.Unit == unit && l.Number != nil && *l.Number == number
}

// parseZaiUsage parses a quota/limit response. ok is false when the body is
// not a usable response (not JSON, a business error, no data); the caller
// reports that as malformed. Limits are identified by type+unit+number, never
// by array index.
func parseZaiUsage(body []byte, now time.Time) (Report, bool) {
	var top struct {
		Code *int `json:"code"`
		Data *struct {
			Level  string            `json:"level"`
			Limits []json.RawMessage `json:"limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return Report{}, false
	}
	if top.Code == nil || *top.Code != 200 || top.Data == nil {
		return Report{}, false
	}

	report := Report{Version: ReportVersion, Status: StatusAvailable, PlanLabel: top.Data.Level, FetchedAt: now.UTC()}

	var fiveHour, weekly *Window
	for _, raw := range top.Data.Limits {
		var l zaiLimit
		if err := json.Unmarshal(raw, &l); err != nil {
			continue
		}
		switch {
		case l.is("TOKENS_LIMIT", 3, 5):
			w := zaiWindow("five_hour", "5-hour", "5h", 18000, l)
			fiveHour = &w
		case l.Type == "TOKENS_LIMIT" && l.Unit != nil && *l.Unit == 6:
			w := zaiWindow("weekly", "Weekly", "wk", 604800, l)
			weekly = &w
		case l.is("TIME_LIMIT", 5, 1):
			report.Balances = append(report.Balances, Balance{
				ID: "mcp_monthly", Label: "Tool calls (monthly)", Unit: "requests",
				Used: l.CurrentValue, Remaining: l.Remaining, Limit: l.Usage,
			})
		}
	}
	if fiveHour != nil {
		report.Windows = append(report.Windows, *fiveHour)
	}
	if weekly != nil {
		report.Windows = append(report.Windows, *weekly)
	}

	if len(report.Windows) == 0 && len(report.Balances) == 0 && top.Data.Level == "" {
		return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailNoPlan}, now), true
	}
	return report, true
}

// zaiWindow builds a summary window from a TOKENS_LIMIT entry; percentage is
// the used percent.
func zaiWindow(id, label, shortLabel string, durationSeconds int64, l zaiLimit) Window {
	var usedPct *float64
	if l.Percentage != nil {
		p := *l.Percentage
		if p < 0 {
			p = 0
		} else if p > 100 {
			p = 100
		}
		usedPct = &p
	}
	var resetsAt *time.Time
	if l.NextResetTime != nil && *l.NextResetTime > 0 {
		t := time.UnixMilli(*l.NextResetTime).UTC()
		resetsAt = &t
	}
	w := newWindow(id, label, shortLabel, true, usedPct, resetsAt)
	w.DurationSeconds = &durationSeconds
	return w
}
