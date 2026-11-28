package server

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func Test_origin_policy_allows_no_origin_and_listed_origins_only(t *testing.T) {
	p := newOriginPolicy([]string{"https://operator.example:8443"}, false)
	r := httptest.NewRequest("POST", "/", nil)
	if !p.check(r) {
		t.Fatal("request without Origin refused")
	}
	for _, origin := range []string{DefaultWebOrigin, "https://operator.example:8443", "HTTPS://APP.SPACINGMIND.COM"} {
		r.Header.Set("Origin", origin)
		if !p.check(r) {
			t.Errorf("listed origin %q refused", origin)
		}
	}
	r.Header.Set("Origin", "https://unlisted.example")
	if p.check(r) {
		t.Fatal("unlisted origin accepted")
	}
	r.Header.Add("Origin", DefaultWebOrigin)
	if p.check(r) {
		t.Fatal("multiple origins accepted")
	}
	if got := EffectiveOrigins([]string{DefaultWebOrigin, "https://a.com", "https://a.com"}, false); !reflect.DeepEqual(got, []string{DefaultWebOrigin, "https://a.com"}) {
		t.Fatalf("effective origins = %v", got)
	}
	if got := EffectiveOrigins([]string{"https://a.com", "https://a.com"}, true); !reflect.DeepEqual(got, []string{"https://a.com"}) {
		t.Fatalf("no-default origins = %v", got)
	}
	if newOriginPolicy(nil, true).allows(DefaultWebOrigin) {
		t.Fatal("default accepted when disabled")
	}
}

func Test_origin_policy_rejects_null_and_lookalikes(t *testing.T) {
	p := newOriginPolicy(nil, false)
	for _, origin := range []string{"null", "", "app.spacingmind.com.evil.org", "https://app.spacingmind.com.evil.org", "https://app.spacingmind.com:8443", "https://app.spacingmind.com:443", "http://app.spacingmind.com", "https://evilapp.spacingmind.com", DefaultWebOrigin + "/", DefaultWebOrigin + "/path", "https://u@app.spacingmind.com", DefaultWebOrigin + "?x", DefaultWebOrigin + "#x", " " + DefaultWebOrigin} {
		t.Run(origin, func(t *testing.T) {
			if p.allows(origin) {
				t.Errorf("accepted %q", origin)
			}
		})
	}
}

func Test_operator_origins_are_validated(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"https://a.com", "https://a.com"}, {" HTTPS://A.COM:8443 ", "https://a.com:8443"}, {"http://localhost:3000", "http://localhost:3000"}, {"http://127.0.0.1", "http://127.0.0.1"}, {"http://[::1]:3000", "http://[::1]:3000"}, {"https://[2001:db8::1]:443", "https://[2001:db8::1]:443"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := ParseAllowedOrigins([]string{tc.value})
			if err != nil || !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("got %v, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, value := range []string{"", " ", "http://example.com", "http://localhost.evil.org", "http://127.0.0.2", "https://*.x.org", "https://a.com/path", "https://a.com/", "https://u@a.com", "https://a.com?x", "https://a.com?", "https://a.com#", "https://a.com#x", "null", "a.com", "ftp://a.com", "https://", "https://a.com:", "https://a.com:abc", "https://a.com:65536", "https://::1", "https://[a.com]", "https://a.com,b.com", "https://a.com https://b.com"} {
		t.Run(value, func(t *testing.T) {
			if got, err := ParseAllowedOrigins([]string{value}); err == nil || !strings.Contains(err.Error(), value) {
				t.Fatalf("got %v, %v; want error naming %q", got, err, value)
			}
		})
	}
	for _, values := range [][]string{nil, {}} {
		if got, err := ParseAllowedOrigins(values); got != nil || err != nil {
			t.Fatalf("empty input: %v, %v", got, err)
		}
	}
}
