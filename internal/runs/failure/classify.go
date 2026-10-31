// Package failure classifies a failed run (plan window, transient 429, auth, model), from a structured
// backend signal and/or the run's final error text. It is pure (stdlib only).
package failure

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The Result.Kind values (the wire's failureKind). Kind == "" means the run
// failed for none of these reasons.
const (
	// KindPlanWindowExhausted: the plan's usage window is spent (a window, a
	// reset time, or the provider's own "usage limit reached").
	KindPlanWindowExhausted = "plan_window_exhausted"
	// KindTransient429: a short-lived throttle (429 / overloaded with a
	// Retry-After or an overload body) that is not a plan window.
	KindTransient429 = "transient_429"
	// KindAuthExpired: the provider refused the credential.
	KindAuthExpired = "auth_expired"
	// KindModelNotInPlan: the account's plan does not include the model.
	KindModelNotInPlan = "model_not_in_plan"
	// KindRateLimited is the residual: a rate limit that cannot be told apart
	// (a bare 429 / rate words, no window, no reset, no Retry-After).
	KindRateLimited = "rate_limited"
	// KindCLIExited: the CLI ended its stream without a result.
	KindCLIExited = "cli_exited"
)

// Typed class hints a runner passes in Input.Code when its backend reported a
// structured error (a codex CodexErrorInfo, an HTTP status). They beat text.
const (
	CodeUsageLimit   = "usage_limit"
	CodeUnauthorized = "unauthorized"
	CodeOverloaded   = "overloaded"
	CodeRateLimit    = "rate_limit"
)

// Signal is a structured rate-limit report from a backend (claude-native's rate_limit_event).
type Signal struct {
	Status   string    // "rejected", "allowed_warning", "allowed", ...
	ResetsAt time.Time // zero = unknown
	Type     string    // "five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet", "overage", ...
}

// Input is what Classify looks at.
type Input struct {
	Provider string  // smind provider id: "glm", "claude-native", "codex-native", "kimi"
	Err      string  // the run's final error text, cause first
	Signal   *Signal // the last signal seen during the run, or nil
	// Vendor is the resolved vendor profile of the provider
	// (zai|anthropic|openai|generic); "" = generic.
	Vendor string
	// Model is the model the run asked for ("" = the harness default).
	Model string
	// Code is a typed class hint from the runner's backend (Code* constants);
	// "" = none. RetryAfter is the provider's Retry-After when the backend saw
	// it (0 = absent). Both are optional.
	Code       string
	RetryAfter time.Duration
}

// Result is the classification. Kind == "" means not rate limited.
type Result struct {
	Kind    string
	Window  string     // "<N>h", "weekly", "weekly-opus", "weekly-sonnet", or ""
	ResetAt *time.Time // UTC; nil = unknown
	// ResetInferred is true when ResetAt was read in an assumed zone (Z.ai's
	// zone-less wall clock as UTC+8, a Claude "resets 3pm" in the daemon's
	// own zone). A stated zone, an epoch or a backend signal is never inferred.
	ResetInferred bool
	// ResetZone names the zone ResetAt's text was read in, for a client's
	// "which zone?" note: "UTC+8" (GLM), the IANA zone Claude printed, or
	// DaemonLocalPrefix + the daemon's zone when assumed. "" = none (no
	// reset parsed, an epoch or a backend signal).
	ResetZone string
	// RetryAfter is the provider's own wait hint for a transient_429 (from the
	// backend or "try again in Ns" text); 0 = none.
	RetryAfter time.Duration
}

// DaemonLocalPrefix starts a ResetZone read in the daemon's own zone.
const DaemonLocalPrefix = "daemon-local:"

var (
	reGLM       = regexp.MustCompile(`(?i)429[^\n]{0,40}usage limit reached for (\d+) hour`)
	reGLMReset  = regexp.MustCompile(`(?i)reset at (?:(\d{4}-\d{2}-\d{2})[ T])?(\d{1,2}:\d{2}(?::\d{2})?)`)
	reClaudeOld = regexp.MustCompile(`(?i)usage limit reached\|(\d{9,11})`)
	reClaudeNew = regexp.MustCompile(`(?i)(\d+)-hour limit reached`)
	reClaudeRst = regexp.MustCompile(`(?i)resets (\d{1,2})(?::(\d{2}))?\s*(am|pm)`)
	reCodex     = regexp.MustCompile(`(?i)you'?ve hit your usage limit`)
	// Claude Code's newer refusal text: "You've hit your session limit · resets
	// 1pm (Asia/Saigon)" (also "weekly", "Opus", "Sonnet"); the zone, when
	// printed, is an IANA name.
	reClaudeHit     = regexp.MustCompile(`(?i)you'?ve hit your (session |weekly |usage |opus |sonnet )?limit`)
	reClaudeResetTZ = regexp.MustCompile(`(?i)resets (\d{1,2})(?::(\d{2}))?\s*(am|pm)(?:\s*\(([A-Za-z_]+/[A-Za-z_+\-0-9]+)\))?`)
	reCodexQuota    = regexp.MustCompile(`(?i)quota exceeded\. check your plan and billing`) // refs/codex error.rs QuotaExceeded
	reGenericUL     = regexp.MustCompile(`(?i)usage limit reached`)
	reGenericRLE    = regexp.MustCompile(`(?i)rate_limit_error`)
	reRateWords     = regexp.MustCompile(`(?i)rate limit|rate-limit|ratelimit|too many requests`)
	// Sources: claude-code CHANGELOG ("Not logged in · Please run /login",
	// the CLI's "Invalid API key"), the Anthropic API error type
	// authentication_error (also what internal/server/proxy.go speaks), HTTP 401.
	reAuth = regexp.MustCompile(`(?i)not logged in|please run /login|invalid api key|invalid x-api-key|oauth token has expired|authentication_error|\b401\b[^\n]{0,40}unauthorized|unauthorized[^\n]{0,40}\b401\b`)
	// Sources: claude-code CHANGELOG ("There's an issue with the selected
	// model ... may not exist or you may not have access to it"), refs/codex
	// core tests ("Model not found <m>", "The '<m>' model is not supported when
	// using Codex with a ChatGPT account"), proxy.go model_not_found.
	reModel = regexp.MustCompile(`(?i)issue with the selected model|may not exist or you may not have access|model not found|model_not_found|model is not supported when using codex|not_found_error[^\n]{0,80}\bmodel\b`)
	// Sources: refs/codex protocol error.rs ("Selected model is at capacity",
	// "We're currently experiencing high demand", "rate limit exceeded: ..."),
	// Anthropic overloaded_error (529).
	reTransient = regexp.MustCompile(`(?i)overloaded_error|selected model is at capacity|experiencing high demand|rate limit exceeded:|\boverloaded\b`)
	reTryAgain  = regexp.MustCompile(`(?i)try again in (\d+)\s*(seconds?|secs?|s|minutes?|mins?|m)\b`)
)

// Classify classifies a failed run. Unknown never classifies and no time is
// ever guessed. Precedence: a rejected signal or an explicit plan-usage text
// (plan_window_exhausted) beats a typed backend code, which beats auth text,
// which beats model text, which beats the generic 429 words (transient_429
// when there is a Retry-After or an overload body, else the residual
// rate_limited).
func Classify(in Input, now time.Time) Result {
	var res Result
	sigHit := false
	if s := in.Signal; s != nil && s.Status == "rejected" && s.Type != "overage" {
		sigHit = true
		res.Kind = KindPlanWindowExhausted
		res.Window = signalWindow(s.Type)
		if !s.ResetsAt.IsZero() {
			t := s.ResetsAt.UTC()
			res.ResetAt = &t
			res.ResetInferred = false
		}
	}
	txt := classifyText(in.Err, now, in.Vendor)
	if sigHit {
		if txt.Kind == KindPlanWindowExhausted {
			if res.Window == "" {
				res.Window = txt.Window
			}
			if res.ResetAt == nil {
				res.ResetAt, res.ResetInferred, res.ResetZone = txt.ResetAt, txt.ResetInferred, txt.ResetZone
			}
		}
		return res
	}
	if txt.Kind == KindPlanWindowExhausted {
		return txt
	}
	switch in.Code {
	case CodeUsageLimit:
		return Result{Kind: KindPlanWindowExhausted}
	case CodeUnauthorized:
		return Result{Kind: KindAuthExpired}
	case CodeOverloaded, CodeRateLimit:
		return Result{Kind: KindTransient429, RetryAfter: retryHint(in, now)}
	}
	if reAuth.MatchString(in.Err) {
		return Result{Kind: KindAuthExpired}
	}
	if reModel.MatchString(in.Err) {
		return Result{Kind: KindModelNotInPlan}
	}
	if reTransient.MatchString(in.Err) {
		return Result{Kind: KindTransient429, RetryAfter: retryHint(in, now)}
	}
	if txt.Kind == KindRateLimited {
		if d := retryHint(in, now); d > 0 {
			return Result{Kind: KindTransient429, RetryAfter: d}
		}
		return txt
	}
	return Result{}
}

// retryHint is the provider's wait hint: the backend's Retry-After, else a
// "try again in N seconds/minutes" in the text; 0 = none.
func retryHint(in Input, _ time.Time) time.Duration {
	if in.RetryAfter > 0 {
		return in.RetryAfter
	}
	if m := reTryAgain.FindStringSubmatch(in.Err); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return 0
		}
		if strings.HasPrefix(strings.ToLower(m[2]), "m") {
			return time.Duration(n) * time.Minute
		}
		return time.Duration(n) * time.Second
	}
	return 0
}

func signalWindow(t string) string {
	switch t {
	case "five_hour":
		return "5h"
	case "seven_day":
		return "weekly"
	case "seven_day_opus":
		return "weekly-opus"
	case "seven_day_sonnet":
		return "weekly-sonnet"
	}
	return ""
}

// profile says which vendor-specific pattern groups classifyText tries for
// vendor: GLM's, Claude's or Codex's; "generic"/"" tries none of them (the
// generic patterns always run, for every vendor).
func profile(vendor string) (glm, claude, codex bool) {
	switch vendor {
	case "zai":
		return true, false, false
	case "anthropic":
		return false, true, false
	case "openai":
		return false, false, true
	}
	return false, false, false
}

func classifyText(s string, now time.Time, vendor string) Result {
	if s == "" {
		return Result{}
	}
	tryGLM, tryClaude, tryCodex := profile(vendor)
	if tryGLM {
		if m := reGLM.FindStringSubmatch(s); m != nil {
			r := Result{Kind: KindPlanWindowExhausted, Window: m[1] + "h"}
			if rm := reGLMReset.FindStringSubmatch(s); rm != nil {
				// Z.ai prints its reset as wall-clock time with no zone, and its
				// backend runs on UTC+8 (Asia/Shanghai): the dogfood 429 of
				// 2026-10-22 16:33 +07 said "reset at 2026-10-22 17:35:18", and
				// third-party reports of the same message read it as UTC+8
				// (ExaDev/agent-shim#448). Never the daemon's own zone.
				r.ResetAt = glmReset(rm[1], rm[2], now.In(glmZone))
				r.ResetInferred = r.ResetAt != nil
				if r.ResetAt != nil {
					r.ResetZone = "UTC+8"
				}
			}
			return r
		}
	}
	if tryClaude {
		if m := reClaudeOld.FindStringSubmatch(s); m != nil {
			r := Result{Kind: KindPlanWindowExhausted}
			if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				t := time.Unix(sec, 0).UTC()
				r.ResetAt = &t
			}
			return r
		}
		if m := reClaudeNew.FindStringSubmatch(s); m != nil {
			r := Result{Kind: KindPlanWindowExhausted, Window: m[1] + "h"}
			if rm := reClaudeRst.FindStringSubmatch(s); rm != nil {
				r.ResetAt = nextAmPm(rm[1], rm[2], rm[3], now)
				r.ResetInferred = r.ResetAt != nil
				r.ResetZone = daemonZone(r.ResetAt, now)
			}
			return r
		}
		if m := reClaudeHit.FindStringSubmatch(s); m != nil {
			r := Result{Kind: KindPlanWindowExhausted, Window: claudeHitWindow(m[1])}
			if rm := reClaudeResetTZ.FindStringSubmatch(s); rm != nil {
				r.ResetAt = nextAmPmIn(rm[1], rm[2], rm[3], rm[4], now)
				r.ResetInferred = r.ResetAt != nil && !knownZone(rm[4])
				switch {
				case r.ResetAt == nil:
				case r.ResetInferred:
					r.ResetZone = daemonZone(r.ResetAt, now)
				default:
					r.ResetZone = rm[4]
				}
			}
			return r
		}
	}
	if tryCodex {
		if reCodex.MatchString(s) || reCodexQuota.MatchString(s) {
			return Result{Kind: KindPlanWindowExhausted}
		}
	}
	if reGenericUL.MatchString(s) {
		return Result{Kind: KindPlanWindowExhausted}
	}
	if reGenericRLE.MatchString(s) || (strings.Contains(s, "429") && reRateWords.MatchString(s)) {
		return Result{Kind: KindRateLimited}
	}
	return Result{}
}

// glmZone is the zone of Z.ai's "Your limit will reset at ..." text: a fixed
// UTC+8 (China has no daylight saving), without needing tzdata.
var glmZone = time.FixedZone("CST", 8*3600)

// glmReset resolves a GLM reset time: with a date it is that absolute
// instant in now's location (never rolled); without, the next occurrence.
func glmReset(date, hms string, now time.Time) *time.Time {
	if date == "" {
		return nextClock(hms, now)
	}
	layout := "2006-01-02 15:04:05"
	if strings.Count(hms, ":") == 1 {
		layout = "2006-01-02 15:04"
	}
	t, err := time.ParseInLocation(layout, date+" "+hms, now.Location())
	if err != nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// nextClock parses "H:MM[:SS]" as wall-clock time in now's location.
func nextClock(hms string, now time.Time) *time.Time {
	parts := strings.Split(hms, ":")
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nextOccurrence(nums[0], nums[1], nums[2], now)
}

func nextAmPm(h, m, ap string, now time.Time) *time.Time {
	hour, err := strconv.Atoi(h)
	if err != nil || hour < 1 || hour > 12 {
		return nil
	}
	min := 0
	if m != "" {
		if min, err = strconv.Atoi(m); err != nil {
			return nil
		}
	}
	hour %= 12
	if strings.EqualFold(ap, "pm") {
		hour += 12
	}
	return nextOccurrence(hour, min, 0, now)
}

func nextOccurrence(h, m, s int, now time.Time) *time.Time {
	if h < 0 || h > 23 || m < 0 || m > 59 || s < 0 || s > 59 {
		return nil
	}
	loc := now.Location()
	y, mo, d := now.Date()
	t := time.Date(y, mo, d, h, m, s, 0, loc)
	if !t.After(now) {
		t = time.Date(y, mo, d+1, h, m, s, 0, loc)
	}
	u := t.UTC()
	return &u
}

// claudeHitWindow maps the word in "You've hit your <word> limit" to a window:
// the session limit is Claude's 5-hour window.
func claudeHitWindow(word string) string {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "session":
		return "5h"
	case "weekly":
		return "weekly"
	case "opus":
		return "weekly-opus"
	case "sonnet":
		return "weekly-sonnet"
	}
	return ""
}

// daemonZone is the ResetZone of a time read in the daemon's own zone ("" when
// no time was parsed): its IANA name, or a UTC offset when it runs on "Local".
func daemonZone(at *time.Time, now time.Time) string {
	if at == nil {
		return ""
	}
	name := now.Location().String()
	if name == "" || name == "Local" {
		name = "UTC" + now.Format("-07:00")
	}
	return DaemonLocalPrefix + name
}

// knownZone reports whether zone names an IANA zone this daemon can load; a
// missing or unknown one leaves the time in the daemon's own zone (inferred).
func knownZone(zone string) bool {
	if zone == "" {
		return false
	}
	_, err := time.LoadLocation(zone)
	return err == nil
}

// nextAmPmIn is nextAmPm in the named IANA zone ("" or unknown = now's own).
func nextAmPmIn(h, m, ap, zone string, now time.Time) *time.Time {
	if zone != "" {
		if loc, err := time.LoadLocation(zone); err == nil {
			now = now.In(loc)
		}
	}
	return nextAmPm(h, m, ap, now)
}

// maxRefusalText bounds a "limit refusal" message: the CLI's whole answer is
// one short line, so a long text that merely mentions a limit is not one.
const maxRefusalText = 300

// IsLimitRefusal reports whether text is Claude Code's own "you've hit your
// limit" refusal (the whole assistant answer of a run that never reached the
// model). Callers must also require that the run did no work.
func IsLimitRefusal(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && len(text) <= maxRefusalText && reClaudeHit.MatchString(text)
}
