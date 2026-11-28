package netenv

import (
	"bytes"
	"log"
	"reflect"
	"strings"
	"testing"
)

// proxyLaunchCase is one effective environment and the exact entries
// ProxyLaunchEntries must append to it.
type proxyLaunchCase struct {
	name      string
	effective []string
	want      []string
}

// runProxyLaunchCases asserts each case's full result, entry for entry.
func runProxyLaunchCases(t *testing.T, cases []proxyLaunchCase) {
	t.Helper()
	for _, c := range cases {
		if got := ProxyLaunchEntries(c.effective); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ProxyLaunchEntries(%q) = %q, want %q", c.name, c.effective, got, c.want)
		}
	}
}

// loopbackEntries are the entries a proxy is always expected to add when
// neither no-proxy name exists yet.
var loopbackEntries = []string{"NO_PROXY=127.0.0.1,::1,localhost", "no_proxy=127.0.0.1,::1,localhost"}

func Test_proxy_launch_bare_host_port_gets_http_scheme(t *testing.T) {
	runProxyLaunchCases(t, []proxyLaunchCase{
		{"HTTPS_PROXY", []string{"HTTPS_PROXY=proxy.corp:3128"},
			append([]string{"HTTPS_PROXY=http://proxy.corp:3128"}, loopbackEntries...)},
		{"HTTP_PROXY", []string{"HTTP_PROXY=proxy.corp:3128"},
			append([]string{"HTTP_PROXY=http://proxy.corp:3128"}, loopbackEntries...)},
		{"https_proxy", []string{"https_proxy=proxy.corp:3128"},
			append([]string{"https_proxy=http://proxy.corp:3128"}, loopbackEntries...)},
		{"http_proxy", []string{"http_proxy=proxy.corp:3128"},
			append([]string{"http_proxy=http://proxy.corp:3128"}, loopbackEntries...)},
	})
}

func Test_proxy_launch_schemed_value_is_untouched(t *testing.T) {
	runProxyLaunchCases(t, []proxyLaunchCase{
		{"http", []string{"HTTPS_PROXY=http://proxy.corp:3128"}, loopbackEntries},
		{"https", []string{"HTTPS_PROXY=https://proxy.corp:3128"}, loopbackEntries},
		{"socks5", []string{"HTTP_PROXY=socks5://proxy.corp:1080"}, loopbackEntries},
	})
}

func Test_proxy_launch_userinfo_is_kept_and_never_logged(t *testing.T) {
	var logged bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(orig)

	entries := ProxyLaunchEntries([]string{"HTTPS_PROXY=user:p4ss@proxy.corp:3128"})

	want := append([]string{"HTTPS_PROXY=http://user:p4ss@proxy.corp:3128"}, loopbackEntries...)
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("ProxyLaunchEntries() = %q, want %q", entries, want)
	}
	if logged.Len() != 0 {
		t.Fatalf("the call logged %q, want nothing (a proxy value may carry user:pass@)", logged.String())
	}
	if strings.Contains(logged.String(), "p4ss") {
		t.Fatal("the log output carries the proxy credentials")
	}
	for _, e := range entries {
		if strings.Contains(e, "p4ss") && e != want[0] {
			t.Fatalf("p4ss appears outside the HTTPS_PROXY entry: %q", e)
		}
	}
}

func Test_proxy_launch_ipv6_bare_value(t *testing.T) {
	runProxyLaunchCases(t, []proxyLaunchCase{
		{"bare IPv6 literal", []string{"HTTP_PROXY=[::1]:8080"},
			append([]string{"HTTP_PROXY=http://[::1]:8080"}, loopbackEntries...)},
	})
}

func Test_proxy_launch_empty_values_are_untouched(t *testing.T) {
	// An empty value is not turned into a scheme entry while another proxy
	// is set.
	runProxyLaunchCases(t, []proxyLaunchCase{
		{"empty alongside a set proxy", []string{"ALL_PROXY=socks.corp:1080", "HTTP_PROXY=", "HTTPS_PROXY=   "},
			loopbackEntries},
	})
	// Only empty proxy values: no proxy is set, so nothing at all.
	if got := ProxyLaunchEntries([]string{"HTTP_PROXY=", "HTTPS_PROXY=   ", "ALL_PROXY=", "all_proxy="}); got != nil {
		t.Errorf("ProxyLaunchEntries(only empty proxy values) = %q, want nil", got)
	}
}

func Test_proxy_launch_no_proxy_gets_loopbacks_without_duplicates(t *testing.T) {
	runProxyLaunchCases(t, []proxyLaunchCase{
		{"existing value keeps its pieces and case", []string{"HTTPS_PROXY=http://p:1", "NO_PROXY=corp.example,LOCALHOST"},
			[]string{"NO_PROXY=corp.example,LOCALHOST,127.0.0.1,::1", "no_proxy=127.0.0.1,::1,localhost"}},
		{"nothing existing", []string{"HTTPS_PROXY=http://p:1"}, loopbackEntries},
		{"all three already present", []string{"HTTPS_PROXY=http://p:1", "NO_PROXY=127.0.0.1,::1,localhost", "no_proxy=LOCALHOST, 127.0.0.1 ,::1"}, nil},
	})
}

func Test_proxy_launch_mixed_case_names_are_independent(t *testing.T) {
	runProxyLaunchCases(t, []proxyLaunchCase{
		{"upper bare, lower schemed, NO_PROXY only", []string{
			"HTTP_PROXY=bare.corp:8080", "https_proxy=http://p:1", "NO_PROXY=corp.example"},
			[]string{
				"HTTP_PROXY=http://bare.corp:8080",
				"NO_PROXY=corp.example,127.0.0.1,::1,localhost",
				"no_proxy=127.0.0.1,::1,localhost"}},
	})
}

func Test_proxy_launch_all_proxy_alone_adds_loopback_only(t *testing.T) {
	runProxyLaunchCases(t, []proxyLaunchCase{
		{"ALL_PROXY", []string{"ALL_PROXY=host:1080"}, loopbackEntries},
		{"all_proxy", []string{"all_proxy=host:1080"}, loopbackEntries},
	})
}

func Test_proxy_launch_nothing_set_means_nothing_returned(t *testing.T) {
	if got := ProxyLaunchEntries(nil); got != nil {
		t.Errorf("ProxyLaunchEntries(nil) = %q, want nil", got)
	}
	if got := ProxyLaunchEntries([]string{"PATH=/bin", "NO_PROXY=corp.example"}); got != nil {
		t.Errorf("ProxyLaunchEntries(no proxy) = %q, want nil (NO_PROXY alone sets no proxy)", got)
	}
}

func Test_proxy_launch_last_entry_wins(t *testing.T) {
	runProxyLaunchCases(t, []proxyLaunchCase{
		// The last value already has a scheme: no scheme entry at all.
		{"last value is schemed", []string{"HTTPS_PROXY=a:1", "HTTPS_PROXY=http://b:2"}, loopbackEntries},
		// The last value is the bare one: it is what gets the scheme.
		{"last value is bare", []string{"HTTPS_PROXY=http://a:1", "HTTPS_PROXY=b:2"},
			append([]string{"HTTPS_PROXY=http://b:2"}, loopbackEntries...)},
	})
}
