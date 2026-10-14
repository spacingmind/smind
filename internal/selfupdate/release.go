package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the GitHub repository releases are fetched from.
	DefaultBaseURL = "https://github.com/spacingmind/smind"

	// BaseURLEnv redirects every release lookup and download to another
	// server with the same layout as GitHub's (`/releases/latest`,
	// `/releases/download/v<version>/<asset>`). It exists for tests (an
	// httptest fake) and is unset in production; it is not a mirror feature.
	BaseURLEnv = "SMIND_UPDATE_BASE_URL"

	maxChecksumsSize = 1 << 20
	maxArchiveSize   = 512 << 20
	maxRedirects     = 10
)

// ErrAssetNotFound means the release server answered 404 for a download: the
// release does not exist or has no asset for this OS/arch.
var ErrAssetNotFound = errors.New("not found")

// Source fetches release metadata and assets.
type Source struct {
	BaseURL string
	Client  *http.Client
}

// NewSource builds the production Source: GitHub, unless BaseURLEnv says
// otherwise. Its transport honours HTTPS_PROXY/HTTP_PROXY/NO_PROXY.
func NewSource() *Source {
	base := strings.TrimRight(os.Getenv(BaseURLEnv), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &Source{BaseURL: base, Client: newHTTPClient()}
}

func newHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	tr.DisableCompression = true // the checksum is over the bytes the release published, not a transparently gunzipped body
	return &http.Client{Transport: tr, Timeout: 10 * time.Minute, CheckRedirect: noHTTPSDowngrade}
}

// noHTTPSDowngrade follows redirects (GitHub bounces asset downloads to its
// object store) but never from https to plain http, and not forever.
func noHTTPSDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if req.URL.Scheme != "https" && via[len(via)-1].URL.Scheme == "https" {
		return fmt.Errorf("refusing redirect from https to %s", req.URL.Scheme)
	}
	return nil
}

// AssetName is the release archive for a platform: `.zip` on Windows,
// `.tar.gz` elsewhere (release-please.yml's "Build and package").
func AssetName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("smind_%s_%s_%s%s", version, goos, goarch, ext)
}

// ChecksumsName is the single checksum manifest published with every release.
const ChecksumsName = "checksums.txt"

func (s *Source) assetURL(version, name string) string {
	return fmt.Sprintf("%s/releases/download/v%s/%s", s.BaseURL, version, name)
}

// LatestVersion resolves the latest stable release the way install.sh does:
// follow `<base>/releases/latest` to its `/releases/tag/v<version>` redirect
// and read the tag, instead of calling the rate-limited GitHub API.
func (s *Source) LatestVersion(ctx context.Context) (string, error) {
	client := *s.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if strings.Contains(req.URL.Path, "/releases/tag/") {
			return http.ErrUseLastResponse // the Location header is the answer; skip the HTML page
		}
		return noHTTPSDowngrade(req, via)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	tagURL := resp.Request.URL
	if loc, err := resp.Location(); err == nil {
		tagURL = loc
	}
	i := strings.Index(tagURL.Path, "/releases/tag/")
	if i < 0 {
		return "", fmt.Errorf("resolve latest release: %s did not lead to a release tag (HTTP %d); is there a published release?", req.URL, resp.StatusCode)
	}
	tag := path.Base(tagURL.Path) // URL.Path is already percent-decoded; ValidVersion vets the result
	v := NormalizeVersion(tag)
	if !ValidVersion(v) {
		return "", fmt.Errorf("resolve latest release: tag %q is not a version", tag)
	}
	return v, nil
}

// Fetch downloads one release asset into memory, failing if it exceeds max.
func (s *Source) Fetch(ctx context.Context, version, name string, max int64) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := s.download(ctx, s.assetURL(version, name), &buf, max); err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// DownloadFile streams a release asset to dst, returning its SHA-256 as
// lower-case hex, computed over the bytes actually written.
func (s *Source) DownloadFile(ctx context.Context, version, name, dst string, max int64) (string, error) {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	sum, err := s.download(ctx, s.assetURL(version, name), f, max)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	return sum, nil
}

func (s *Source) download(ctx context.Context, rawURL string, w io.Writer, max int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", fmt.Errorf("%w (HTTP 404 from %s)", ErrAssetNotFound, rawURL)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, max+1))
	if err != nil {
		return "", err
	}
	if n > max {
		return "", fmt.Errorf("larger than the %d-byte limit", max)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
