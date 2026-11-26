// Package netenv gives a redacted view of the outbound proxy variables of
// the environment: host only, never credentials, in a fixed order.
package netenv

import (
	"net/url"
	"strings"
)

// ProxyVar is one set proxy variable with its value as shown to users.
type ProxyVar struct {
	Name  string
	Value string
}

// RedactProxy returns scheme://host[:port] without userinfo, path, query or
// fragment. A value without a scheme is read as http://host:port. Whatever
// does not parse or has no host becomes "(unparsable)"; no part of raw is
// echoed, so credentials in an unparsable value never leak.
func RedactProxy(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "(unparsable)"
	}
	return u.Scheme + "://" + u.Host
}

// ProxyEnv lists the set proxy variables, in the order HTTPS_PROXY,
// https_proxy, HTTP_PROXY, http_proxy, NO_PROXY, no_proxy. *_PROXY values
// are redacted with RedactProxy; NO_PROXY is a host list, not a URL, and is
// only truncated to 120 runes.
func ProxyEnv(getenv func(string) string) []ProxyVar {
	if getenv == nil {
		return nil
	}
	var out []ProxyVar
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy"} {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			continue
		}
		value := v
		switch name {
		case "NO_PROXY", "no_proxy":
			if r := []rune(v); len(r) > 120 {
				value = string(r[:120]) + "..."
			}
		default:
			value = RedactProxy(v)
		}
		out = append(out, ProxyVar{Name: name, Value: value})
	}
	return out
}
