package accounts

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/oauth2"
)

func Test_is_terminal_refresh_error_table(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"retrieve invalid_grant", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, true},
		{"wrapped retrieve invalid_grant", fmt.Errorf("x: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}), true},
		{"retrieve other code", &oauth2.RetrieveError{ErrorCode: "temporarily_unavailable"}, false},
		{"marker invalid_grant", errors.New("body: invalid_grant"), true},
		{"marker expired", errors.New("refresh_token_expired"), true},
		{"marker reused", errors.New("refresh_token_reused"), true},
		{"marker invalidated", errors.New("refresh_token_invalidated"), true},
		{"network", errors.New("dial tcp: i/o timeout"), false},
	}
	for _, c := range cases {
		if got := IsTerminalRefreshError(c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
