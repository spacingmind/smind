package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/relay/pairing"
	"github.com/spacingmind/smind/internal/relay/relaytest"
	"github.com/spacingmind/smind/internal/relay/server"
)

const connectSecret = "ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01ab01"

func fpOf(cert tls.Certificate) string {
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:])
}

type connectResult struct {
	code           int
	stdout, stderr string
	saved          bridge.Config
	ok             bool
}

// connect runs `relay connect` against addr with the given stdin, trusting
// roots as the system roots, and returns what it said and saved.
func connect(t *testing.T, roots *relaytest.CA, stdin string, args ...string) connectResult {
	t.Helper()
	dir := t.TempDir()
	var probe bridge.TLSProbe
	if roots != nil {
		probe.Roots = roots.Pool
	}
	var out, errOut bytes.Buffer
	code := runRelayConnect(args, dir, probe, strings.NewReader(stdin), &out, &errOut)
	cfg, ok, err := bridge.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return connectResult{code, out.String(), errOut.String(), cfg, ok}
}

func loopbackLeaf(t *testing.T, ca *relaytest.CA, o relaytest.LeafOptions) tls.Certificate {
	t.Helper()
	if o.IPs == nil && o.DNSNames == nil {
		o.IPs = []net.IP{net.ParseIP("127.0.0.1")}
	}
	cert, _, _, err := ca.Issue(o)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestRelayConnectTriesSystemRootsFirst(t *testing.T) {
	ca, err := relaytest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	addr := relaytest.ServeTLS(t, loopbackLeaf(t, ca, relaytest.LeafOptions{}))

	// No stdin at all: a CA-verified relay never asks anything.
	r := connect(t, ca, "", addr, "homelab", connectSecret)
	if r.code != 0 || !r.ok {
		t.Fatalf("connect = %d, saved %v; stderr %q", r.code, r.ok, r.stderr)
	}
	if r.saved.Fingerprint != "" {
		t.Fatalf("stored fingerprint %q for a CA-verified relay, want empty (CA mode)", r.saved.Fingerprint)
	}
	if strings.Contains(r.stdout, "[y/N]") || !strings.Contains(r.stdout, "verified by system roots") {
		t.Fatalf("stdout = %q, want a verified-by-system-roots note and no prompt", r.stdout)
	}
	if r.saved.RelayAddress != addr || r.saved.WorkspaceID != "homelab" || r.saved.SecretHex != connectSecret {
		t.Fatalf("saved config = %+v", r.saved)
	}

	// The same relay, when the system does not trust its CA, is asked about.
	r = connect(t, nil, "", addr, "homelab", connectSecret)
	if r.code == 0 || r.ok || !strings.Contains(r.stdout, "[y/N]") {
		t.Fatalf("without the CA in the roots: code %d saved %v stdout %q; want a refusal after a prompt", r.code, r.ok, r.stdout)
	}
}

func TestRelayConnectAsksBeforePinning(t *testing.T) {
	selfSigned, err := server.LoadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	addr := relaytest.ServeTLS(t, selfSigned)
	fp := fpOf(selfSigned)

	// Shown, then refused when stdin is closed (non-interactive) or says no.
	for name, stdin := range map[string]string{"closed stdin": "", "no": "n\n", "empty answer": "\n", "garbage": "maybe\n"} {
		r := connect(t, nil, stdin, addr, "homelab", connectSecret)
		if r.code != 1 || r.ok {
			t.Errorf("%s: code %d, saved %v; want 1 and nothing saved", name, r.code, r.ok)
		}
		if !strings.Contains(r.stdout, fp) || !strings.Contains(r.stdout, "[y/N]") {
			t.Errorf("%s: the prompt must show the fingerprint %s: %q", name, fp, r.stdout)
		}
		if !strings.Contains(r.stderr, "nothing was saved") {
			t.Errorf("%s: stderr = %q, want 'nothing was saved'", name, r.stderr)
		}
	}

	// Confirmed: pinned to exactly what was shown.
	for _, yes := range []string{"y\n", "yes\n", "Y\n"} {
		r := connect(t, nil, yes, addr, "homelab", connectSecret)
		if r.code != 0 || !r.ok || r.saved.Fingerprint != fp {
			t.Errorf("answer %q: code %d saved %+v; want a pin of %s", yes, r.code, r.saved, fp)
		}
	}
}

func TestRelayConnectAsksForCertificatesThatDoNotVerify(t *testing.T) {
	ca, err := relaytest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		cert tls.Certificate
		want string
	}{
		"hostname mismatch": {loopbackLeaf(t, ca, relaytest.LeafOptions{DNSNames: []string{"relay.example.test"}}), "IP SANs"},
		"expired": {loopbackLeaf(t, ca, relaytest.LeafOptions{
			NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(-24 * time.Hour),
		}), "expired"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			addr := relaytest.ServeTLS(t, c.cert)
			// Trusted CA, bad certificate: still not silently accepted.
			r := connect(t, ca, "n\n", addr, "homelab", connectSecret)
			if r.code == 0 || r.ok || !strings.Contains(r.stdout, "[y/N]") || !strings.Contains(r.stdout, c.want) {
				t.Fatalf("code %d saved %v stdout %q; want a refusal that names %q", r.code, r.ok, r.stdout, c.want)
			}
			// Confirming pins that certificate (and nothing about the CA).
			r = connect(t, ca, "y\n", addr, "homelab", connectSecret)
			if r.code != 0 || r.saved.Fingerprint != fpOf(c.cert) {
				t.Fatalf("confirmed: code %d saved %+v, want a pin of the presented leaf", r.code, r.saved)
			}
		})
	}
}

func TestRelayConnectFingerprintFlag(t *testing.T) {
	selfSigned, err := server.LoadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	addr := relaytest.ServeTLS(t, selfSigned)
	fp := fpOf(selfSigned)

	// The right fingerprint pins without a prompt (stdin is closed), whether
	// the flag comes before or after the positionals.
	for _, args := range [][]string{
		{"--fingerprint", fp, addr, "homelab", connectSecret},
		{addr, "homelab", connectSecret, "--fingerprint", fp},
		{"--fingerprint=" + strings.ToUpper(fp), addr, "homelab", connectSecret},
	} {
		r := connect(t, nil, "", args...)
		if r.code != 0 || r.saved.Fingerprint != fp || strings.Contains(r.stdout, "[y/N]") {
			t.Errorf("%v: code %d saved %+v stdout %q", args, r.code, r.saved, r.stdout)
		}
	}

	// A different fingerprint is refused: what the relay presents must match.
	other, _ := server.LoadOrCreateCert(t.TempDir())
	r := connect(t, nil, "y\n", "--fingerprint", fpOf(other), addr, "homelab", connectSecret)
	if r.code != 1 || r.ok || !strings.Contains(r.stderr, "nothing was saved") {
		t.Errorf("mismatch: code %d saved %v stderr %q", r.code, r.ok, r.stderr)
	}
	// Malformed.
	for _, bad := range []string{"xyz", "abcd", strings.Repeat("g", 64)} {
		if r := connect(t, nil, "", "--fingerprint", bad, addr, "homelab", connectSecret); r.code != 2 || r.ok {
			t.Errorf("--fingerprint %q: code %d saved %v, want 2 and nothing saved", bad, r.code, r.ok)
		}
	}
}

func TestRelayConnectArgumentsAndUnreachableRelay(t *testing.T) {
	if r := connect(t, nil, "", "only-an-address"); r.code != 2 {
		t.Errorf("too few arguments: code %d, want 2", r.code)
	}
	if r := connect(t, nil, "", "127.0.0.1:1", "h", "not-hex"); r.code != 2 {
		t.Errorf("bad secret: code %d, want 2", r.code)
	}
	// Nothing listening: an error, not a prompt (there is no certificate to confirm).
	r := connect(t, nil, "y\n", "127.0.0.1:1", "h", connectSecret)
	if r.code != 1 || r.ok || strings.Contains(r.stdout, "[y/N]") {
		t.Errorf("unreachable: code %d saved %v stdout %q", r.code, r.ok, r.stdout)
	}
}

// offerPayload decodes the JSON inside an offer URL's fragment.
func offerPayload(t *testing.T, offerURL string) map[string]any {
	t.Helper()
	u, err := url.Parse(offerURL)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Fragment, "offer="))
	if err != nil {
		t.Fatalf("offer fragment %q: %v", u.Fragment, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOfferOmitsFPForCAVerifiedRelay(t *testing.T) {
	for _, tc := range []struct {
		name, fp string
		wantFP   bool
	}{{"CA relay", "", false}, {"pinned relay", strings.Repeat("cd", 32), true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := bridge.SaveConfig(dir, bridge.Config{
				RelayAddress: "relay.example.test:7400", WorkspaceID: "homelab", SecretHex: connectSecret, Fingerprint: tc.fp,
			}); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			if code := runRelayOffer(nil, dir, &out, &errOut, false); code != 0 {
				t.Fatalf("offer = %d: %s", code, errOut.String())
			}
			offerURL := strings.TrimSpace(out.String())
			payload := offerPayload(t, offerURL)
			if _, has := payload["fp"]; has != tc.wantFP {
				t.Fatalf("offer payload %v: has fp = %v, want %v", payload, has, tc.wantFP)
			}
			// It still parses (an empty fp is valid) and says so.
			parsed, err := pairing.ParseURL(offerURL)
			if err != nil || parsed.RelayFingerprint != tc.fp {
				t.Fatalf("ParseURL = %+v, %v; want fingerprint %q", parsed, err, tc.fp)
			}
		})
	}
}

func TestRelayTLSCertAndKeyFlags(t *testing.T) {
	var errOut bytes.Buffer
	cfg, code := parseRelayServeFlags([]string{"--tls-cert", "/etc/relay/fullchain.pem", "--tls-key", "/etc/relay/privkey.pem"}, &errOut)
	if code != 0 || cfg.TLSCertFile != "/etc/relay/fullchain.pem" || cfg.TLSKeyFile != "/etc/relay/privkey.pem" {
		t.Fatalf("both flags: code %d cfg %+v err %q", code, cfg, errOut.String())
	}
	if cfg, code = parseRelayServeFlags(nil, &errOut); code != 0 || cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
		t.Fatalf("no flags: code %d cfg %+v", code, cfg)
	}
	for _, args := range [][]string{{"--tls-cert", "c.pem"}, {"--tls-key", "k.pem"}} {
		errOut.Reset()
		if _, code := parseRelayServeFlags(args, &errOut); code != 2 || !strings.Contains(errOut.String(), "must be given together") {
			t.Errorf("%v: code %d err %q, want 2 and 'must be given together'", args, code, errOut.String())
		}
	}
	var help bytes.Buffer
	printRelayUsage(&help)
	for _, want := range []string{"--tls-cert", "--tls-key", "--fingerprint", "never\npins a certificate silently"} {
		if !strings.Contains(help.String(), want) {
			t.Errorf("relay help lacks %q", want)
		}
	}
}
