package serve

import (
	"net/url"
	"strings"
)

// SafeRepoURL returns raw reduced to something safe to show a peer: no local
// paths, no credentials, no query or fragment. It returns "" when nothing
// safe remains.
func SafeRepoURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || isLocalPath(s) {
		return ""
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return ""
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ssh", "git":
		default:
			return ""
		}
		u.User = nil
		u.RawQuery = ""
		u.ForceQuery = false
		u.Fragment = ""
		u.RawFragment = ""
		return u.String()
	}
	at := strings.Index(s, "@")
	colon := strings.Index(s, ":")
	if at < 0 || colon < 0 || colon < at {
		return ""
	}
	if strings.Contains(s[:colon], "/") {
		return ""
	}
	if s[:at] == "git" {
		return s
	}
	return s[at+1:]
}

func isLocalPath(s string) bool {
	for _, p := range []string{"/", "./", "../", "~", `\\`, "file://"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			return true
		}
	}
	return len(s) >= 3 && isASCIILetter(s[0]) && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
}

func isASCIILetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
