package netenv

import (
	"strings"
	"testing"
)

func TestRedactProxy(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"http://user:p4ss@proxy.corp:3128/path?q=1", "http://proxy.corp:3128"},
		{"proxy.corp:3128", "http://proxy.corp:3128"},
		{"http://[::1]:8080", "http://[::1]:8080"},
		{"socks5://u:secret@h:1080", "socks5://h:1080"},
		{"http://user:p4ss@", "(unparsable)"},
		{"://p4ss@@@", "(unparsable)"},
	}
	for _, c := range cases {
		got := RedactProxy(c.raw)
		if got != c.want {
			t.Errorf("RedactProxy(%q) = %q, want %q", c.raw, got, c.want)
		}
		if strings.Contains(got, "p4ss") || strings.Contains(got, "secret") {
			t.Errorf("RedactProxy(%q) = %q leaks credentials", c.raw, got)
		}
	}
}

func TestProxyEnvOrderAndRedaction(t *testing.T) {
	long := strings.Repeat("a,", 100) // 200 chars
	vars := map[string]string{
		"HTTPS_PROXY": "http://u:pw@h:3128",
		"no_proxy":    long,
	}
	got := ProxyEnv(func(k string) string { return vars[k] })
	want := []ProxyVar{
		{Name: "HTTPS_PROXY", Value: "http://h:3128"},
		{Name: "no_proxy", Value: strings.Repeat("a,", 60) + "..."},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d vars, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("var %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if r := []rune(got[1].Value); len(r) != 123 {
		t.Fatalf("no_proxy value is %d runes, want 123 (120 + ...): %q", len(r), got[1].Value)
	}
}
