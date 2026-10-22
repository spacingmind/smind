package forge

import "testing"

func TestPRURL_Validation(t *testing.T) {
	const host = "github.com"
	cases := []struct {
		name, raw, want string
	}{
		{"plain", "https://github.com/o/r/pull/7", "https://github.com/o/r/pull/7"},
		{"host case-insensitive", "https://GitHub.COM/o/r/pull/7", "https://GitHub.COM/o/r/pull/7"},
		{"query and fragment stripped", "https://github.com/o/r/pull/7?x=1#issuecomment-2", "https://github.com/o/r/pull/7"},
		{"trailing slash normalised", "https://github.com/o/r/pull/7/", "https://github.com/o/r/pull/7"},
		{"dots dashes underscores", "https://github.com/a.b-c/d_e.f/pull/12", "https://github.com/a.b-c/d_e.f/pull/12"},
		{"javascript scheme", "javascript:alert(1)", ""},
		{"javascript with host-looking text", "javascript://github.com/o/r/pull/7%0aalert(1)", ""},
		{"http", "http://github.com/o/r/pull/7", ""},
		{"userinfo", "https://user:pw@github.com/o/r/pull/7", ""},
		{"userinfo name only", "https://github.com@evil.example.com/o/r/pull/7", ""},
		{"other host", "https://evil.example.com/o/r/pull/7", ""},
		{"host suffix", "https://github.com.evil.example.com/o/r/pull/7", ""},
		{"host with port", "https://github.com:8443/o/r/pull/7", ""},
		{"trailing path", "https://github.com/o/r/pull/7/files", ""},
		{"issues not pulls", "https://github.com/o/r/issues/7", ""},
		{"non numeric", "https://github.com/o/r/pull/abc", ""},
		{"zero", "https://github.com/o/r/pull/0", ""},
		{"negative", "https://github.com/o/r/pull/-1", ""},
		{"extra leading segment", "https://github.com/x/o/r/pull/7", ""},
		{"dotdot owner", "https://github.com/../r/pull/7", ""},
		{"empty", "", ""},
		{"garbage", "::not a url", ""},
		{"relative", "/o/r/pull/7", ""},
	}
	for _, tc := range cases {
		got, ok := ValidPRURL(tc.raw, host)
		if tc.want == "" {
			if ok || got != "" {
				t.Errorf("%s: ValidPRURL(%q) = %q, %v, want rejection", tc.name, tc.raw, got, ok)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("%s: ValidPRURL(%q) = %q, %v, want %q", tc.name, tc.raw, got, ok, tc.want)
		}
	}
	if _, ok := ValidPRURL("https://github.com/o/r/pull/7", ""); ok {
		t.Error("empty host accepted")
	}
	if got, ok := ValidPRURL("https://ghe.corp.example/o/r/pull/7", "ghe.corp.example"); !ok || got != "https://ghe.corp.example/o/r/pull/7" {
		t.Errorf("enterprise host = %q, %v", got, ok)
	}
}
