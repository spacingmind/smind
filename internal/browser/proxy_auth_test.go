package browser

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rawNoAuth sends one request to the proxy exactly as given (no credential is added).
func rawNoAuth(t *testing.T, r *proxyRig, req string) string {
	t.Helper()
	c, err := net.Dial("tcp", r.px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(c, req)
	b, _ := io.ReadAll(c)
	return string(b)
}

func basic(userPass string) string {
	return "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(userPass)) + "\r\n"
}

func TestProxyAuth_Table(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.AuthFailuresPerMinute = 1000 }) // the table fails on purpose
	live := newHitServer(t, nil)
	sec := rig.px.Secret()
	if len(sec) != 32 {
		t.Fatalf("a 128-bit secret is generated: %q", sec)
	}
	cases := []struct {
		name string
		hdr  string
		want string // status code prefix
	}{
		{"no header", "", "407"},
		{"wrong secret", basic("smind:" + strings.Repeat("0", 32)), "407"},
		{"right secret wrong user", basic("admin:" + sec), "407"},
		{"secret with a suffix", basic("smind:" + sec + "x"), "407"},
		{"a prefix of the secret", basic("smind:" + sec[:31]), "407"},
		{"empty password", basic("smind:"), "407"},
		{"user only", basic("smind"), "407"},
		{"bearer scheme", "Proxy-Authorization: Bearer " + sec + "\r\n", "407"},
		{"not base64", "Proxy-Authorization: Basic !!!\r\n", "407"},
		{"the raw secret, no scheme", "Proxy-Authorization: " + sec + "\r\n", "407"},
		{"plain Authorization instead", "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("smind:"+sec)) + "\r\n", "407"},
		{"right secret", basic("smind:" + sec), "200"},
		{"scheme is case-insensitive", "Proxy-Authorization: bAsIc " + base64.StdEncoding.EncodeToString([]byte("smind:"+sec)) + "\r\n", "200"},
	}
	forms := map[string]string{
		"absolute-form": fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n", live.port),
		"CONNECT":       fmt.Sprintf("CONNECT 127.0.0.1:%d HTTP/1.1\r\nHost: x\r\n", live.port),
	}
	for form, line := range forms {
		for _, c := range cases {
			resp := rawNoAuth(t, rig, line+c.hdr+"Connection: close\r\n\r\n")
			st := strings.Fields(resp + " ")
			got := ""
			if len(st) > 1 {
				got = st[1]
			}
			if got != c.want {
				t.Errorf("%s / %s: status %q, want %s\n%s", form, c.name, got, c.want, resp)
			}
			if c.want == "407" {
				if !strings.Contains(resp, `Proxy-Authenticate: Basic realm="smind"`) {
					t.Errorf("%s / %s: a 407 carries the challenge:\n%s", form, c.name, resp)
				}
				if strings.Contains(resp, sec) || strings.Contains(strings.ToLower(resp), "secret") {
					t.Errorf("%s / %s: the 407 leaks a hint:\n%s", form, c.name, resp)
				}
			}
		}
	}
}

// Nothing an unauthenticated client sends reaches the policy, the resolver or a dial.
func TestProxyAuth_UnauthenticatedNeverReachesTheDial(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, nil)
	live := newHitServer(t, nil)
	for _, req := range []string{
		fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n", live.port),
		fmt.Sprintf("CONNECT 127.0.0.1:%d HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n", live.port),
		"GET http://localhost/ HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n",
	} {
		_ = rawNoAuth(t, rig, req)
	}
	if got := rig.dials.list(); len(got) != 0 || live.hits.Load() != 0 {
		t.Fatalf("an unauthenticated client caused a dial: %v hits=%d", got, live.hits.Load())
	}
}

// The credential is the proxy's alone: the upstream never sees it, on the forward path.
func TestProxyAuth_UpstreamNeverSeesTheSecret(t *testing.T) {
	var seen http.Header
	up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Clone() })
	rig := newProxyRig(t, nil, proxyResolver, nil)
	resp := rig.raw(t, fmt.Sprintf("GET http://127.0.0.1:%d/ HTTP/1.1\r\nHost: x\r\n\r\n", up.port))
	if !strings.HasPrefix(resp, "HTTP/1.1 200") {
		t.Fatalf("authenticated request: %s", resp)
	}
	for k, v := range seen {
		if strings.Contains(strings.ToLower(k), "authorization") || strings.Contains(strings.Join(v, ","), rig.px.Secret()) {
			t.Fatalf("the upstream received %s: %v", k, v)
		}
	}
}

func TestProxyAuth_WrongCredentialFloodIsCutAndNotedOnce(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.AuthFailuresPerMinute = 3 })
	req := "GET http://localhost/ HTTP/1.1\r\nHost: x\r\n" + basic("smind:wrong") + "Connection: close\r\n\r\n"
	var answered, cut int
	for i := 0; i < 10; i++ {
		if resp := rawNoAuth(t, rig, req); strings.HasPrefix(resp, "HTTP/1.1 407") {
			answered++
		} else if resp == "" {
			cut++
		} else {
			t.Fatalf("unexpected answer: %q", resp)
		}
	}
	if answered != 3 || cut != 7 {
		t.Fatalf("3 answered with 407 then cut: answered=%d cut=%d", answered, cut)
	}
	if resp := rig.raw(t, "GET http://localhost/ HTTP/1.1\r\nHost: x\r\n\r\n"); strings.HasPrefix(resp, "HTTP/1.1 407") || resp == "" {
		t.Fatalf("the authenticated browser must not be cut: %q", resp)
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	n := 0
	for _, r := range rig.refuse {
		if strings.Contains(r, "wrong proxy credential") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the flood is noted once: %d in %v", n, rig.refuse)
	}
}

// Another local process spamming wrong credentials must not stop the browser's
// FIRST request (which carries no credential) from getting the 407 that makes it
// ask for the credential: that request is never cut.
func TestProxyAuth_SpamCannotLockTheBrowserOut(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.AuthFailuresPerMinute = 2 })
	bad := "GET http://localhost/ HTTP/1.1\r\nHost: x\r\n" + basic("smind:wrong") + "Connection: close\r\n\r\n"
	for i := 0; i < 12; i++ {
		_ = rawNoAuth(t, rig, bad)
	}
	first := "GET http://localhost/ HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n" // no credential yet
	if resp := rawNoAuth(t, rig, first); !strings.HasPrefix(resp, "HTTP/1.1 407") || !strings.Contains(resp, "Proxy-Authenticate") {
		t.Fatalf("a credential-less first request must still get the challenge after a spam: %q", resp)
	}
	for i := 0; i < 30; i++ { // and a credential-less flood is never cut either
		if resp := rawNoAuth(t, rig, first); !strings.HasPrefix(resp, "HTTP/1.1 407") {
			t.Fatalf("request %d without a credential was cut: %q", i, resp)
		}
	}
}

func TestProxyAuth_SecretIsNotPrinted(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, nil)
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(f, rig.px); strings.Contains(out, rig.px.Secret()) {
			t.Fatalf("%s of a Proxy shows the secret: %s", f, out)
		}
	}
	if rig.px.cfg.Secret != "" {
		t.Fatal("the config copy of the secret is cleared")
	}
	a, b := newProxyRig(t, nil, proxyResolver, nil), newProxyRig(t, nil, proxyResolver, nil)
	if a.px.Secret() == b.px.Secret() {
		t.Fatal("every proxy has its own secret")
	}
}

// A pinned secret for a test is honoured (so a session test can know it).
func TestProxyAuth_PinnedSecret(t *testing.T) {
	rig := newProxyRig(t, nil, proxyResolver, func(c *ProxyConfig) { c.Secret = "pinned-for-test" })
	if rig.px.Secret() != "pinned-for-test" {
		t.Fatal("pinned secret")
	}
	if resp := rig.raw(t, "GET http://localhost/ HTTP/1.1\r\nHost: x\r\n\r\n"); strings.HasPrefix(resp, "HTTP/1.1 407") {
		t.Fatalf("the pinned secret authenticates: %s", resp)
	}
}
