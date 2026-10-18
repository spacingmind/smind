package accounts

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePastedRedirect_AcceptsBareQueryAndCodeHashForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  callbackResult
	}{
		{"full url", "http://localhost:54545/callback?code=C1&state=S1", callbackResult{code: "C1", state: "S1"}},
		{"full url any host and scheme", "https://example.com/x/y?state=S1&code=C1&extra=1", callbackResult{code: "C1", state: "S1"}},
		{"url with fragment query", "http://localhost:1455/auth/callback#code=C1&state=S1", callbackResult{code: "C1", state: "S1"}},
		{"url query wins over fragment", "http://h/cb?code=C1&state=S1#code=C2&state=S2", callbackResult{code: "C1", state: "S1"}},
		{"schemeless url", "localhost:54545/callback?code=C1&state=S1", callbackResult{code: "C1", state: "S1"}},
		{"bare query", "code=C1&state=S1", callbackResult{code: "C1", state: "S1"}},
		{"bare query with leading question mark", "?code=C1&state=S1", callbackResult{code: "C1", state: "S1"}},
		{"percent-encoded values", "code=a%2Fb&state=c%3Dd", callbackResult{code: "a/b", state: "c=d"}},
		{"code hash", "C1#S1", callbackResult{code: "C1", state: "S1"}},
		{"code hash with surrounding whitespace", "  \tC1#S1\n", callbackResult{code: "C1", state: "S1"}},
		{"code hash with base64url characters", "abc-DEF_123#xyz-789_", callbackResult{code: "abc-DEF_123", state: "xyz-789_"}},
		{"vendor error", "http://localhost:1455/auth/callback?error=access_denied&state=S1", callbackResult{state: "S1", errCode: "access_denied"}},
		{"vendor error in bare query", "error=access_denied", callbackResult{errCode: "access_denied"}},
		{"free-text error is not echoed", "error=Totally%20Not%20A%20Code!", callbackResult{errCode: "error"}},
	}
	t.Run("oauthComplete accepts bare query and code-hash forms", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got, err := parsePastedRedirect(tt.input)
				if err != nil {
					t.Fatalf("parsePastedRedirect(%q) error = %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("parsePastedRedirect(%q) = %+v, want %+v", tt.input, got, tt.want)
				}
			})
		}
	})
}

func TestParsePastedRedirect_Rejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace only", " \n\t "},
		{"too long", strings.Repeat("a", 5000)},
		{"too long code hash", strings.Repeat("a", 3000) + "#" + strings.Repeat("b", 3000)},
		{"garbage", "this is not a redirect"},
		{"empty code", "code=&state=S1"},
		{"empty code in url", "http://localhost:54545/callback?code=&state=S1"},
		{"no code", "state=S1"},
		{"url without query", "http://localhost:54545/callback"},
		{"empty state half of code hash", "C1#"},
		{"empty code half of code hash", "#S1"},
		{"two hashes", "C1#S1#S2"},
		{"bad escape", "code=%zz&state=S1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePastedRedirect(tt.input)
			var le *LoginError
			if !errors.As(err, &le) || le.Code != LoginCodeBadRedirect {
				t.Fatalf("parsePastedRedirect(%.40q) error = %v, want a %s LoginError", tt.input, err, LoginCodeBadRedirect)
			}
			if strings.Contains(err.Error(), tt.input) && len(tt.input) > 3 {
				t.Errorf("error %q echoes the input", err.Error())
			}
		})
	}
}
