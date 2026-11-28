package accounts

import (
	"errors"
	"strings"

	"golang.org/x/oauth2"
)

// ErrNeedsRelogin means the account's refresh token was refused for good: the
// person has to sign in to the account again.
var ErrNeedsRelogin = errors.New("sign in again: this account's refresh token is no longer accepted")

// terminalRefreshMarkers are substrings of a refresher's error text that mean
// the refresh token itself is dead (the hand-written refreshers embed the
// provider's response body in the error). Used for classification only; the
// text is never copied out.
var terminalRefreshMarkers = []string{
	"invalid_grant",
	"refresh_token_expired",
	"refresh_token_reused",
	"refresh_token_invalidated",
}

// IsTerminalRefreshError reports whether err is the provider refusing the
// refresh token for good, as opposed to a transient failure.
func IsTerminalRefreshError(err error) bool {
	if err == nil {
		return false
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
		return true
	}
	msg := err.Error()
	for _, m := range terminalRefreshMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
