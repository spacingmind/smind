package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// anthropicUsageURL is Anthropic's OAuth usage endpoint. It is a constant:
// the only injection point is the source's unexported url/client fields,
// which tests set (never config).
const anthropicUsageURL = "https://api.anthropic.com/api/oauth/usage"

// anthropicOAuthBeta is the beta header the usage endpoint requires.
const anthropicOAuthBeta = "oauth-2025-04-20"

// anthropicSource fetches plan usage from Anthropic's OAuth usage endpoint.
type anthropicSource struct {
	client *http.Client
	url    string
	now    func() time.Time
}

var _ usageSource = (*anthropicSource)(nil)

// newAnthropicSource derives a usage client from base (same transport, 15 s
// timeout, redirects never followed).
func newAnthropicSource(base *http.Client) *anthropicSource {
	return &anthropicSource{client: newUsageClient(base), url: anthropicUsageURL, now: time.Now}
}

// fetch implements usageSource: one GET of the usage endpoint with the OAuth
// bearer token, parsed into a Report. The token never reaches an error, a
// Report or a log.
func (s *anthropicSource) fetch(ctx context.Context, accessToken string) (Report, error) {
	now := s.now()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+accessToken)
	header.Set("anthropic-beta", anthropicOAuthBeta)
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

	report, ok := parseAnthropicUsage(body, now)
	if !ok {
		return malformedResult(now)
	}
	return report, nil
}

// anthropicWindow is one top-level utilization window.
type anthropicWindow struct {
	utilization lenientFloat
	resetsAt    *string
}

// anthropicScopeLabel names the model or surface a scoped limit applies to.
type anthropicScopeLabel struct {
	ID          *string `json:"id"`
	DisplayName *string `json:"display_name"`
}

// anthropicLimitEntry is one entry of the response's limits array.
type anthropicLimitEntry struct {
	Kind     string `json:"kind"`
	Percent  lenientFloat
	ResetsAt *string `json:"resets_at"`
	Scope    *struct {
		Model   *anthropicScopeLabel `json:"model"`
		Surface *anthropicScopeLabel `json:"surface"`
	} `json:"scope"`
}

// anthropicScopedLimit is one model- or surface-scoped weekly limit,
// normalized away from whichever shape of the response described it (a legacy
// top-level seven_day_<model> key or a limits[] entry). The API describes the
// same limit both ways during a migration; reconcileAnthropicScopedLimits
// merges the two shapes exactly once.
type anthropicScopedLimit struct {
	dimension ScopeKind // ScopeModel or ScopeSurface
	id        string    // provider's own id, "" when absent
	name      string    // display name, or the id when no display name
	usedPct   *float64
	resetsAt  *time.Time
}

// parseAnthropicUsage parses a usage response body. ok is false when the body
// is not a usable usage response (HTML, not a JSON object, no windows at
// all); the caller reports that as a malformed result.
func parseAnthropicUsage(body []byte, now time.Time) (Report, bool) {
	if looksLikeHTML(body) {
		return Report{}, false
	}
	// Each entry decodes independently so one malformed section cannot take
	// down the windows that already parsed.
	var top struct {
		FiveHour         json.RawMessage `json:"five_hour"`
		SevenDay         json.RawMessage `json:"seven_day"`
		SevenDayOpus     json.RawMessage `json:"seven_day_opus"`
		SevenDayOmelette json.RawMessage `json:"seven_day_omelette"`
		Limits           json.RawMessage `json:"limits"`
		ExtraUsage       json.RawMessage `json:"extra_usage"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return Report{}, false
	}

	report := Report{Version: ReportVersion, Status: StatusAvailable, FetchedAt: now.UTC()}

	if w, ok := anthropicDecodeWindow(top.FiveHour); ok {
		report.Windows = append(report.Windows, newWindow("five_hour", "Session", "5h", true, w.utilization.ptr(), parseRFC3339(w.resetsAt)))
	}
	if w, ok := anthropicDecodeWindow(top.SevenDay); ok {
		report.Windows = append(report.Windows, newWindow("weekly", "Weekly", "wk", true, w.utilization.ptr(), parseRFC3339(w.resetsAt)))
	}

	scoped := reconcileAnthropicScopedLimits(
		anthropicLegacyScopedLimits(top.SevenDayOpus, top.SevenDayOmelette),
		anthropicScopedLimitsFromArray(top.Limits),
	)
	// Window ids are React keys: unique within a response (an id equal to
	// another entry's normalized name is suffixed, not dropped) and stable
	// across refreshes.
	taken := map[string]bool{"five_hour": true, "weekly": true}
	for _, limit := range scoped {
		w := anthropicScopedWindow(limit)
		for suffix := 2; taken[w.ID]; suffix++ {
			w.ID = anthropicScopedWindowID(limit) + "_" + strconv.Itoa(suffix)
		}
		taken[w.ID] = true
		report.Windows = append(report.Windows, w)
	}
	if len(report.Windows) == 0 {
		return Report{}, false
	}

	if enabled := anthropicExtraUsage(top.ExtraUsage); enabled != nil {
		report.OverageEnabled = enabled
		value := "Disabled"
		if *enabled {
			value = "Enabled"
		}
		report.Details = append(report.Details, Detail{ID: "extra_usage", Label: "Extra usage", Value: value})
	}
	return report, true
}

// anthropicDecodeWindow decodes one raw window entry; ok is false when the
// entry is absent, null or malformed.
func anthropicDecodeWindow(raw json.RawMessage) (anthropicWindow, bool) {
	if !anthropicRawPresent(raw) {
		return anthropicWindow{}, false
	}
	var w struct {
		Utilization lenientFloat `json:"utilization"`
		ResetsAt    *string      `json:"resets_at"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return anthropicWindow{}, false
	}
	return anthropicWindow{utilization: w.Utilization, resetsAt: w.ResetsAt}, true
}

// anthropicLegacyScopedLimits reads the pre-limits[] scoped windows. The keys
// are model-scoped by definition, which is what stops a surface named
// "Omelette" from being mistaken for the legacy Omelette model window.
func anthropicLegacyScopedLimits(opus, omelette json.RawMessage) []anthropicScopedLimit {
	specs := []struct {
		raw  json.RawMessage
		name string
	}{{opus, "Opus"}, {omelette, "Omelette"}}
	var limits []anthropicScopedLimit
	for _, spec := range specs {
		w, ok := anthropicDecodeWindow(spec.raw)
		if !ok {
			continue
		}
		limits = append(limits, anthropicScopedLimit{
			dimension: ScopeModel,
			name:      spec.name,
			usedPct:   w.utilization.ptr(),
			resetsAt:  parseRFC3339(w.resetsAt),
		})
	}
	return limits
}

// anthropicScopedLimitsFromArray reads the scoped weekly limits from limits[].
// Entries are validated one at a time so a single malformed or newly-shaped
// entry cannot take down the rest; entries of other kinds and entries that
// name nothing renderable are skipped.
func anthropicScopedLimitsFromArray(raw json.RawMessage) []anthropicScopedLimit {
	if !anthropicRawPresent(raw) {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	var limits []anthropicScopedLimit
	for _, entry := range entries {
		var e anthropicLimitEntry
		if err := json.Unmarshal(entry, &e); err != nil || e.Kind != "weekly_scoped" {
			continue
		}
		limit, ok := anthropicScopedLimitFromEntry(e)
		if !ok {
			continue
		}
		limits = append(limits, limit)
	}
	return limits
}

// anthropicScopedLimitFromEntry resolves a limits[] entry's scope; ok is
// false when it names neither a model nor a surface with an id or display
// name.
func anthropicScopedLimitFromEntry(e anthropicLimitEntry) (anthropicScopedLimit, bool) {
	if e.Scope == nil {
		return anthropicScopedLimit{}, false
	}
	dimensions := []struct {
		label     *anthropicScopeLabel
		dimension ScopeKind
	}{{e.Scope.Model, ScopeModel}, {e.Scope.Surface, ScopeSurface}}
	for _, d := range dimensions {
		if d.label == nil {
			continue
		}
		id := strings.TrimSpace(anthropicDerefOrEmpty(d.label.ID))
		name := strings.TrimSpace(anthropicDerefOrEmpty(d.label.DisplayName))
		if name == "" {
			name = id
		}
		if name == "" {
			continue
		}
		return anthropicScopedLimit{
			dimension: d.dimension,
			id:        id,
			name:      name,
			usedPct:   e.Percent.ptr(),
			resetsAt:  parseRFC3339(e.ResetsAt),
		}, true
	}
	return anthropicScopedLimit{}, false
}

func anthropicDerefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// anthropicIsSameLimit is the single definition of identity for scoped
// limits: different dimensions are never the same limit; when both sides
// carry the API's own id that id decides (so "fable-pro" and "fable_pro"
// stay apart); otherwise the normalized name is the only link between a
// legacy key (never has an id) and its limits[] counterpart.
func anthropicIsSameLimit(a, b anthropicScopedLimit) bool {
	if a.dimension != b.dimension {
		return false
	}
	if a.id != "" && b.id != "" {
		return a.id == b.id
	}
	return anthropicNormalizeName(a.name) == anthropicNormalizeName(b.name)
}

// anthropicNormalizeName folds a name down to the characters an id built from
// a name is allowed to carry.
func anthropicNormalizeName(name string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			sep = false
			b.WriteRune(r)
		} else {
			sep = true
		}
	}
	return b.String()
}

// reconcileAnthropicScopedLimits merges the legacy and limits[] descriptions
// into one limit per identity. A limits[] entry wins on identity (that is the
// shape the API is migrating towards), but its values are nullable, so each
// field falls back to the legacy twin.
func reconcileAnthropicScopedLimits(legacy, fromLimitsArray []anthropicScopedLimit) []anthropicScopedLimit {
	reconciled := append([]anthropicScopedLimit(nil), legacy...)
	for _, limit := range fromLimitsArray {
		index := -1
		for i, candidate := range reconciled {
			if anthropicIsSameLimit(candidate, limit) {
				index = i
				break
			}
		}
		if index == -1 {
			reconciled = append(reconciled, limit)
			continue
		}
		twin := reconciled[index]
		merged := limit
		if merged.usedPct == nil {
			merged.usedPct = twin.usedPct
		}
		if merged.resetsAt == nil {
			merged.resetsAt = twin.resetsAt
		}
		reconciled[index] = merged
	}
	return reconciled
}

// anthropicScopedWindowID builds a scoped window's id: the provider's own id
// verbatim (normalizing it would collapse "fable-pro" and "fable_pro"), else
// the normalized name.
func anthropicScopedWindowID(limit anthropicScopedLimit) string {
	id := "weekly_" + string(limit.dimension) + "_"
	if limit.id != "" {
		return id + limit.id
	}
	return id + anthropicNormalizeName(limit.name)
}

// anthropicScopedWindow renders one scoped limit. Emitted even at 0% with a
// nil UsedPct: a zero bar answers "how much of this model have I used" and
// must not come and go between refreshes.
func anthropicScopedWindow(limit anthropicScopedLimit) Window {
	scope := &Scope{Kind: limit.dimension, ID: limit.id, Name: limit.name}
	w := newWindow(anthropicScopedWindowID(limit), "Weekly · "+limit.name, "wk "+limit.name, false, limit.usedPct, limit.resetsAt)
	w.Scope = scope
	return w
}

// anthropicExtraUsage reads extra_usage.is_enabled; nil when absent.
func anthropicExtraUsage(raw json.RawMessage) *bool {
	if !anthropicRawPresent(raw) {
		return nil
	}
	var extra struct {
		IsEnabled *bool `json:"is_enabled"`
	}
	if err := json.Unmarshal(raw, &extra); err != nil {
		return nil
	}
	return extra.IsEnabled
}

func anthropicRawPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
