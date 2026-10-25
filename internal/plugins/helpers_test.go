package plugins

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// ent is one archive entry for the in-test archive builders: archives are
// built here, never checked in as binaries.
type ent struct {
	name   string
	body   string
	typ    byte // tar type flag; 0 = regular file
	target string
	exec   bool
}

func buildTar(t *testing.T, gz bool, ents []ent) []byte {
	t.Helper()
	var buf bytes.Buffer
	var tw *tar.Writer
	var gw *gzip.Writer
	if gz {
		gw = gzip.NewWriter(&buf)
		tw = tar.NewWriter(gw)
	} else {
		tw = tar.NewWriter(&buf)
	}
	for _, e := range ents {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Linkname: e.target, Format: tar.FormatPAX}
		if e.exec {
			h.Mode = 0o755
		}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if typ == tar.TypeDir {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("tar header %q: %v", e.name, err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if gw != nil {
		if err := gw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, ents []ent) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range ents {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		switch e.typ {
		case tar.TypeSymlink:
			h.SetMode(os.ModeSymlink | 0o777)
		case tar.TypeDir:
			h.SetMode(os.ModeDir | 0o755)
		default:
			h.SetMode(0o644)
			if e.exec {
				h.SetMode(0o755)
			}
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatalf("zip header %q: %v", e.name, err)
		}
		body := e.body
		if e.typ == tar.TypeSymlink {
			body = e.target
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// lyingZip builds a zip whose one entry's header claims 10 bytes while the
// deflated payload really expands to n bytes of zeros: what a zip bomb does.
func lyingZip(t *testing.T, name string, n int) []byte {
	t.Helper()
	payload := bytes.Repeat([]byte{0}, n)
	var comp bytes.Buffer
	fw, _ := flate.NewWriter(&comp, flate.BestCompression)
	fw.Write(payload)
	fw.Close()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name: name, Method: zip.Deflate, CRC32: crc32.ChecksumIEEE(payload),
		CompressedSize64: uint64(comp.Len()), UncompressedSize64: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(comp.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sum256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

const goodManifest = `{"id":"demo","name":"Demo","version":"1.0.0","api":1,"engines":{"smind":">=0.1.0"},"capabilities":["tasks.read"]}`

func manifestWith(id, version string, extra string) string {
	m := `{"id":"` + id + `","name":"Demo","version":"` + version + `","api":1,"engines":{"smind":">=0.1.0"}`
	if extra != "" {
		m += "," + extra
	}
	return m + "}"
}

// writePluginDir makes a plugin source directory with the manifest and files.
func writePluginDir(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ManifestFile), []byte(manifest))
	for n, b := range files {
		writeFile(t, filepath.Join(dir, n), []byte(b))
	}
	return dir
}

type env struct {
	home string
	st   *store.Store
	m    *Manager
	on   *bool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	on := true
	e := &env{home: home, st: st, on: &on}
	e.m = New(Options{Home: home, Store: st, Version: "0.15.0", Enabled: func() bool { return on }})
	return e
}

// snapshot lists every file under dir (the database aside), for before/after
// comparison ("nothing left behind").
func snapshot(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if strings.HasPrefix(rel, "smind.db") || d.IsDir() {
			return nil // only files count: an empty directory holds nothing
		}
		out = append(out, rel)
		return nil
	})
	return out
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}
