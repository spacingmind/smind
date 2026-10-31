package files

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/testutil"
)

var testLimits = Limits{MaxReadBytes: 50 << 20, MaxChunkBytes: 4 << 20}

func writeFile(t *testing.T, root, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// leaksPath reports whether err's text contains any of dirs, raw or as a
// quoted string (%q doubles Windows backslashes, which hides a raw match).
func leaksPath(err error, dirs ...string) bool {
	msg := err.Error()
	for _, d := range dirs {
		q := strconv.Quote(d)
		if strings.Contains(msg, d) || strings.Contains(msg, q[1:len(q)-1]) {
			return true
		}
	}
	return false
}

// TestErrName pins the error-display form of a caller path with POSIX and
// Windows spellings alike, on every OS.
func TestErrName(t *testing.T) {
	for in, want := range map[string]string{
		"doc.pdf":               "doc.pdf",
		"sub/doc.pdf":           "sub/doc.pdf",
		`sub\doc.pdf`:           `sub\doc.pdf`,
		"/tmp/x/secret.bin":     "<path>",
		`C:\Users\x\secret.bin`: "<path>",
		"C:secret.bin":          "<path>",
		`\\server\share\x.pdf`:  "<path>",
		`\\?\C:\x\y.pdf`:        "<path>",
		"../secret.bin":         "<path>",
		"a/../../b":             "<path>",
		`a\..\..\b`:             "<path>",
		"":                      "<path>",
	} {
		if got := errName(in); got != want {
			t.Errorf("errName(%q) = %q, want %q", in, got, want)
		}
	}
}

func asInvalid(err error) bool {
	var ip *InvalidParamsError
	return errors.As(err, &ip)
}

func TestFileStat_SniffsContentNotExtension(t *testing.T) {
	root := t.TempDir()
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	zip := append([]byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"), []byte("[Content_Types].xml")...)
	// A 3-byte rune cut by the 8 KiB window must still be text.
	cut := append(bytes.Repeat([]byte("a"), TextWindow-1), []byte("€€")...)

	cases := []struct {
		name     string
		data     []byte
		kind     string
		mismatch bool
		preview  bool
		ext      string
	}{
		{"a.txt", pdf, KindPDF, true, false, ".txt"},
		{"b.pdf", []byte("just text\n"), KindText, true, false, ".pdf"},
		{"c.PDF", pdf, KindPDF, false, true, ".pdf"}, // case-insensitive extension
		{"d.png", png, KindImage, false, true, ".png"},
		{"e.docx", zip, KindOffice, false, false, ".docx"},
		{"f.dat", []byte{}, KindText, false, true, ".dat"},
		{"g.txt", []byte("has\x00nul"), KindOther, true, false, ".txt"},
		{"h.bin", []byte{0xff, 0xfe, 0xfd, 0x00}, KindOther, false, false, ".bin"},
		{"i.md", cut, KindText, false, true, ".md"},
		{"j.pdf", append(bytes.Repeat([]byte("x"), 500), pdf...), KindPDF, false, true, ".pdf"},
	}
	for _, tc := range cases {
		writeFile(t, root, tc.name, tc.data)
		got, err := Stat(root, tc.name)
		if err != nil {
			t.Fatalf("%s: Stat: %v", tc.name, err)
		}
		if got.Kind != tc.kind || got.Mismatch != tc.mismatch || got.Previewable != tc.preview || got.Ext != tc.ext {
			t.Errorf("%s: got kind=%s mismatch=%v previewable=%v ext=%s (mime %s), want kind=%s mismatch=%v previewable=%v ext=%s",
				tc.name, got.Kind, got.Mismatch, got.Previewable, got.Ext, got.Mime, tc.kind, tc.mismatch, tc.preview, tc.ext)
		}
		if got.Size != int64(len(tc.data)) || got.Mtime.IsZero() {
			t.Errorf("%s: size=%d mtime=%v", tc.name, got.Size, got.Mtime)
		}
	}
}

func TestFileStat_RejectsNonRegular(t *testing.T) {
	root := newTestRoot(t)
	if _, err := Stat(root, "sub"); !asInvalid(err) {
		t.Errorf("directory: err = %v, want InvalidParamsError", err)
	}
	if _, err := Stat(root, "missing.pdf"); err == nil {
		t.Error("missing: want error")
	} else if _, ok := err.(*MissingError); !ok {
		t.Errorf("missing: err = %T %v, want *MissingError", err, err)
	}
	if _, err := Stat(root, "../x"); err == nil {
		t.Error("../ escape: want refusal")
	}
	t.Run("symlink_outside", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}
		testutil.Link(t, outside, filepath.Join(root, "evil"))
		if _, err := Stat(root, "evil"); err == nil {
			t.Error("symlink to outside: want refusal")
		}
	})
}

func readAll(t *testing.T, root, path string, chunk int64) ([]byte, int) {
	t.Helper()
	lim := Limits{MaxReadBytes: 50 << 20, MaxChunkBytes: chunk}
	var out []byte
	var mt *time.Time
	eofs := 0
	for {
		r, err := ReadRange(root, path, int64(len(out)), 1<<30, lim, mt)
		if err != nil {
			t.Fatalf("ReadRange at %d: %v", len(out), err)
		}
		out = append(out, r.Data...)
		mt = &r.Mtime
		if r.EOF {
			eofs++
			return out, eofs
		}
		if len(r.Data) == 0 {
			t.Fatalf("empty non-EOF chunk at %d", len(out))
		}
	}
}

func TestFileReadRange_RoundTrip(t *testing.T) {
	root := t.TempDir()
	data := make([]byte, 3<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "rand.bin", data)
	for _, chunk := range []int64{512 << 10, 4 << 20, 1000003} {
		got, eofs := readAll(t, root, "rand.bin", chunk)
		if !bytes.Equal(got, data) {
			t.Errorf("chunk %d: reassembled bytes differ", chunk)
		}
		if eofs != 1 {
			t.Errorf("chunk %d: eof seen %d times, want 1", chunk, eofs)
		}
	}
}

func TestFileReadRange_ClampsAndBounds(t *testing.T) {
	root := t.TempDir()
	data := bytes.Repeat([]byte("0123456789"), 100) // 1000 bytes
	writeFile(t, root, "d.bin", data)
	lim := Limits{MaxReadBytes: 5000, MaxChunkBytes: 100}

	r, err := ReadRange(root, "d.bin", 0, 999999, lim, nil)
	if err != nil || len(r.Data) != 100 || r.EOF {
		t.Fatalf("over-cap length: len=%d eof=%v err=%v, want 100 bytes, not eof", len(r.Data), r.EOF, err)
	}
	r, err = ReadRange(root, "d.bin", 950, 100, lim, nil)
	if err != nil || len(r.Data) != 50 || !r.EOF {
		t.Fatalf("tail: len=%d eof=%v err=%v, want 50 bytes + eof", len(r.Data), r.EOF, err)
	}
	for _, off := range []int64{1000, 1001, 1 << 40} {
		r, err = ReadRange(root, "d.bin", off, 10, lim, nil)
		if err != nil || len(r.Data) != 0 || !r.EOF {
			t.Errorf("offset %d past EOF: len=%d eof=%v err=%v", off, len(r.Data), r.EOF, err)
		}
	}
	if r, err = ReadRange(root, "d.bin", 0, 0, lim, nil); err != nil || len(r.Data) != 0 || r.EOF {
		t.Errorf("zero length mid-file: len=%d eof=%v err=%v", len(r.Data), r.EOF, err)
	}
	for _, c := range [][2]int64{{-1, 10}, {0, -1}, {-5, -5}} {
		if _, err = ReadRange(root, "d.bin", c[0], c[1], lim, nil); !asInvalid(err) {
			t.Errorf("offset=%d length=%d: err = %v, want InvalidParamsError", c[0], c[1], err)
		}
	}
}

func TestFileReadRange_ContainmentAndTOCTOU(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "ok.bin", []byte("fine"))
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.bin")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{"../secret.bin", outside, ".."}
	t.Run("symlink_outside", func(t *testing.T) {
		testutil.Link(t, outside, filepath.Join(root, "evil"))
		if r, err := ReadRange(root, "evil", 0, 10, testLimits, nil); err == nil || len(r.Data) != 0 {
			t.Errorf("symlink to outside: want refusal, got %d bytes err=%v", len(r.Data), err)
		}
	})
	for _, p := range paths {
		r, err := ReadRange(root, p, 0, 10, testLimits, nil)
		if err == nil || len(r.Data) != 0 {
			t.Errorf("%q: want refusal, got %d bytes err=%v", p, len(r.Data), err)
		}
		if err != nil && leaksPath(err, outsideDir, root) {
			t.Errorf("%q: error leaks an absolute path: %v", p, err)
		}
	}

	// A file that grew past the cap after an earlier stat: too_large, and
	// never a byte of it.
	lim := Limits{MaxReadBytes: 1024, MaxChunkBytes: 4096}
	writeFile(t, root, "grow.bin", bytes.Repeat([]byte("a"), 512))
	if _, err := ReadRange(root, "grow.bin", 0, 4096, lim, nil); err != nil {
		t.Fatalf("under cap: %v", err)
	}
	writeFile(t, root, "grow.bin", bytes.Repeat([]byte("a"), 2048))
	r, err := ReadRange(root, "grow.bin", 0, 4096, lim, nil)
	var tl *TooLargeError
	if !errors.As(err, &tl) || len(r.Data) != 0 {
		t.Errorf("grown file: err = %v len=%d, want TooLargeError and no data", err, len(r.Data))
	}
	if tl != nil && tl.RPCCode() != "too_large" {
		t.Errorf("code = %q", tl.RPCCode())
	}
}

func TestFileReadRange_IfMtimeConflict(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "m.bin", bytes.Repeat([]byte("z"), 300))
	lim := Limits{MaxReadBytes: 1 << 20, MaxChunkBytes: 100}
	first, err := ReadRange(root, "m.bin", 0, 100, lim, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReadRange(root, "m.bin", 100, 100, lim, &first.Mtime); err != nil {
		t.Fatalf("unchanged file with matching ifMtime: %v", err)
	}
	later := first.Mtime.Add(5 * time.Second)
	if err = os.Chtimes(filepath.Join(root, "m.bin"), later, later); err != nil {
		t.Fatal(err)
	}
	_, err = ReadRange(root, "m.bin", 100, 100, lim, &first.Mtime)
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.RPCCode() != "conflict" {
		t.Errorf("touched file: err = %v, want ConflictError(conflict)", err)
	}
}

func TestFileReadRange_ErrorsNameNoContentOrAbsolutePath(t *testing.T) {
	root := t.TempDir()
	_, err := ReadRange(root, "nope.bin", 0, 1, testLimits, nil)
	if _, ok := err.(*MissingError); !ok {
		t.Fatalf("missing: err = %T %v", err, err)
	}
	if leaksPath(err, root) {
		t.Errorf("missing error leaks root: %v", err)
	}
	_, err = ReadRange(root, filepath.Join("no", "such", "dir", "x.bin"), 0, 1, testLimits, nil)
	if _, ok := err.(*MissingError); !ok {
		t.Errorf("missing parent dir: err = %T %v, want *MissingError", err, err)
	}
}
