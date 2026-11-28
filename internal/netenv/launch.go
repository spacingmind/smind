package netenv

import "strings"

// launchProxyNames are the environment names whose non-empty value (spaces
// ignored) means an outbound proxy is set, in the order they are checked.
var launchProxyNames = []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"}

// launchSchemeNames are the proxy names a bare value of gets an http://
// scheme, in the order their entries are emitted.
var launchSchemeNames = []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"}

// launchNoProxyNames are the two no-proxy names, in the order their entries
// are emitted.
var launchNoProxyNames = []string{"NO_PROXY", "no_proxy"}

// launchLoopbackNames are the loopback names that must stay outside the
// proxy, in the order they are appended.
var launchLoopbackNames = []string{"127.0.0.1", "::1", "localhost"}

// ProxyLaunchEntries returns the extra NAME=VALUE entries a launch appends
// after the environment a child would otherwise inherit, so the child sees
// a proxy setup that works. effective is that environment, as NAME=VALUE
// entries where the last entry of a name wins and an entry without "=" is
// ignored. Nothing is returned when no proxy is set.
//
// Two problems this fixes: a bare proxy value (HTTPS_PROXY=proxy.corp:3128,
// no scheme) is read as http://host:port by the daemon's own transport but
// refused by older Claude Code headless runs, so a setup that works for the
// daemon breaks every claude-native turn -- the same value with an explicit
// scheme works for both. And the agents the daemon launches talk to its
// loopback services (the MCP bridge, the browser proxy, the run socket),
// which a corporate proxy cannot reach, so the loopback names must be
// excluded from proxying.
//
// The returned entries, in a fixed order: for each of HTTP_PROXY,
// http_proxy, HTTPS_PROXY, https_proxy whose trimmed value is non-empty and
// carries no "://", NAME=http://<value> -- userinfo, a port and an IPv6
// literal such as [::1]:8080 stay exactly as written, a value that already
// has a scheme and ALL_PROXY are never emitted. Then, for each of NO_PROXY
// and no_proxy whose value does not already list 127.0.0.1, ::1 and
// localhost (compared case-insensitively, spaces around a piece ignored):
// the value extended with the missing names in that order, or exactly
// 127.0.0.1,::1,localhost when it is empty or absent.
//
// A proxy value may carry user:pass@, so the function writes nothing
// anywhere -- no log, no error, no event: the entries only.
func ProxyLaunchEntries(effective []string) []string {
	set := false
	for _, name := range launchProxyNames {
		if strings.TrimSpace(launchEnvValue(effective, name)) != "" {
			set = true
			break
		}
	}
	if !set {
		return nil
	}
	var out []string
	for _, name := range launchSchemeNames {
		if v := strings.TrimSpace(launchEnvValue(effective, name)); v != "" && !strings.Contains(v, "://") {
			out = append(out, name+"=http://"+v)
		}
	}
	for _, name := range launchNoProxyNames {
		if v, ok := launchNoProxyValue(launchEnvValue(effective, name)); ok {
			out = append(out, name+"="+v)
		}
	}
	return out
}

// launchEnvValue returns the last value of name in effective, "" when the
// name is absent; an entry without "=" is ignored.
func launchEnvValue(effective []string, name string) string {
	value := ""
	for _, entry := range effective {
		if n, v, ok := strings.Cut(entry, "="); ok && n == name {
			value = v
		}
	}
	return value
}

// launchNoProxyValue returns the no-proxy value to emit for current and
// whether anything was appended: the loopback names it lacks, joined onto
// the original value with commas (the pieces and their spelling are kept as
// written; spaces are ignored for the comparison only). An empty or absent
// value becomes exactly the three loopback names. false when current
// already lists all three, so nothing is emitted for that name.
func launchNoProxyValue(current string) (string, bool) {
	if strings.TrimSpace(current) == "" {
		return strings.Join(launchLoopbackNames, ","), true
	}
	present := make([]bool, len(launchLoopbackNames))
	for _, piece := range strings.Split(current, ",") {
		piece = strings.TrimSpace(piece)
		for i, name := range launchLoopbackNames {
			if strings.EqualFold(piece, name) {
				present[i] = true
			}
		}
	}
	out := current
	appended := false
	for i, name := range launchLoopbackNames {
		if !present[i] {
			out += "," + name
			appended = true
		}
	}
	return out, appended
}
