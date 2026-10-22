// Package forge holds the forge-neutral (GitHub today, others later) types,
// the Provider interface, and the typed error set shared by every forge
// client (ADR-0039 §1, §6).
package forge

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Error codes for the wire (wsapi picks Code up via RPCCode).
const (
	CodeCLIMissing      = "forge_cli_missing"
	CodeUnauthenticated = "forge_unauthenticated"
	CodeNoRemote        = "forge_no_remote"
	CodeNotFound        = "forge_not_found"
	CodeRateLimited     = "forge_rate_limited"
	CodeTimeout         = "forge_timeout"
	CodeError           = "forge_error"
)

// Fixed, user-presentable messages per code. Each names the next step and
// never carries argv or stderr (those go in Error.Detail, scrubbed).
const (
	msgCLIMissing      = "The GitHub CLI (`gh`) isn't installed on the daemon host."
	msgUnauthenticated = "GitHub isn't signed in on the daemon host. Run `gh auth login` there, or paste a token in Settings → GitHub."
	msgNoRemote        = "This workspace's `origin` isn't a GitHub remote."
	msgNotFound        = "That pull request or issue wasn't found, or you don't have access."
	msgRateLimited     = "GitHub is rate-limiting requests. Try again later."
	msgTimeout         = "GitHub didn't answer in time."
	msgError           = "GitHub returned an error."
)

// Error is the typed forge error: a machine-readable Code, a fixed Message
// sentence, an optional scrubbed Detail, and RetryAt for rate limits.
type Error struct {
	Code    string
	Message string
	Detail  string
	RetryAt time.Time
}

// Error returns the fixed Message only -- never argv, never stderr.
func (e *Error) Error() string { return e.Message }

// RPCCode is the wire error code.
func (e *Error) RPCCode() string { return e.Code }

// RPCData is the wire error data: detail and retryAt, each omitted when
// empty; nil when both are.
func (e *Error) RPCData() any {
	if e.Detail == "" && e.RetryAt.IsZero() {
		return nil
	}
	m := map[string]any{}
	if e.Detail != "" {
		m["detail"] = e.Detail
	}
	if !e.RetryAt.IsZero() {
		m["retryAt"] = e.RetryAt.UTC().Format(time.RFC3339)
	}
	return m
}

// NewError builds the typed error for code with its fixed message and the
// clipped, scrubbed detail. Unknown codes become CodeError.
func NewError(code, detail string) *Error {
	e := &Error{Code: code, Detail: ClipDetail(detail)}
	switch code {
	case CodeCLIMissing:
		e.Message = msgCLIMissing
	case CodeUnauthenticated:
		e.Message = msgUnauthenticated
	case CodeNoRemote:
		e.Message = msgNoRemote
	case CodeNotFound:
		e.Message = msgNotFound
	case CodeRateLimited:
		e.Message = msgRateLimited
	case CodeTimeout:
		e.Message = msgTimeout
	default:
		e.Code = CodeError
		e.Message = msgError
	}
	return e
}

var scrubRes = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`(?i)(authorization:).*`),
	regexp.MustCompile(`://[^/\s@]*:[^/\s@]*@`),
	regexp.MustCompile(`x-access-token:[^@\s]*@`),
}

const redacted = "[redacted]"

// Scrub redacts GitHub tokens, Authorization header values, and URL
// userinfo from s.
func Scrub(s string) string {
	s = scrubRes[0].ReplaceAllString(s, redacted)
	s = scrubRes[1].ReplaceAllString(s, redacted)
	s = scrubRes[2].ReplaceAllString(s, "${1} "+redacted)
	s = scrubRes[3].ReplaceAllString(s, "://"+redacted+"@")
	s = scrubRes[4].ReplaceAllString(s, redacted+"@")
	return s
}

// detailClip bounds Error.Detail.
const detailClip = 300

// ClipDetail scrubs s, trims it, and cuts it to 300 bytes on a rune
// boundary, appending "…" when cut.
func ClipDetail(s string) string {
	s = strings.TrimSpace(Scrub(s))
	if len(s) <= detailClip {
		return s
	}
	cut := detailClip
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
