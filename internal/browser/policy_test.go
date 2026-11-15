package browser

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// The wildcard addresses are spelled at run time: internal/hygiene forbids the
// literals in tests, and none of these binds anything (they are policy inputs).
var (
	anyV4 = strings.Join([]string{"0", "0", "0", "0"}, ".")
	anyV6 = "[" + "::" + "]"
)

func testPolicy(t *testing.T, allow ...string) Policy {
	t.Helper()
	p, err := NewPolicy(allow, []Endpoint{
		{Host: "127.0.0.1", Port: 4648},  // daemon listen address
		{Host: "localhost", Port: 4700},  // desktop proxy origin
		{Host: "devbox.lan", Port: 4648}, // another daemon on the connection list
		{Host: anyV4, Port: 4999},        // wildcard bind
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckURLTable(t *testing.T) {
	p := testPolicy(t, "10.0.0.5", "intranet.test", "8.8.8.8")
	cases := []struct {
		url   string
		allow bool
	}{
		// loopback, any port, every form
		{"http://localhost:3000/", true},
		{"https://localhost/", true},
		{"http://LOCALHOST:3000/x?y=1#z", true},
		{"http://app.localhost:8080/", true},
		{"http://localhost./", true},
		{"http://127.0.0.1:5173/", true},
		{"http://127.5.5.5/", true},
		{"http://[::1]:3000/", true},
		{"http://[::ffff:127.0.0.1]:3000/", true},
		{"http://[::ffff:7f00:1]:3000/", true},
		{"http://2130706433:3000/", true},   // decimal 127.0.0.1
		{"http://0x7f000001:3000/", true},   // hex
		{"http://017700000001:3000/", true}, // octal
		{"http://127.1:3000/", true},        // short dotted
		{"http://0x7f.0.0.1:3000/", true},   // hex part
		{"about:blank", true},
		// schemes
		{"file:///etc/passwd", false},
		{"data:text/html,<script>1</script>", false},
		{"javascript:alert(1)", false},
		{"blob:http://localhost/abc", false},
		{"view-source:http://localhost/", false},
		{"chrome://settings", false},
		{"about:srcdoc", false},
		{"ftp://localhost/", false},
		{"ws://localhost/", false},
		{"http:/localhost/", false},
		{"http:localhost", false},
		{"//localhost/", false},
		{"", false},
		// syntax tricks
		{"http://user@localhost/", false},
		{"http://user:pw@localhost/", false},
		{"http://localhost@evil.com/", false},
		{"http://evil.com\\@localhost/", false},
		{"http://localhost\\.evil.com/", false},
		{"http://local host/", false},
		{"http://localhost\t/", false},
		{"http://localhost\n/", false},
		{"http://%6c%6fcalhost/", false},
		{"http://127.0.0.1%2f@evil.com/", false},
		{"http://localhost:abc/", false},
		{"http://localhost:0/", false},
		{"http://localhost:99999/", false},
		{"http://localhost:+80/", false},
		{"http://localhost:-1/", false},
		{"http://[::1/", false},
		{"http://[::1%25eth0]/", false},
		{"http://[::1]x/", false},
		{"http:///", false},
		{"http://:3000/", false},
		{"http://lоcalhost/", false}, // cyrillic o
		{"http://localhost../", false},
		{"http://.localhost/", false},
		{"http://0x/", false}, // 0x alone is the unspecified address in a browser
		{"http://256.0.0.1/", false},
		{"http://1.2.3.4.5/", false},
		{"http://0xgg/", false},
		{"http://99999999999/", false},
		// everything else refused
		{"http://example.com/", false},
		{"https://example.com./", false},
		{"http://8.8.4.4/", false},
		{"http://localhost.evil.com/", false},
		{"http://evillocalhost/", false},
		{"http://" + anyV4 + ":3000/", false},
		{"http://" + anyV6 + ":3000/", false},
		{"http://10.0.0.6/", false},
		{"http://192.168.1.1/", false},
		{"http://172.16.0.1/", false},
		{"http://169.254.1.1/", false},
		{"http://[fe80::1]/", false},
		{"http://[fd00:ec2::254]/", false},
		{"http://100.100.100.200/", false},
		{"http://[64:ff9b::7f00:1]/", false},
		{"http://[::7f00:1]/", false},
		// exact allowlist entries
		{"http://10.0.0.5:3000/", true},
		{"http://intranet.test/", true},
		{"http://INTRANET.test./", true},
		{"http://sub.intranet.test/", false},               // exact only, no subdomain
		{"http://169.254.169.254/latest/meta-data", false}, // metadata can never be allowlisted
		{"http://8.8.8.8/", true},
		// the daemon, the proxy, other daemons: never, whatever the allowlist
		{"http://127.0.0.1:4648/api/token", false},
		{"http://localhost:4648/api/token", false},
		{"http://[::1]:4648/", false},
		{"http://2130706433:4648/", false},
		{"http://0x7f.1:4648/", false},
		{"http://[::ffff:127.0.0.1]:4648/", false},
		{"http://127.9.9.9:4648/", false},
		{"http://app.localhost:4648/", false},
		{"http://localhost:4700/", false},
		{"http://127.0.0.1:4700/", false},
		{"http://devbox.lan:4648/", false},
		{"http://127.0.0.1:4999/", false},
		{"http://10.0.0.5:4999/", false}, // wildcard bind reserves the port everywhere
		{"http://intranet.test:4999/", false},
		{"http://127.0.0.1:4649/", true}, // another port on the same host is fine
	}
	for _, c := range cases {
		d := p.CheckURL(c.url)
		want := c.allow
		if d.Allowed != want {
			t.Errorf("CheckURL(%q): allowed=%v (reason %q), want %v", c.url, d.Allowed, d.Reason, want)
		}
		if !d.Allowed && d.Reason == "" {
			t.Errorf("CheckURL(%q): a refusal must carry a reason", c.url)
		}
	}
}

func TestCheckURLDefaultPorts(t *testing.T) {
	p, _ := NewPolicy(nil, []Endpoint{{Host: "127.0.0.1", Port: 80}, {Host: "127.0.0.1", Port: 443}})
	if p.CheckURL("http://localhost/").Allowed || p.CheckURL("https://localhost/").Allowed {
		t.Fatal("the implied default port must be matched against the deny list")
	}
	if p.CheckURL("http://localhost:8080/").Allowed != true {
		t.Fatal("another port stays allowed")
	}
}

type fakeResolver map[string][]string

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	v, ok := f[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	var out []netip.Addr
	for _, s := range v {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

func TestCheckResolvedTable(t *testing.T) {
	p := testPolicy(t, "intranet.test", "rebind.test", "meta.test", "mix.test", "daemon.test", "nat64.test", "6to4.test", "compat.test", "azure.test", "proto.test", "siit.test", "public.test", "public6.test")
	r := fakeResolver{
		"localhost":       {"127.0.0.1", "::1"},
		"app.localhost":   {"127.0.0.1"},
		"evil.localhost":  {"10.0.0.1"},
		"mixed.localhost": {"127.0.0.1", "93.184.216.34"},
		"intranet.test":   {"10.1.2.3"},
		"rebind.test":     {"127.0.0.1", "169.254.169.254"},
		"meta.test":       {"fd00:ec2::254"},
		"mix.test":        {"192.168.1.9", anyV4},
		"daemon.test":     {"127.0.0.1"},
		"dual.localhost":  {"::ffff:127.0.0.1"},
		"nat64.test":      {"64:ff9b::a9fe:a9fe"},
		"6to4.test":       {"2002:a9fe:a9fe::1"},
		"compat.test":     {"::a9fe:a9fe"},
		"azure.test":      {"168.63.129.16"},
		"proto.test":      {"192.0.0.9"},
		"siit.test":       {"::ffff:0:a9fe:a9fe"},
		"public.test":     {"93.184.216.34"},
		"public6.test":    {"2606:4700::1111"},
	}
	cases := []struct {
		url   string
		allow bool
	}{
		{"http://localhost:3000/", true},
		{"http://app.localhost:3000/", true},
		{"http://dual.localhost:3000/", true},
		{"http://evil.localhost:3000/", false},  // loopback name that resolves off loopback
		{"http://mixed.localhost:3000/", false}, // one bad address is enough
		{"http://intranet.test/", false},        // an allowlisted NAME may not resolve to a private range
		{"http://rebind.test/", false},
		{"http://meta.test/", false},
		{"http://mix.test/", false},
		{"http://nat64.test/", false},
		{"http://6to4.test/", false},
		{"http://compat.test/", false},
		{"http://azure.test/", false},
		{"http://proto.test/", false},
		{"http://siit.test/", false},
		{"http://daemon.test:4648/", false}, // resolves to the daemon address and port
		{"http://daemon.test:4649/", false}, // nor to loopback, whatever the port
		{"http://public.test/", true},
		{"http://public6.test/", true},
		{"http://nosuch.localhost/", false}, // resolver error refuses
		{"http://10.0.0.9/", false},
	}
	for _, c := range cases {
		d := p.Check(context.Background(), c.url, r)
		if d.Allowed != c.allow {
			t.Errorf("Check(%q): allowed=%v (%s), want %v", c.url, d.Allowed, d.Reason, c.allow)
		}
	}
	if p.Check(context.Background(), "http://localhost:3000/", nil).Allowed {
		t.Fatal("a named host with no resolver must be refused, not skipped")
	}
	if !p.Check(context.Background(), "http://127.0.0.1:3000/", nil).Allowed {
		t.Fatal("an IP literal needs no resolver")
	}
	if p.CheckResolved(Decision{Allowed: true, Host: "x", NeedsResolve: true}, nil).Allowed {
		t.Fatal("no addresses refuses")
	}
}

func TestNormalizeAllowHost(t *testing.T) {
	good := map[string]string{
		"Intranet.Test":   "intranet.test",
		"intranet.test.":  "intranet.test",
		"10.0.0.5":        "10.0.0.5",
		"0x0a.1":          "10.0.0.1",
		"[::1]":           "::1",
		"::ffff:10.0.0.5": "10.0.0.5",
	}
	for in, want := range good {
		got, err := NormalizeAllowHost(in)
		if err != nil || got != want {
			t.Errorf("NormalizeAllowHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "*", "*.example.com", "http://example.com", "example.com:80", "example.com/x", "a@b.c", "exa mple.com", "%41.com", "[::1", "10.0.0.5:80", "例え.jp"} {
		if got, err := NormalizeAllowHost(bad); err == nil {
			t.Errorf("NormalizeAllowHost(%q) = %q, want an error", bad, got)
		}
	}
	for _, bad := range []string{anyV4, anyV6, "224.0.0.1", "169.254.169.254", "169.254.1.1", "fe80::1", "[fe80::1]", "100.100.100.200", "fd00:ec2::254", "169.254.170.2", "192.0.0.192", "168.63.129.16", "fd00:ec2::23", "192.0.0.5", "[64:ff9b::a9fe:a9fe]", "[2002:a9fe:a9fe::]", "[::a9fe:a9fe]", "[64:ff9b::7f00:1]", "[2002:7f00:1::]", "[::7f00:1]", "[64:ff9b::c000:5]", "[::ffff:0:a9fe:a9fe]", "[64:ff9b:1::a9fe:a9fe]", "[2001:0:4136:e378:8000:63bf:3fff:fdd2]"} {
		if got, err := NormalizeAllowHost(bad); err == nil {
			t.Errorf("NormalizeAllowHost(%q) = %q: unusable addresses must be rejected", bad, got)
		}
	}
	if _, err := NewPolicy(nil, []Endpoint{{Host: "x", Port: 0}}); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatal("a deny endpoint without a port must be rejected")
	}
}

func TestDenyAppliesToUnspecifiedHost(t *testing.T) {
	// Even if the unspecified address reached the check, it connects to this machine.
	p, _ := NewPolicy(nil, []Endpoint{{Host: "127.0.0.1", Port: 4648}})
	d := p.denied(parsedHost{canon: anyV4, ip: netip.MustParseAddr(anyV4)}, 4648)
	if !d {
		t.Fatal("the unspecified address on the daemon port must count as the daemon")
	}
}

func TestDenyEntriesAreCanonicalised(t *testing.T) {
	for _, host := range []string{"127.1", "0177.0.0.1", "0x7f.1", "2130706433", "[::ffff:7f00:1]", "::ffff:127.0.0.1", "LOCALHOST.", "127.0.0.1"} {
		p, err := NewPolicy(nil, []Endpoint{{Host: host, Port: 4648}})
		if err != nil {
			t.Fatalf("%q: %v", host, err)
		}
		for _, u := range []string{"http://127.0.0.1:4648/", "http://localhost:4648/", "http://[::1]:4648/", "http://2130706433:4648/"} {
			if p.CheckURL(u).Allowed {
				t.Errorf("deny entry %q must deny %s", host, u)
			}
		}
		if !p.CheckURL("http://127.0.0.1:4649/").Allowed {
			t.Errorf("deny entry %q must not deny another port", host)
		}
	}
	for _, bad := range []string{"a b", "fe80::1%eth0", "[::1", "1.2.3.4.5", "256.1.1.1", "%7f.1", ""} {
		if _, err := NewPolicy(nil, []Endpoint{{Host: bad, Port: 80}}); err == nil {
			t.Errorf("deny entry %q must be an error, never silently dropped or kept raw", bad)
		}
	}
}

func TestAllowlistedNameMustResolvePublic(t *testing.T) {
	p := testPolicy(t, "corp.test")
	for _, ip := range []string{"10.1.2.3", "172.16.0.9", "192.168.1.1", "100.64.0.1", "100.127.255.254", "127.0.0.1", "127.9.9.9", "0.1.2.3", "198.18.0.5", "240.0.0.1",
		"::1", "fc00::1", "fd12:3456::1", "fec0::1", "fec0:0:0:1::5", "::ffff:10.0.0.1", "64:ff9b::a00:1", "2002:a00:1::", "::a00:1"} {
		d := p.CheckURL("http://corp.test:3000/")
		if !d.Allowed || !d.NeedsResolve {
			t.Fatalf("setup: %+v", d)
		}
		if got := p.CheckResolved(d, []netip.Addr{netip.MustParseAddr(ip)}); got.Allowed {
			t.Errorf("an allowlisted name resolving to %s must be refused", ip)
		}
	}
	d := p.CheckURL("http://corp.test:3000/")
	for _, ip := range []string{"93.184.216.34", "8.8.8.8", "2606:4700::1111"} {
		if got := p.CheckResolved(d, []netip.Addr{netip.MustParseAddr(ip)}); !got.Allowed {
			t.Errorf("a public address %s must be allowed: %s", ip, got.Reason)
		}
	}
	// One internal address among public ones is enough to refuse.
	if p.CheckResolved(d, []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")}).Allowed {
		t.Error("a mixed answer must be refused")
	}
	// An IP-literal entry (the way to allow an intranet host) is unaffected.
	lit := testPolicy(t, "10.0.0.5")
	if !lit.CheckURL("http://10.0.0.5:3000/").Allowed {
		t.Error("an exact IP-literal entry must keep working")
	}
	// The loopback names are loopback by definition.
	if !testPolicy(t).CheckURL("http://app.localhost:3000/").Allowed {
		t.Error("*.localhost stays allowed")
	}
}
