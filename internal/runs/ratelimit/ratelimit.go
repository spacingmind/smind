// Package ratelimit classifies a failed run as rate limited, from a structured
// backend signal and/or the run's final error text. It is pure (stdlib only).
package ratelimit

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// KindRateLimited is the Result.Kind for a rate-limited run.
const KindRateLimited = "rate_limited"

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
}

// Result is the classification. Kind == "" means not rate limited.
type Result struct {
	Kind    string
	Window  string     // "<N>h", "weekly", "weekly-opus", "weekly-sonnet", or ""
	ResetAt *time.Time // UTC; nil = unknown
}

var (
	reGLM        = regexp.MustCompile(`(?i)429[^\n]{0,40}usage limit reached for (\d+) hour`)
	reGLMReset   = regexp.MustCompile(`(?i)reset at (?:(\d{4}-\d{2}-\d{2})[ T])?(\d{1,2}:\d{2}(?::\d{2})?)`)
	reClaudeOld  = regexp.MustCompile(`(?i)usage limit reached\|(\d{9,11})`)
	reClaudeNew  = regexp.MustCompile(`(?i)(\d+)-hour limit reached`)
	reClaudeRst  = regexp.MustCompile(`(?i)resets (\d{1,2})(?::(\d{2}))?\s*(am|pm)`)
	reCodex      = regexp.MustCompile(`(?i)you'?ve hit your usage limit`)
	reGenericUL  = regexp.MustCompile(`(?i)usage limit reached`)
	reGenericRLE = regexp.MustCompile(`(?i)rate_limit_error`)
	reRateWords  = regexp.MustCompile(`(?i)rate limit|rate-limit|ratelimit|too many requests`)
)

// Classify reports whether a failed run was rate limited. Unknown never
// classifies and no time is ever guessed.
func Classify(in Input, now time.Time) Result {
	var res Result
	sigHit := false
	if s := in.Signal; s != nil && s.Status == "rejected" && s.Type != "overage" {
		sigHit = true
		res.Kind = KindRateLimited
		res.Window = signalWindow(s.Type)
		if !s.ResetsAt.IsZero() {
			t := s.ResetsAt.UTC()
			res.ResetAt = &t
		}
	}
	txt := classifyText(in.Err, now)
	if txt.Kind == "" {
		return res
	}
	if !sigHit {
		return txt
	}
	if res.Window == "" {
		res.Window = txt.Window
	}
	if res.ResetAt == nil {
		res.ResetAt = txt.ResetAt
	}
	return res
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

func classifyText(s string, now time.Time) Result {
	if s == "" {
		return Result{}
	}
	if m := reGLM.FindStringSubmatch(s); m != nil {
		r := Result{Kind: KindRateLimited, Window: m[1] + "h"}
		if rm := reGLMReset.FindStringSubmatch(s); rm != nil {
			r.ResetAt = glmReset(rm[1], rm[2], now)
		}
		return r
	}
	if m := reClaudeOld.FindStringSubmatch(s); m != nil {
		r := Result{Kind: KindRateLimited}
		if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			t := time.Unix(sec, 0).UTC()
			r.ResetAt = &t
		}
		return r
	}
	if m := reClaudeNew.FindStringSubmatch(s); m != nil {
		r := Result{Kind: KindRateLimited, Window: m[1] + "h"}
		if rm := reClaudeRst.FindStringSubmatch(s); rm != nil {
			r.ResetAt = nextAmPm(rm[1], rm[2], rm[3], now)
		}
		return r
	}
	if reCodex.MatchString(s) {
		return Result{Kind: KindRateLimited}
	}
	if reGenericUL.MatchString(s) || reGenericRLE.MatchString(s) ||
		(strings.Contains(s, "429") && reRateWords.MatchString(s)) {
		return Result{Kind: KindRateLimited}
	}
	return Result{}
}

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
