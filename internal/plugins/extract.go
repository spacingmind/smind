package plugins

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Limits bounds what an archive or a source directory may unpack to. The caps
// are enforced on the bytes actually produced, never on the sizes an archive
// header claims (a zip bomb lies in its headers).
type Limits struct {
	MaxEntries    int
	MaxTotalBytes int64
	MaxFileBytes  int64
}

// DefaultLimits is generous for a plugin (code, prompts, themes) and small
// enough that a bomb cannot fill a disk.
var DefaultLimits = Limits{MaxEntries: 5000, MaxTotalBytes: 64 << 20, MaxFileBytes: 16 << 20}

// MaxArchiveBytes caps an archive as downloaded or read, before extraction.
const MaxArchiveBytes = 64 << 20

// treeWriter accumulates the entries of one extraction: it validates names,
// detects duplicates and case collisions (on any filesystem, so a plugin
// installs the same everywhere) and counts what is written.
type treeWriter struct {
	root   string
	lim    Limits
	files  map[string]bool // lower-cased relative path -> is a file
	dirs   map[string]bool
	names  map[string]string // lower-cased -> original, for the collision message
	n      int
	total  int64
	hasher *treeHasher
}

func newTreeWriter(root string, lim Limits) *treeWriter {
	return &treeWriter{root: root, lim: lim, files: map[string]bool{}, dirs: map[string]bool{}, names: map[string]string{}}
}

// CleanEntryName validates an archive entry or directory-walk name and returns
// its clean forward-slashed form. It rejects every spelling of a path that
// could leave the destination on any supported OS.
func CleanEntryName(name string) (string, error) {
	if name == "" {
		return "", newErr(CodeUnsafeEntry, "empty entry name")
	}
	if strings.ContainsRune(name, 0) {
		return "", newErr(CodeUnsafeEntry, "entry name %q contains a NUL byte", name)
	}
	if strings.Contains(name, `\`) {
		return "", newErr(CodeUnsafeEntry, "entry name %q contains a backslash", name)
	}
	if strings.HasPrefix(name, "/") || driveRe.MatchString(name) {
		return "", newErr(CodeUnsafeEntry, "entry name %q is absolute", name)
	}
	var segs []string
	for _, s := range strings.Split(name, "/") {
		switch {
		case s == "" || s == ".":
			continue
		case s == "..":
			return "", newErr(CodeUnsafeEntry, "entry name %q escapes the plugin directory", name)
		case strings.Contains(s, ":"):
			return "", newErr(CodeUnsafeEntry, "entry name %q contains ':' (drive letter or alternate data stream)", name)
		case windowsReserved[strings.ToLower(strings.TrimRight(strings.SplitN(s, ".", 2)[0], " "))]:
			return "", newErr(CodeUnsafeEntry, "entry name %q is a Windows device name", name)
		case strings.HasSuffix(s, ".") || strings.HasSuffix(s, " "):
			return "", newErr(CodeUnsafeEntry, "entry name %q ends in a dot or space", name)
		}
		segs = append(segs, s)
	}
	if len(segs) == 0 {
		return "", newErr(CodeUnsafeEntry, "entry name %q is empty after cleaning", name)
	}
	return strings.Join(segs, "/"), nil
}

// claim registers rel (already clean) as a file or directory, enforcing the
// entry cap, duplicates, case collisions and file/dir clashes.
func (w *treeWriter) claim(rel string, isDir bool) error {
	lower := strings.ToLower(rel)
	if prev, ok := w.names[lower]; ok {
		if prev == rel && isDir && w.dirs[lower] {
			return nil // a directory listed twice is harmless
		}
		if prev == rel {
			return newErr(CodeDuplicateEntry, "entry %q appears twice", rel)
		}
		return newErr(CodeDuplicateEntry, "entries %q and %q differ only in case", prev, rel)
	}
	segs := strings.Split(rel, "/")
	for i := 1; i < len(segs); i++ {
		anc := strings.Join(segs[:i], "/")
		lanc := strings.ToLower(anc)
		if w.files[lanc] {
			return newErr(CodeDuplicateEntry, "entry %q sits under the file %q", rel, anc)
		}
		if prev, ok := w.names[lanc]; ok && prev != anc {
			return newErr(CodeDuplicateEntry, "directories %q and %q differ only in case", prev, anc)
		}
	}
	w.n++
	if w.n > w.lim.MaxEntries {
		return newErr(CodeTooManyEntries, "more than %d entries", w.lim.MaxEntries)
	}
	for i := 1; i < len(segs); i++ { // remember implicit parent directories
		anc := strings.Join(segs[:i], "/")
		w.names[strings.ToLower(anc)] = anc
		w.dirs[strings.ToLower(anc)] = true
	}
	w.names[lower] = rel
	if isDir {
		w.dirs[lower] = true
	} else {
		w.files[lower] = true
	}
	return nil
}

func (w *treeWriter) dest(rel string) (string, error) {
	full := filepath.Join(w.root, filepath.FromSlash(rel))
	r, err := filepath.Rel(w.root, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", newErr(CodeUnsafeEntry, "entry %q resolves outside the destination", rel)
	}
	return full, nil
}

func (w *treeWriter) mkdir(rel string) error {
	if err := w.claim(rel, true); err != nil {
		return err
	}
	full, err := w.dest(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(full, 0o755)
}

// writeFile streams r into rel. declared is the size the archive header (or
// stat) claims: it fails fast when that alone is over a cap, but is never
// trusted: the bytes really read are counted and cut at the cap, because a
// bomb's header lies.
func (w *treeWriter) writeFile(rel string, r io.Reader, exec bool, declared int64) error {
	if declared > w.lim.MaxFileBytes {
		return newErr(CodeTooLarge, "entry %q declares %d bytes, over the %d byte per-file cap", rel, declared, w.lim.MaxFileBytes)
	}
	if declared > w.lim.MaxTotalBytes-w.total {
		return newErr(CodeTooLarge, "archive declares more than the %d byte total cap", w.lim.MaxTotalBytes)
	}
	if err := w.claim(rel, false); err != nil {
		return err
	}
	full, err := w.dest(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	remaining := w.lim.MaxTotalBytes - w.total
	limit := w.lim.MaxFileBytes
	if remaining < limit {
		limit = remaining
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, limit+1))
	cerr := f.Close()
	w.total += n
	switch {
	case n > w.lim.MaxFileBytes:
		return newErr(CodeTooLarge, "entry %q is over the %d byte per-file cap", rel, w.lim.MaxFileBytes)
	case n > limit:
		return newErr(CodeTooLarge, "archive expands past the %d byte total cap", w.lim.MaxTotalBytes)
	case err != nil:
		var pe *Error
		if errors.As(err, &pe) {
			return pe
		}
		return &Error{Code: CodeBadArchive, Msg: fmt.Sprintf("entry %q is corrupt or larger than its header declares", rel), Err: err}
	case cerr != nil:
		return cerr
	}
	if w.hasher != nil {
		w.hasher.add(rel, h.Sum(nil))
	}
	return nil
}

// treeHasher produces a stable content hash of a tree: sha256 over sorted
// "path NUL filehash" records (file modes are not part of it: Windows has none).
type treeHasher struct{ recs []string }

func (t *treeHasher) add(rel string, fileHash []byte) {
	t.recs = append(t.recs, rel+"\x00"+hex.EncodeToString(fileHash))
}

func (t *treeHasher) sum() string {
	sort.Strings(t.recs)
	h := sha256.New()
	for _, r := range t.recs {
		io.WriteString(h, r)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ExtractArchive unpacks the zip, tar or tar.gz at archivePath into dest (an
// existing empty directory) and returns the tree hash. The format is detected
// from the bytes. Any unsafe entry fails the whole extraction; the caller
// removes dest.
func ExtractArchive(archivePath, dest string, lim Limits) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	head, _ := br.Peek(512)
	w := newTreeWriter(dest, lim)
	w.hasher = &treeHasher{}
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		st, err := f.Stat()
		if err != nil {
			return "", err
		}
		err = extractZip(f, st.Size(), w)
		if err != nil {
			return "", err
		}
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}):
		gz, err := gzip.NewReader(br)
		if err != nil {
			return "", &Error{Code: CodeBadArchive, Msg: "not a valid gzip stream", Err: err}
		}
		defer gz.Close()
		// The gzip stream itself is bounded by the total cap plus tar framing,
		// so a bomb stops decompressing instead of only failing per entry.
		bound := &capReader{r: gz, left: lim.MaxTotalBytes + int64(lim.MaxEntries)*1024 + (1 << 20)}
		if err := extractTar(bound, w); err != nil {
			return "", err
		}
	case len(head) >= 262 && string(head[257:262]) == "ustar":
		if err := extractTar(br, w); err != nil {
			return "", err
		}
	default:
		return "", newErr(CodeBadArchive, "unrecognized archive format (expected zip, tar or tar.gz)")
	}
	if w.n == 0 {
		return "", newErr(CodeBadArchive, "archive is empty")
	}
	return w.hasher.sum(), nil
}

// capReader fails with a typed too_large error once more than left bytes have
// been read, so a decompression bomb stops decompressing.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, newErr(CodeTooLarge, "archive decompresses past its size cap")
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

func extractTar(r io.Reader, w *treeWriter) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			var pe *Error
			if errors.As(err, &pe) {
				return pe
			}
			return &Error{Code: CodeBadArchive, Msg: "unreadable tar", Err: err}
		}
		switch h.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeSymlink, tar.TypeLink:
			return newErr(CodeLinkRejected, "entry %q is a symlink or hardlink; links are not allowed in plugin archives", h.Name)
		case tar.TypeDir, tar.TypeReg:
		default:
			return newErr(CodeUnsafeEntry, "entry %q has unsupported type %q (device, fifo or special file)", h.Name, string(h.Typeflag))
		}
		rel, err := CleanEntryName(h.Name)
		if err != nil {
			if h.Typeflag == tar.TypeDir && (h.Name == "./" || h.Name == ".") {
				continue
			}
			return err
		}
		if h.Typeflag == tar.TypeDir {
			if err := w.mkdir(rel); err != nil {
				return err
			}
			continue
		}
		if err := w.writeFile(rel, tr, h.FileInfo().Mode()&0o111 != 0, h.Size); err != nil {
			return err
		}
	}
}

func extractZip(ra io.ReaderAt, size int64, w *treeWriter) error {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return &Error{Code: CodeBadArchive, Msg: "unreadable zip", Err: err}
	}
	if len(zr.File) > w.lim.MaxEntries {
		return newErr(CodeTooManyEntries, "more than %d entries", w.lim.MaxEntries)
	}
	for _, zf := range zr.File {
		mode := zf.Mode()
		switch {
		case mode&fs.ModeSymlink != 0:
			return newErr(CodeLinkRejected, "entry %q is a symlink; links are not allowed in plugin archives", zf.Name)
		case mode&(fs.ModeDevice|fs.ModeCharDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeIrregular) != 0:
			return newErr(CodeUnsafeEntry, "entry %q is a device or special file", zf.Name)
		}
		rel, err := CleanEntryName(zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := w.mkdir(rel); err != nil {
				return err
			}
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return &Error{Code: CodeBadArchive, Msg: fmt.Sprintf("cannot open entry %q", zf.Name), Err: err}
		}
		err = w.writeFile(rel, rc, mode&0o111 != 0, int64(min(zf.UncompressedSize64, 1<<62)))
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// CopyDir copies the directory src into dest (an existing empty directory)
// under the same rules as an archive: no symlinks of any kind, no special
// files, the same name and size checks. src itself may be a symlink the Human
// typed; it is resolved once and only the real directory's contents are read.
// A top-level .git directory is skipped. It returns the tree hash.
func CopyDir(src, dest string, lim Limits) (string, error) {
	root, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", &Error{Code: CodeBadSource, Msg: "cannot resolve source directory", Err: err}
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return "", newErr(CodeBadSource, "%s is not a directory", src)
	}
	w := newTreeWriter(dest, lim)
	w.hasher = &treeHasher{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if p == root {
			return nil
		}
		relOS, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relOS)
		if rel == ".git" && d.IsDir() {
			return filepath.SkipDir
		}
		typ := d.Type()
		switch {
		case typ&fs.ModeSymlink != 0:
			return newErr(CodeLinkRejected, "%q is a symlink; links are not allowed in a plugin directory", rel)
		case typ.IsDir():
			if _, err := CleanEntryName(rel); err != nil {
				return err
			}
			return w.mkdir(rel)
		case !typ.IsRegular():
			return newErr(CodeUnsafeEntry, "%q is a device or special file", rel)
		}
		if _, err := CleanEntryName(rel); err != nil {
			return err
		}
		// Open, then check the opened file is the regular file the walk saw,
		// so a symlink swapped in after the walk is not followed.
		lst, err := os.Lstat(p)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		fst, err := f.Stat()
		if err != nil {
			return err
		}
		if !lst.Mode().IsRegular() || !fst.Mode().IsRegular() || !os.SameFile(lst, fst) {
			return newErr(CodeLinkRejected, "%q changed while being copied", rel)
		}
		return w.writeFile(rel, f, fst.Mode()&0o111 != 0, fst.Size())
	})
	if err != nil {
		return "", err
	}
	return w.hasher.sum(), nil
}

// treeHashOf recomputes the tree hash of an installed directory the way
// CopyDir computed it, for tamper detection.
func treeHashOf(root string) (string, error) {
	h := &treeHasher{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if p == root {
			return nil
		}
		relOS, _ := filepath.Rel(root, p)
		rel := filepath.ToSlash(relOS)
		if rel == "data" && d.IsDir() {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return newErr(CodeLinkRejected, "%q is not a regular file", rel)
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		fh := sha256.New()
		if _, err := io.Copy(fh, f); err != nil {
			return err
		}
		h.add(rel, fh.Sum(nil))
		return nil
	})
	if err != nil {
		return "", err
	}
	return h.sum(), nil
}
