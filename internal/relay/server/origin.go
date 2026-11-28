package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DefaultWebOrigin is the hosted web client's origin (ADR-0052 H10).
const DefaultWebOrigin = "https://app.spacingmind.com"

// ParseAllowedOrigins validates and normalises operator-configured browser
// origins (spec D3/D4).
func ParseAllowedOrigins(values []string) ([]string, error) {
	var origins []string
	for _, value := range values {
		origin, ok := parseOrigin(strings.TrimSpace(value))
		if !ok {
			return nil, fmt.Errorf("relay: invalid allowed origin %q", value)
		}
		origins = append(origins, origin)
	}
	return origins, nil
}

// parseOrigin accepts only a serialized HTTP(S) origin, never a URL with
// credentials, a path (even /), query, or fragment. Ports remain explicit.
func parseOrigin(value string) (string, bool) {
	if strings.ContainsAny(value, "*?#") {
		return "", false
	}
	u, err := url.Parse(value)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname())
	if scheme != "https" && scheme != "http" {
		return "", false
	}
	if scheme == "http" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return "", false
	}
	if strings.Contains(host, ":") {
		if net.ParseIP(host) == nil || !strings.HasPrefix(u.Host, "[") {
			return "", false
		}
	} else {
		// Restrict DNS names to hostname characters; url.Parse otherwise accepts
		// characters such as commas which cannot identify an operator's host.
		if host == "" || strings.ContainsAny(u.Host, "[]") {
			return "", false
		}
		for _, c := range host {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
				return "", false
			}
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return "", false
		}
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

// EffectiveOrigins returns the allowlist: the default (unless noDefault) plus
// the already validated extras, de-duplicated.
func EffectiveOrigins(extras []string, noDefault bool) []string {
	var origins []string
	seen := make(map[string]bool)
	add := func(origin string) {
		if !seen[origin] {
			seen[origin] = true
			origins = append(origins, origin)
		}
	}
	if !noDefault {
		add(DefaultWebOrigin)
	}
	for _, origin := range extras {
		add(origin)
	}
	return origins
}

type originPolicy struct{ origins map[string]bool }

func newOriginPolicy(extras []string, noDefault bool) *originPolicy {
	p := &originPolicy{origins: make(map[string]bool)}
	for _, origin := range EffectiveOrigins(extras, noDefault) {
		p.origins[origin] = true
	}
	return p
}

func (p *originPolicy) allows(originHeader string) bool {
	origin, ok := parseOrigin(originHeader)
	return ok && p.origins[origin]
}

func (p *originPolicy) check(r *http.Request) bool {
	origins, present := r.Header["Origin"]
	if !present {
		return true
	}
	return len(origins) == 1 && p.allows(origins[0])
}
