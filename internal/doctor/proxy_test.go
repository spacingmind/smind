package doctor

import (
	"strings"
	"testing"
)

func proxyEnv(vars map[string]string) Env {
	return Env{
		SmindHome: "/smind", Home: "/home/u", RetentionDays: 30,
		Getenv: func(k string) string { return vars[k] },
	}
}

func TestOutboundProxyFinding(t *testing.T) {
	// (a) nothing set -> no finding.
	if got := OutboundProxy(proxyEnv(nil)); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}

	// (b) https set with credentials -> one info finding, redacted.
	fs := OutboundProxy(proxyEnv(map[string]string{"HTTPS_PROXY": "http://u:topsecret@proxy.corp:3128"}))
	if len(fs) != 1 {
		t.Fatalf("want one finding, got %+v", fs)
	}
	f := fs[0]
	if f.ID != "network.proxy" || f.Level != LevelInfo || f.Provider != "network" {
		t.Fatalf("finding: %+v", f)
	}
	if !strings.Contains(f.Message, "HTTPS_PROXY=http://proxy.corp:3128") {
		t.Errorf("message misses redacted host: %q", f.Message)
	}
	if strings.Contains(f.Message, "topsecret") || strings.Contains(f.Message, "u:") {
		t.Errorf("message leaks credentials: %q", f.Message)
	}

	// (c) lower-case https_proxy with a non-http scheme -> warn with fix.
	fs = OutboundProxy(proxyEnv(map[string]string{"https_proxy": "socks5://h:1080"}))
	if len(fs) != 1 || fs[0].Level != LevelWarn {
		t.Fatalf("want warn, got %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "provider calls through a non-http proxy are refused") {
		t.Errorf("message: %q", fs[0].Message)
	}
	if fs[0].Fix != "use an http:// proxy URL, or unset the variable" {
		t.Errorf("fix: %q", fs[0].Fix)
	}

	// (d) a long NO_PROXY is truncated, keeping the message short.
	fs = OutboundProxy(proxyEnv(map[string]string{"NO_PROXY": strings.Repeat("h", 300)}))
	if len(fs) != 1 {
		t.Fatalf("want one finding, got %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "...") || len(fs[0].Message) >= 400 {
		t.Fatalf("NO_PROXY not truncated: %q", fs[0].Message)
	}

	// (e) a value that does not parse says so instead of blaming the scheme.
	fs = OutboundProxy(proxyEnv(map[string]string{"HTTPS_PROXY": "http://"}))
	if len(fs) != 1 || fs[0].Level != LevelWarn {
		t.Fatalf("want one warn finding, got %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "could not be parsed") {
		t.Errorf("message: %q", fs[0].Message)
	}
	if strings.Contains(fs[0].Message, "non-http") {
		t.Errorf("message blames the scheme for an unparsable value: %q", fs[0].Message)
	}
	if fs[0].Fix != "fix the proxy URL (scheme://host:port), or unset the variable" {
		t.Errorf("fix: %q", fs[0].Fix)
	}
	if !strings.Contains(fs[0].Message, "HTTPS_PROXY=(unparsable)") {
		t.Errorf("message does not show the redacted value: %q", fs[0].Message)
	}
}
