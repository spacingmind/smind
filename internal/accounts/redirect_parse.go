package accounts

import (
	"net/url"
	"strings"
)

// maxPastedRedirectBytes bounds what account.oauthComplete will parse. A real
// redirect is a few hundred bytes; the cap keeps a bad paste cheap.
const maxPastedRedirectBytes = 4096

// badRedirect is the one error parsePastedRedirect returns for input it
// cannot use. Its detail is fixed text: it never contains the input, which
// carries the authorization code and state.
func badRedirect() error {
	return &LoginError{
		Code:   LoginCodeBadRedirect,
		Detail: "the pasted text is not a login redirect (expected the redirect URL, its query string, or code#state)",
	}
}

// parsePastedRedirect extracts the authorization result from text a user
// pasted after the vendor's redirect failed to reach the daemon's listener
// (ADR-0033 D3). Accepted forms:
//
//	a full URL      http://localhost:54545/callback?code=C&state=S
//	a bare query    ?code=C&state=S  or  code=C&state=S
//	Claude's paste  C#S
//
// code, state and error are read from the query; when absent there they are
// read from the fragment parsed as a query. An error= value yields a result
// with errCode set (and no code requirement), like the listener's. Anything
// else is a LoginCodeBadRedirect error that never echoes the input.
func parsePastedRedirect(s string) (callbackResult, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxPastedRedirectBytes {
		return callbackResult{}, badRedirect()
	}

	if code, state, ok := splitCodeHash(s); ok {
		return callbackResult{code: code, state: state}, nil
	}

	rest, fragment := s, ""
	if i := strings.Index(rest, "#"); i >= 0 {
		rest, fragment = rest[:i], rest[i+1:]
	}
	query := ""
	switch i := strings.Index(rest, "?"); {
	case i >= 0:
		query = rest[i+1:]
	case !strings.Contains(rest, "/"):
		// A bare query string (code=C&state=S) has no path to skip. A URL
		// without a query (http://host/cb) falls through to "no code".
		query = rest
	}

	q, err := url.ParseQuery(query)
	if err != nil {
		return callbackResult{}, badRedirect()
	}
	f, err := url.ParseQuery(fragment)
	if err != nil {
		return callbackResult{}, badRedirect()
	}
	get := func(key string) string {
		if v := q.Get(key); v != "" {
			return v
		}
		return f.Get(key)
	}

	res := callbackResult{code: get("code"), state: get("state")}
	if e := get("error"); e != "" {
		res.errCode = safeErrCode(e)
		return res, nil
	}
	if res.code == "" {
		return callbackResult{}, badRedirect()
	}
	return res, nil
}

// splitCodeHash recognizes Claude's "code#state" paste: exactly one '#',
// two non-empty parts, and none of the characters that would make it a URL or
// a query string.
func splitCodeHash(s string) (code, state string, ok bool) {
	if strings.Count(s, "#") != 1 || strings.ContainsAny(s, "=&/:? \t\r\n") {
		return "", "", false
	}
	code, state, _ = strings.Cut(s, "#")
	if code == "" || state == "" {
		return "", "", false
	}
	return code, state, true
}

// safeErrCode keeps a vendor error code (RFC 6749 §4.1.2.1: access_denied,
// ...) only when it looks like one, so pasted free text never lands in an
// error message.
func safeErrCode(s string) string {
	if len(s) > 64 {
		return "error"
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '_' {
			return "error"
		}
	}
	return s
}
