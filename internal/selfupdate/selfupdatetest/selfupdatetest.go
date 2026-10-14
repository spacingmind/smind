// Package selfupdatetest is a fake GitHub-style release server for testing
// smind update: `/releases/latest` redirects to `/releases/tag/v<latest>`, and
// assets live at `/releases/download/v<version>/<name>`.
//
// Archives contain a fake "binary" (see Binary) instead of a real executable,
// so tests run on every OS; pair it with RunVersion as the Updater's
// RunVersion probe.
package selfupdatetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// Server is the fake release host. Mutate its fields before the code under
// test runs; handlers read them under the lock.
type Server struct {
	*httptest.Server

	mu sync.Mutex
	// Latest is the version `/releases/latest` redirects to ("" = no release: 404).
	Latest string
	// Assets maps "<version>/<asset name>" to bytes; checksums.txt is one such entry.
	Assets map[string][]byte
	// Redirect makes every asset download bounce through /cdn/ first, like GitHub's object store.
	Redirect bool
	// Requests records every request path, in order.
	Requests []string
}

// New starts a Server and closes it with the test.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{Assets: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Requests = append(s.Requests, r.URL.Path)
	latest, redirect := s.Latest, s.Redirect
	s.mu.Unlock()

	switch {
	case r.URL.Path == "/releases/latest":
		if latest == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/releases/tag/v"+latest, http.StatusFound)
	case strings.HasPrefix(r.URL.Path, "/releases/tag/"):
		_, _ = w.Write([]byte("<html>release page</html>"))
	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		s.asset(w, r, strings.TrimPrefix(r.URL.Path, "/cdn/"))
	case strings.HasPrefix(r.URL.Path, "/releases/download/v"):
		key := strings.TrimPrefix(r.URL.Path, "/releases/download/v")
		if redirect {
			http.Redirect(w, r, "/cdn/"+key, http.StatusFound)
			return
		}
		s.asset(w, r, key)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) asset(w http.ResponseWriter, r *http.Request, key string) {
	s.mu.Lock()
	b, ok := s.Assets[key]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(b)
}

// Binary is the fake executable shipped for a version.
func Binary(version string) []byte { return []byte("FAKE-SMIND " + version + "\n") }

// RunVersion is an Updater.RunVersion probe for fake binaries: it reads the
// file instead of executing it.
func RunVersion(_ context.Context, exe string) (string, error) {
	b, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	v, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "FAKE-SMIND ")
	if !ok {
		return "", fmt.Errorf("%s is not a fake smind binary", exe)
	}
	return v, nil
}

// Add publishes version for goos/goarch with the fake binary and a matching
// checksums.txt entry (other platforms already listed for that version are
// kept).
func (s *Server) Add(t testing.TB, version, goos, goarch string) {
	t.Helper()
	s.AddArchive(t, version, goos, goarch, Archive(t, goos, "smind", Binary(version)))
}

// AddArchive publishes archive bytes as version's asset for goos/goarch and
// records their true SHA-256 in checksums.txt.
func (s *Server) AddArchive(t testing.TB, version, goos, goarch string, archive []byte) {
	t.Helper()
	name := assetName(version, goos, goarch)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Assets[version+"/"+name] = archive
	sum := sha256.Sum256(archive)
	line := hex.EncodeToString(sum[:]) + "  " + name + "\n"
	s.Assets[version+"/checksums.txt"] = append(s.Assets[version+"/checksums.txt"], line...)
}

// SetChecksums replaces version's checksums.txt wholesale.
func (s *Server) SetChecksums(version, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Assets[version+"/checksums.txt"] = []byte(content)
}

// Paths returns a copy of the recorded request paths.
func (s *Server) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Requests...)
}

func assetName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("smind_%s_%s_%s%s", version, goos, goarch, ext)
}

// File is one archive entry.
type File struct {
	Name string
	Body []byte
}

// Archive builds a release archive (zip for windows, tar.gz otherwise) holding
// member (with body) and a LICENSE.
func Archive(t testing.TB, goos, member string, body []byte) []byte {
	t.Helper()
	return ArchiveOf(t, goos, File{member, body}, File{"LICENSE", []byte("MIT")})
}

// ArchiveOf builds an archive from arbitrary entries, names verbatim (so
// traversal names can be tested).
func ArchiveOf(t testing.TB, goos string, files ...File) []byte {
	t.Helper()
	var buf bytes.Buffer
	if goos == "windows" {
		zw := zip.NewWriter(&buf)
		for _, f := range files {
			w, err := zw.Create(f.Name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(f.Body); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.Name, Mode: 0o755, Size: int64(len(f.Body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.Body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
