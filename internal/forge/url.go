package forge

import (
	"net/url"
	"regexp"
	"strings"
)

// prPathRe is the only path shape a PR URL may have.
var prPathRe = regexp.MustCompile(`^/([A-Za-z0-9_.-]{1,100})/([A-Za-z0-9_.-]{1,100})/pull/([1-9][0-9]{0,9})/?$`)

// ValidPRURL checks that raw is a pull request URL on host and returns its
// canonical form (query and fragment dropped, no trailing slash). It accepts
// https only, no userinfo, host equal to host (case-insensitive), and exactly
// /<owner>/<name>/pull/<n>; anything else returns "", false. A PR URL comes
// from the forge CLI's output and ends up in a clickable link, so a hostile
// value (javascript:, another host, credentials) must never get through
// (ADR-0039 section 11).
func ValidPRURL(raw, host string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || host == "" ||
		!strings.EqualFold(u.Host, host) {
		return "", false
	}
	m := prPathRe.FindStringSubmatch(u.Path)
	if m == nil || m[1] == "." || m[1] == ".." || m[2] == "." || m[2] == ".." {
		return "", false
	}
	return "https://" + u.Host + "/" + m[1] + "/" + m[2] + "/pull/" + m[3], true
}
