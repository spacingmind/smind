package failure

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reRetryDelayText = regexp.MustCompile(`"retryDelay"\s*:\s*"(\d+(?:\.\d+)?)s"`)

const retryInfoMaxDepth = 6

// ParseRetryInfo reads a google.rpc.RetryInfo detail out of a JSON error body
// (in error.details[] or a top-level details[]) and returns its retryDelay.
// (0, false) when there is none or the delay is not a positive, sane number.
func ParseRetryInfo(body []byte) (time.Duration, bool) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, false
	}
	return findRetryInfo(v, 0)
}

func findRetryInfo(v any, depth int) (time.Duration, bool) {
	if depth > retryInfoMaxDepth {
		return 0, false
	}
	switch x := v.(type) {
	case map[string]any:
		if t, _ := x["@type"].(string); strings.HasSuffix(t, "google.rpc.RetryInfo") {
			if rd, ok := x["retryDelay"]; ok {
				if d, ok := delayValue(rd); ok {
					return d, true
				}
			}
		}
		for _, c := range x {
			if d, ok := findRetryInfo(c, depth+1); ok {
				return d, true
			}
		}
	case []any:
		for _, c := range x {
			if d, ok := findRetryInfo(c, depth+1); ok {
				return d, true
			}
		}
	}
	return 0, false
}

func delayValue(v any) (time.Duration, bool) {
	var secs float64
	switch x := v.(type) {
	case string:
		s := strings.TrimSuffix(strings.TrimSpace(x), "s")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		secs = f
	case map[string]any:
		s, ok := numberOf(x["seconds"])
		if !ok && x["seconds"] != nil {
			return 0, false
		}
		n, _ := numberOf(x["nanos"])
		secs = s + n/1e9
	default:
		return 0, false
	}
	if math.IsNaN(secs) || math.IsInf(secs, 0) || secs <= 0 || secs > 1e9 {
		return 0, false
	}
	return time.Duration(secs * float64(time.Second)), true
}

func numberOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

// retryInfoFromText finds a RetryInfo retryDelay inside an error string that
// embeds the provider's JSON body.
func retryInfoFromText(s string) (time.Duration, bool) {
	m := reRetryDelayText.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil || f <= 0 || f > 1e9 {
		return 0, false
	}
	return time.Duration(f * float64(time.Second)), true
}
