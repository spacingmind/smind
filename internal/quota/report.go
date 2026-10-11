package quota

import (
	"encoding/json"
	"strings"
	"time"
)

// ReportVersion is written into every stored report. A stored payload without
// "v" is the legacy Usage shape and decodes to "no report".
const ReportVersion = 2

// Status is the overall outcome of a usage fetch.
type Status string

const (
	StatusAvailable   Status = "available"   // windows/balances are meaningful
	StatusUnavailable Status = "unavailable" // expected, explainable: see Problem
	StatusError       Status = "error"       // transient: network, 5xx, 429, parse
)

// ProblemKind says why an account has no usable usage data.
type ProblemKind string

const (
	ProblemExpired  ProblemKind = "expired"  // credential expired and could not be refreshed
	ProblemRejected ProblemKind = "rejected" // provider answered 401/403
	ProblemNoQuota  ProblemKind = "no_quota" // account has no plan windows (API key, unsupported provider)
)

// Fixed vocabulary for Report.Error and Problem.Detail. Nothing a provider
// sends (body, header, message) is ever copied into a Report.
const (
	errMalformed     = "malformed response"
	errTimeout       = "timeout"
	errNetwork       = "network error"
	errRateLimited   = "rate limited"
	errTooLarge      = "response too large"
	detailAPIKey     = "API-key accounts have no plan windows"
	detailUnsupport  = "provider not supported"
	detailExpired    = "login expired"
	detailRejected   = "login rejected"
	detailNoCredData = "account has no usable credential"
)

// Problem explains an unavailable report.
type Problem struct {
	Kind      ProblemKind `json:"kind"`
	Status    int         `json:"status,omitempty"`    // HTTP status for rejected
	ExpiresAt *time.Time  `json:"expiresAt,omitempty"` // for expired: when the login expired
	Detail    string      `json:"detail,omitempty"`    // fixed vocabulary, never a response body
}

// Tone is a coarse severity derived from UsedPct.
type Tone string

const (
	ToneDefault Tone = "default"
	ToneOK      Tone = "ok"
	ToneWarning Tone = "warning"
	ToneDanger  Tone = "danger"
)

// ToneFromUsedPct maps a used percentage to a Tone: nil is default, above 90
// danger, 70 and up warning, otherwise ok.
func ToneFromUsedPct(usedPct *float64) Tone {
	if usedPct == nil {
		return ToneDefault
	}
	switch {
	case *usedPct > 90:
		return ToneDanger
	case *usedPct >= 70:
		return ToneWarning
	default:
		return ToneOK
	}
}

// ScopeKind says what a window is limited to. Only ScopeAccount ("") and
// ScopeModel ever take part in routing.
type ScopeKind string

const (
	ScopeAccount ScopeKind = ""
	ScopeModel   ScopeKind = "model"   // e.g. Anthropic weekly Opus
	ScopeSurface ScopeKind = "surface" // e.g. a product surface; display only
	ScopeFeature ScopeKind = "feature" // e.g. Codex code review; display only
)

// Scope narrows a window to one model, surface or feature.
type Scope struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id,omitempty"` // provider's own id when present
	Name string    `json:"name"`         // display name, also the model matcher token
}

// Window is one rolling usage window (5-hour, weekly, per-model weekly...).
type Window struct {
	ID              string     `json:"id"` // stable across refreshes: five_hour, weekly, weekly_model_opus, ...
	Label           string     `json:"label"`
	ShortLabel      string     `json:"shortLabel,omitempty"`
	Scope           *Scope     `json:"scope,omitempty"`   // nil = account-wide
	Summary         bool       `json:"summary,omitempty"` // shown in the sidebar item by default
	UsedPct         *float64   `json:"usedPct"`           // nil = provider did not say
	RemainingPct    *float64   `json:"remainingPct"`      // max(0, 100-UsedPct)
	ResetsAt        *time.Time `json:"resetsAt"`
	DurationSeconds *int64     `json:"durationSeconds,omitempty"`
	Tone            Tone       `json:"tone"`
}

// AppliesTo reports whether the window limits requests for model. An
// account-wide window always applies; a model-scoped window applies only when
// the lower-cased model id contains the normalized scope name; surface and
// feature windows never apply (they are display-only), and neither does a
// model-scoped window when model is empty.
func (w Window) AppliesTo(model string) bool {
	if w.Scope == nil {
		return true
	}
	if w.Scope.Kind != ScopeModel || model == "" {
		return false
	}
	token := normalizeToken(w.Scope.Name)
	if token == "" {
		return false
	}
	return strings.Contains(normalizeToken(model), token)
}

// normalizeToken lower-cases s and folds every run of non-alphanumeric
// characters into one "-", trimming leading and trailing separators.
func normalizeToken(s string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if sep && b.Len() > 0 {
				b.WriteByte('-')
			}
			sep = false
			b.WriteRune(r)
		} else {
			sep = true
		}
	}
	return b.String()
}

// Balance is a non-window quantity (credits, dollars, requests, tokens).
type Balance struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Used      *float64 `json:"used,omitempty"`
	Remaining *float64 `json:"remaining,omitempty"`
	Limit     *float64 `json:"limit,omitempty"`
	Unit      string   `json:"unit"` // usd | credits | requests | tokens
}

// Detail is one label/value row shown with the report.
type Detail struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// Report is what a fetch learned about one account's plan usage.
type Report struct {
	Version        int       `json:"v"`
	Status         Status    `json:"status"`
	PlanLabel      string    `json:"planLabel,omitempty"` // "plus", "pro"; never an email
	Windows        []Window  `json:"windows,omitempty"`
	Balances       []Balance `json:"balances,omitempty"`
	Details        []Detail  `json:"details,omitempty"`
	OverageEnabled *bool     `json:"overageEnabled,omitempty"` // Anthropic extra_usage.is_enabled
	Problem        *Problem  `json:"problem,omitempty"`
	Error          string    `json:"error,omitempty"` // fixed vocabulary: "timeout", "http 503", "malformed response"
	FetchedAt      time.Time `json:"fetchedAt"`
}

// EncodeReport marshals r for quota_snapshots.usage_data, stamping
// ReportVersion.
func EncodeReport(r Report) (string, error) {
	r.Version = ReportVersion
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeReport parses a quota_snapshots.usage_data payload. ok is false (and
// no error is surfaced) for anything that is not a report this build
// understands: the legacy Usage shape (no "v"), a newer "v", or garbage. The
// next successful fetch overwrites such a row.
func DecodeReport(data string) (r Report, ok bool) {
	var probe struct {
		Version int `json:"v"`
	}
	if err := json.Unmarshal([]byte(data), &probe); err != nil || probe.Version != ReportVersion {
		return Report{}, false
	}
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		return Report{}, false
	}
	return r, true
}

// newWindow builds a Window from a used percentage and reset time, deriving
// RemainingPct and Tone.
func newWindow(id, label, shortLabel string, summary bool, usedPct *float64, resetsAt *time.Time) Window {
	w := Window{
		ID:         id,
		Label:      label,
		ShortLabel: shortLabel,
		Summary:    summary,
		UsedPct:    usedPct,
		ResetsAt:   resetsAt,
		Tone:       ToneFromUsedPct(usedPct),
	}
	if usedPct != nil {
		rem := 100 - *usedPct
		if rem < 0 {
			rem = 0
		}
		w.RemainingPct = &rem
	}
	return w
}

// unavailableReport is a Report for an explainable "no usable data" outcome.
func unavailableReport(p Problem, now time.Time) Report {
	return Report{Version: ReportVersion, Status: StatusUnavailable, Problem: &p, FetchedAt: now.UTC()}
}

// errorReport is a Report carrying a transient failure in fixed vocabulary.
func errorReport(msg string, now time.Time) Report {
	return Report{Version: ReportVersion, Status: StatusError, Error: msg, FetchedAt: now.UTC()}
}
