package link

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

const loginTimeout = 10 * time.Second

// DialDirect connects to a direct transport: it logs in with password (when
// non-empty) to get a bearer token, then opens the peer's /ws with it. A
// wss route with a fingerprint is verified against that pin instead of the
// CA store. Errors never carry the password or the token.
func DialDirect(ctx context.Context, t Transport, password string) (Conn, error) {
	if t.Kind != KindDirect {
		return nil, fmt.Errorf("link: transport kind %q is not direct", t.Kind)
	}
	if _, err := CheckDirectRoute(t.URL, t.AllowCleartext); err != nil {
		return nil, err
	}
	u, err := url.Parse(t.URL)
	if err != nil {
		return nil, errors.New("link: invalid route url")
	}
	var tlsCfg *tls.Config
	if u.Scheme == "wss" && t.Fingerprint != "" {
		fp, err := hex.DecodeString(t.Fingerprint)
		if err != nil || len(fp) != sha256.Size {
			return nil, errors.New("link: route fingerprint is not a hex sha256")
		}
		tlsCfg = &tls.Config{
			InsecureSkipVerify:    true, //nolint:gosec // the pin replaces CA verification, see pinLeaf.
			VerifyPeerCertificate: pinLeaf(fp),
			MinVersion:            tls.VersionTLS12,
		}
	}
	header := http.Header{}
	if password != "" {
		token, err := login(ctx, u, password, tlsCfg)
		if err != nil {
			return nil, err
		}
		header.Set("Authorization", "Bearer "+token)
	}
	wsURL := *u
	wsURL.Path = strings.TrimSuffix(u.Path, "/") + "/ws"
	wsURL.RawPath = ""
	c, err := wsclient.DialURL(ctx, wsURL.String(), header, tlsCfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// login exchanges the password for a bearer token at /api/login on the same
// host. Failures are reported without the password or any response body.
func login(ctx context.Context, base *url.URL, password string, tlsCfg *tls.Config) (string, error) {
	lu := *base
	if base.Scheme == "wss" {
		lu.Scheme = "https"
	} else {
		lu.Scheme = "http"
	}
	lu.Path = strings.TrimSuffix(base.Path, "/") + "/api/login"
	lu.RawPath = ""
	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		return "", errors.New("link: encode login")
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lu.String(), bytes.NewReader(body))
	if err != nil {
		return "", errors.New("link: build login request")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout:   loginTimeout,
		Transport: &http.Transport{TLSClientConfig: tlsCfg, Proxy: http.ProxyFromEnvironment},
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("link: login to %s failed: %s", base.Host, errKind(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("link: login to %s refused (HTTP %d)", base.Host, resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Token == "" {
		return "", fmt.Errorf("link: login to %s returned no token", base.Host)
	}
	return out.Token, nil
}

// errKind describes a transport error without echoing the request URL.
func errKind(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// pinLeaf accepts only a server whose leaf certificate hashes to fp. The
// leaf is the one the handshake proves key possession for; extra chain
// certificates are attacker-controlled and ignored.
func pinLeaf(fp []byte) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) > 0 {
			if sum := sha256.Sum256(rawCerts[0]); bytes.Equal(sum[:], fp) {
				return nil
			}
		}
		return errors.New("link: certificate fingerprint does not match pin")
	}
}
