package quota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Codex quota observation (ADR-0028 §7.3). Fixtures-only and inert: nothing
// calls these while the proxy's OpenAI upstream is api.openai.com/v1, which
// does not emit x-codex-* headers. Ports refs/cliproxyapi/internal/runtime/
// executor/helps/codex_quota.go (ParseCodexQuotaEventHeaders) and the codex
// arm of sdk/cliproxy/auth/quota_signals.go (isQuotaSignalHeaderForProvider);
// HTTP header names come from refs/codex/codex-rs/codex-api/src/rate_limits.rs.

const (
	codexRateLimitsEvent   = "codex.rate_limits"
	codexAdditionalPrefix  = "X-Codex-Additional-"
	maxCodexAdditionalSets = 8
)

var codexSignalMarkers = []string{
	"-allowed", "-limit-reached", "-limit-name", "-used-percent",
	"-window-minutes", "-reset-after-seconds", "-reset-at", "-over-secondary-limit-percent",
}

func isCodexSignalHeader(lower string) bool {
	if lower == "x-codex-active-limit" || lower == "x-codex-plan-type" || strings.HasPrefix(lower, "x-codex-credits-") {
		return true
	}
	if !strings.HasPrefix(lower, "x-codex-") {
		return false
	}
	for _, m := range codexSignalMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// codexFieldSuffixes are the per-namespace fields the parser reads.
var codexFieldSuffixes = []string{
	"primary-used-percent", "primary-window-minutes", "primary-reset-after-seconds", "primary-reset-at",
	"secondary-used-percent", "secondary-window-minutes", "secondary-reset-after-seconds", "secondary-reset-at",
	"limit-reached", "limit-name", "allowed",
}

// splitCodexField splits "x-codex-<ns->field" into namespace and field. The
// base (unnamed) namespace is "".
func splitCodexField(lower string) (ns, field string, ok bool) {
	rest, found := strings.CutPrefix(lower, "x-codex-")
	if !found {
		return "", "", false
	}
	for _, f := range codexFieldSuffixes {
		if rest == f {
			return "", f, true
		}
		if p, found := strings.CutSuffix(rest, "-"+f); found && p != "" {
			return strings.TrimPrefix(p, "additional-"), f, true
		}
	}
	return "", "", false
}

// ParseCodexObservation reads the x-codex-* HTTP headers of one response.
// Cooldown is never derived from it.
func ParseCodexObservation(h http.Header, at time.Time) (Observation, bool) {
	sig := collectSignals(h, isCodexSignalHeader)
	o := Observation{At: at, PlanType: sig["x-codex-plan-type"]}

	byNS := map[string]map[string]string{}
	for name, v := range sig {
		ns, field, ok := splitCodexField(name)
		if !ok {
			continue
		}
		if byNS[ns] == nil {
			byNS[ns] = map[string]string{}
		}
		byNS[ns][field] = v
	}
	namespaces := make([]string, 0, len(byNS))
	for ns := range byNS {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces) // "" (base) first, the rest alphabetical

	for _, ns := range namespaces {
		f := byNS[ns]
		status := codexStatus(f)
		for _, role := range []string{"primary", "secondary"} {
			if w, ok := codexWindow(ns, role, f, status, at); ok {
				o.Windows = append(o.Windows, w)
			}
		}
	}
	if len(o.Windows) == 0 && o.PlanType == "" {
		return Observation{}, false
	}
	return o, true
}

func codexStatus(f map[string]string) string {
	if strings.EqualFold(f["limit-reached"], "true") {
		return statusRejected
	}
	switch strings.ToLower(f["allowed"]) {
	case "true":
		return statusAllowed
	case "false":
		return statusRejected
	}
	return ""
}

func codexWindow(ns, role string, f map[string]string, status string, at time.Time) (Window, bool) {
	used := codexPct(f[role+"-used-percent"])
	var resets *time.Time
	if sec, ok := parseNumber(f[role+"-reset-at"]); ok && sec > 0 && sec <= maxResetEpochSec {
		t := time.Unix(int64(sec), 0)
		resets = &t
	} else if sec, ok := parseNumber(f[role+"-reset-after-seconds"]); ok && sec >= 0 && sec <= maxResetEpochSec {
		t := at.Add(time.Duration(sec) * time.Second)
		resets = &t
	}
	if used == nil && resets == nil {
		return Window{}, false
	}

	id, label, short := strings.ToLower(role), strings.ToUpper(role[:1])+role[1:], ""
	var duration *int64
	if m, ok := parseNumber(f[role+"-window-minutes"]); ok && m > 0 && m <= 1e7 {
		minutes := int64(m)
		secs := minutes * 60
		duration = &secs
		switch minutes {
		case 300:
			id, label, short = "five_hour", "5-hour", "5h"
		case 10080:
			id, label, short = "weekly", "Weekly", "7d"
		default:
			id, label, short = fmt.Sprintf("window_%dm", minutes), fmt.Sprintf("%d-minute", minutes), fmt.Sprintf("%dm", minutes)
		}
	}
	w := newWindow(id, label, short, ns == "" && id == "five_hour", used, resets)
	w.DurationSeconds = duration
	if ns != "" {
		name := f["limit-name"]
		if name == "" {
			name = ns
		}
		w.ID = ns + "_" + w.ID
		w.Label = name + " " + w.Label
		w.Summary = false
		w.Scope = &Scope{Kind: ScopeFeature, Name: ns}
	}
	return observedWindow(w, status), true
}

func codexPct(raw string) *float64 {
	f, ok := parseNumber(raw)
	if !ok || f < 0 || f > 100 {
		return nil
	}
	return &f
}

func parseNumber(raw string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// CodexQuotaEventHeaders turns one Codex websocket event (a codex.rate_limits
// event or an error event carrying headers) into the x-codex-* header form of
// HTTP responses. nil means the payload carries no quota data. Both
// additional_rate_limits shapes (object keyed by limit name, array with
// limit_name) and both snake_case and camelCase spellings are read; at most 8
// additional limits are kept, in document order.
func CodexQuotaEventHeaders(payload []byte) http.Header {
	if len(payload) == 0 {
		return nil
	}
	kind := codexEventKind(payload)
	if kind == codexEventOther {
		return nil
	}
	var root map[string]json.RawMessage
	if !decodeJSON(payload, &root) {
		return nil
	}
	if kind == codexEventError {
		return codexHeadersObject(rawValue(root, "headers"))
	}

	headers := make(http.Header)
	has := false
	has = addCodexRateLimit(headers, "X-Codex-", firstRaw(root, "rate_limits", "rateLimit")) || has

	if add := firstRaw(root, "additional_rate_limits", "additionalRateLimits"); add != nil {
		count := 0
		eachAdditional(add, func(name string, value json.RawMessage) bool {
			if count >= maxCodexAdditionalSets {
				return false
			}
			ident := normalizeCodexIdentifier(name)
			if ident == "" {
				return true
			}
			info := firstRaw(objectOf(value), "rate_limit", "rateLimit")
			if info == nil {
				info = value
			}
			prefix := codexAdditionalPrefix + ident + "-"
			if addCodexRateLimit(headers, prefix, info) {
				if validCodexText(name) {
					headers.Set(prefix+"Limit-Name", name)
				}
				has = true
				count++
			}
			return true
		})
	}
	has = addCodexRateLimit(headers, "X-Codex-Code-Review-", firstRaw(root, "code_review_rate_limits", "codeReviewRateLimits")) || has

	if credits := objectOf(firstRaw(root, "credits")); credits != nil {
		has = setCodexScalar(headers, "X-Codex-Credits-Has-Credits", credits, "has_credits", "hasCredits") || has
		has = setCodexScalar(headers, "X-Codex-Credits-Unlimited", credits, "unlimited") || has
		has = setCodexScalar(headers, "X-Codex-Credits-Balance", credits, "balance") || has
	}
	if !has {
		return nil
	}
	if active := firstScalar(root, "metered_limit_name", "meteredLimitName", "limit_name", "limitName"); validCodexIdentifier(active) {
		headers.Set("X-Codex-Active-Limit", active)
	}
	if plan := firstScalar(root, "plan_type", "planType"); validCodexText(plan) {
		headers.Set("X-Codex-Plan-Type", plan)
	}
	return headers
}

const (
	codexEventOther = iota
	codexEventError
	codexEventRateLimits
)

// codexEventKind scans a bounded prefix for the top-level "type" so frames
// that are not quota events are never fully parsed (ref codexQuotaEventKind).
func codexEventKind(payload []byte) int {
	const scanLimit = 256
	head := payload
	if len(head) > scanLimit {
		head = head[:scanLimit]
	}
	space := func(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }
	for i := 0; i+6 < len(head); i++ {
		if !bytes.HasPrefix(head[i:], []byte(`"type"`)) {
			continue
		}
		j := i + 6
		for j < len(head) && space(head[j]) {
			j++
		}
		if j >= len(head) || head[j] != ':' {
			continue
		}
		j++
		for j < len(head) && space(head[j]) {
			j++
		}
		switch {
		case bytes.HasPrefix(head[j:], []byte(`"error"`)):
			return codexEventError
		case bytes.HasPrefix(head[j:], []byte(`"`+codexRateLimitsEvent+`"`)):
			return codexEventRateLimits
		}
	}
	return codexEventOther
}

func decodeJSON(data []byte, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v) == nil
}

func objectOf(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if !decodeJSON(raw, &m) {
		return nil
	}
	return m
}

func rawValue(obj map[string]json.RawMessage, key string) json.RawMessage {
	v, ok := obj[key]
	if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return nil
	}
	return v
}

func firstRaw(obj map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, k := range keys {
		if v := rawValue(obj, k); v != nil {
			return v
		}
	}
	return nil
}

// scalarOf renders a JSON string, number or bool; anything else is "".
func scalarOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if !decodeJSON(raw, &v) {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func firstScalar(obj map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if s := scalarOf(rawValue(obj, k)); s != "" {
			return s
		}
	}
	return ""
}

func setCodexScalar(h http.Header, name string, obj map[string]json.RawMessage, keys ...string) bool {
	s := firstScalar(obj, keys...)
	if s == "" {
		return false
	}
	h.Set(name, s)
	return true
}

func addCodexRateLimit(h http.Header, prefix string, raw json.RawMessage) bool {
	info := objectOf(raw)
	if info == nil {
		return false
	}
	changed := setCodexScalar(h, prefix+"Allowed", info, "allowed")
	changed = setCodexScalar(h, prefix+"Limit-Reached", info, "limit_reached", "limitReached") || changed
	for _, role := range []string{"primary", "secondary"} {
		win := objectOf(firstRaw(info, role))
		if win == nil {
			continue
		}
		used, hasUsed := parseNumber(firstScalar(win, "used_percent", "usedPercent"))
		minutes, hasMinutes := parseNumber(firstScalar(win, "window_minutes", "windowMinutes"))
		after, hasAfter := parseNumber(firstScalar(win, "reset_after_seconds", "resetAfterSeconds"))
		at, hasAt := parseNumber(firstScalar(win, "reset_at", "resetAt"))
		hasAfter = hasAfter && after >= 0
		hasAt = hasAt && at > 0
		if !hasUsed || !hasMinutes || used < 0 || used > 100 || minutes < 1 || (!hasAfter && !hasAt) {
			continue
		}
		wp := prefix + strings.ToUpper(role[:1]) + role[1:] + "-"
		setCodexScalar(h, wp+"Used-Percent", win, "used_percent", "usedPercent")
		setCodexScalar(h, wp+"Window-Minutes", win, "window_minutes", "windowMinutes")
		if hasAfter {
			setCodexScalar(h, wp+"Reset-After-Seconds", win, "reset_after_seconds", "resetAfterSeconds")
		}
		if hasAt {
			setCodexScalar(h, wp+"Reset-At", win, "reset_at", "resetAt")
		}
		changed = true
	}
	return changed
}

// eachAdditional walks additional_rate_limits in document order: an object
// keyed by limit name, or an array whose items carry limit_name.
func eachAdditional(raw json.RawMessage, fn func(name string, value json.RawMessage) bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return
	}
	switch tok {
	case json.Delim('{'):
		for dec.More() {
			k, err := dec.Token()
			key, isStr := k.(string)
			if err != nil || !isStr {
				return
			}
			var v json.RawMessage
			if dec.Decode(&v) != nil {
				return
			}
			if !fn(strings.TrimSpace(key), v) {
				return
			}
		}
	case json.Delim('['):
		for dec.More() {
			var v json.RawMessage
			if dec.Decode(&v) != nil {
				return
			}
			name := firstScalar(objectOf(v), "limit_name", "limitName", "name")
			if name == "" {
				continue
			}
			if !fn(name, v) {
				return
			}
		}
	}
}

func codexHeadersObject(raw json.RawMessage) http.Header {
	obj := objectOf(raw)
	if obj == nil {
		return nil
	}
	h := make(http.Header)
	for key, v := range obj {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if !isCodexEventHeaderName(name) {
			continue
		}
		if s := scalarOf(v); s != "" {
			h.Set(name, s)
		}
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// isCodexEventHeaderName is the ref's isCodexQuotaHeaderName: it also lets
// retry-after and x-ratelimit-* through the event, where the parsers then
// ignore them.
func isCodexEventHeaderName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "retry-after" || strings.HasPrefix(lower, "x-ratelimit-") {
		return true
	}
	return isCodexSignalHeader(lower)
}

func normalizeCodexIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, c := range value {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-':
			b.WriteRune(c)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	ident := strings.Trim(b.String(), "-_.")
	if ident == "" || !validCodexIdentifier(ident) {
		return ""
	}
	return ident
}

func validCodexIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func validCodexText(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\r\n")
}
