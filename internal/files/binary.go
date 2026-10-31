package files

// binary.go -- binary-safe file access for office mode (ADR-0049 §2): Stat
// classifies a file by sniffing its content, ReadRange returns a bounded
// byte range. Read (files.go) stays the UTF-8-only editor path.
//
// Every entry point opens the file once and checks the *open descriptor*
// (regular file, size cap), so there is no window between the checks and
// the read. Error text names the caller's own relative path and a fixed
// reason, never an absolute path or file content.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	// SniffWindow is how many leading bytes the magic checks and
	// http.DetectContentType look at.
	SniffWindow = 1024
	// TextWindow is how many leading bytes must be valid UTF-8 without NUL
	// for a file to be classified as text.
	TextWindow = 8 << 10
)

// File kinds reported by Stat.
const (
	KindPDF    = "pdf"
	KindText   = "text"
	KindImage  = "image"
	KindOffice = "office"
	KindOther  = "other"
)

// Limits are the transport- and server-side ceilings of a ReadRange.
type Limits struct {
	// MaxReadBytes is the largest file ReadRange will serve any part of.
	MaxReadBytes int64
	// MaxChunkBytes is the largest single range; a longer request is clamped.
	MaxChunkBytes int64
}

// InvalidParamsError is a bad-argument outcome ("invalid_params" on the wire).
type InvalidParamsError struct{ Msg string }

func (e *InvalidParamsError) Error() string   { return e.Msg }
func (e *InvalidParamsError) RPCCode() string { return "invalid_params" }

// TooLargeError reports a file over Limits.MaxReadBytes ("too_large").
type TooLargeError struct {
	Path string
	Max  int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("read %q: file is larger than the %d byte read limit", e.Path, e.Max)
}
func (e *TooLargeError) RPCCode() string { return "too_large" }

// StatInfo is Stat's result.
type StatInfo struct {
	Size        int64     `json:"size"`
	Mtime       time.Time `json:"mtime"`
	Kind        string    `json:"kind"`
	Mime        string    `json:"mime"`
	Ext         string    `json:"ext"`
	Mismatch    bool      `json:"mismatch"`
	Previewable bool      `json:"previewable"`
}

// Range is ReadRange's result.
type Range struct {
	Offset int64
	Data   []byte
	// EOF is true when Offset+len(Data) reached the end of the file as the
	// open descriptor saw it (or the read cap).
	EOF   bool
	Mtime time.Time
}

// Stat classifies path (relative to root) from its first bytes, never from
// its extension. A directory, device, FIFO or socket is *InvalidParamsError;
// a missing file is *MissingError.
func Stat(root, path string) (StatInfo, error) {
	f, fi, err := openRegular(root, path)
	if err != nil {
		return StatInfo{}, err
	}
	defer f.Close()

	buf := make([]byte, TextWindow)
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return StatInfo{}, openError(path, err)
	}
	buf = buf[:n]

	ext := strings.ToLower(filepath.Ext(normalizeSeparators(path)))
	kind, mime := sniff(buf, fi.Size() > int64(n))
	mismatch := extMismatch(ext, kind)
	return StatInfo{
		Size:        fi.Size(),
		Mtime:       fi.ModTime(),
		Kind:        kind,
		Mime:        mime,
		Ext:         ext,
		Mismatch:    mismatch,
		Previewable: !mismatch && (kind == KindPDF || kind == KindText || kind == KindImage),
	}, nil
}

// ReadRange returns the bytes [offset, offset+n) of path, n clamped to
// lim.MaxChunkBytes, to the read cap and to the end of the file. An offset
// at or past EOF is an empty, EOF result. ifMtime, when non-nil, must equal
// the descriptor's mtime or the call fails with *ConflictError.
func ReadRange(root, path string, offset, length int64, lim Limits, ifMtime *time.Time) (Range, error) {
	if offset < 0 || length < 0 {
		return Range{}, &InvalidParamsError{Msg: "offset and length must not be negative"}
	}
	f, fi, err := openRegular(root, path)
	if err != nil {
		return Range{}, err
	}
	defer f.Close()

	if fi.Size() > lim.MaxReadBytes {
		return Range{}, &TooLargeError{Path: path, Max: lim.MaxReadBytes}
	}
	if ifMtime != nil && !fi.ModTime().Equal(*ifMtime) {
		return Range{}, &ConflictError{Path: path}
	}

	// The end of what may be served: the descriptor's size, and never past
	// the read cap even if the file grows while we read.
	end := min(fi.Size(), lim.MaxReadBytes)
	n := min(length, lim.MaxChunkBytes)
	if offset >= end {
		return Range{Offset: offset, EOF: true, Mtime: fi.ModTime()}, nil
	}
	n = min(n, end-offset)

	buf := make([]byte, n)
	got, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return Range{}, openError(path, err)
	}
	buf = buf[:got]
	return Range{
		Offset: offset,
		Data:   buf,
		EOF:    got < int(n) || offset+int64(got) >= end,
		Mtime:  fi.ModTime(),
	}, nil
}

// openRegular resolves path inside root, opens it once and verifies the
// open descriptor is a regular file that is the very file the resolved path
// named (a symlink swapped in after resolution fails SameFile).
func openRegular(root, path string) (*os.File, os.FileInfo, error) {
	if err := refuseUnsafeForm(path); err != nil {
		return nil, nil, err
	}
	target, err := resolveInRoot(root, path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, &MissingError{Path: path}
		}
		if strings.Contains(err.Error(), "escapes the worktree root") {
			return nil, nil, fmt.Errorf("read %q: path escapes the worktree root", path)
		}
		return nil, nil, fmt.Errorf("read %q: cannot resolve path", path)
	}

	// Stat before Open: opening a FIFO or device can block, so a
	// non-regular file must be refused before any open.
	pre, err := os.Lstat(target)
	if err != nil {
		return nil, nil, openError(path, err)
	}
	if !pre.Mode().IsRegular() {
		return nil, nil, &InvalidParamsError{Msg: fmt.Sprintf("read %q: not a regular file", path)}
	}

	f, err := os.Open(target)
	if err != nil {
		return nil, nil, openError(path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, openError(path, err)
	}
	if !fi.Mode().IsRegular() || !os.SameFile(pre, fi) {
		f.Close()
		return nil, nil, &InvalidParamsError{Msg: fmt.Sprintf("read %q: not a regular file", path)}
	}
	return f, fi, nil
}

// openError maps an OS error to a typed or fixed-vocabulary one, so no
// absolute path from the *PathError reaches the wire.
func openError(path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &MissingError{Path: path}
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("read %q: permission denied", path)
	case isSharingViolation(err):
		return fmt.Errorf("read %q: file is in use by another program", path)
	}
	return fmt.Errorf("read %q: cannot read file", path)
}

// errorSharingViolation is Windows ERROR_SHARING_VIOLATION.
const errorSharingViolation = syscall.Errno(32)

func isSharingViolation(err error) bool {
	return runtime.GOOS == "windows" && errors.Is(err, errorSharingViolation)
}

// refuseUnsafeForm rejects path forms that must not reach the filesystem:
// on Windows a UNC or device path (resolving it would start SMB
// authentication before the containment check) and a drive-relative form
// such as "C:foo". A local verbatim path (\\?\C:\...) is fine.
func refuseUnsafeForm(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if isUNCOrDevicePath(path) {
		return &InvalidParamsError{Msg: fmt.Sprintf("read %q: network and device paths are not allowed", path)}
	}
	n := normalizeSeparators(path)
	if vol := filepath.VolumeName(n); vol != "" && !filepath.IsAbs(n) {
		return &InvalidParamsError{Msg: fmt.Sprintf("read %q: drive-relative paths are not allowed", path)}
	}
	return nil
}

// isUNCOrDevicePath reports whether path (either separator) starts a UNC
// share (\\server\share, \\?\UNC\...) or a device namespace (\\.\...).
// \\?\C:\... is a local verbatim path and is not reported.
func isUNCOrDevicePath(path string) bool {
	p := strings.ReplaceAll(path, "/", `\`)
	if !strings.HasPrefix(p, `\\`) {
		return false
	}
	if rest, ok := strings.CutPrefix(p, `\\?\`); ok {
		return len(rest) >= 4 && strings.EqualFold(rest[:4], `UNC\`)
	}
	return true
}

func normalizeSeparators(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(path, "/", `\`)
	}
	return path
}

// sniff classifies buf (the file's leading bytes, at most TextWindow);
// truncated says the file continues past buf.
func sniff(buf []byte, truncated bool) (kind, mime string) {
	head := buf[:min(len(buf), SniffWindow)]
	if bytes.Contains(head, []byte("%PDF-")) {
		return KindPDF, "application/pdf"
	}
	if bytes.HasPrefix(head, []byte("PK\x03\x04")) {
		for _, marker := range []string{"[Content_Types].xml", "word/", "xl/", "ppt/"} {
			if bytes.Contains(head, []byte(marker)) {
				return KindOffice, "application/zip"
			}
		}
	}
	detected := http.DetectContentType(head)
	if strings.HasPrefix(detected, "image/") {
		return KindImage, detected
	}
	if isText(buf, truncated) {
		if strings.HasPrefix(detected, "text/") && !strings.Contains(detected, "utf-16") {
			return KindText, detected
		}
		return KindText, "text/plain; charset=utf-8"
	}
	return KindOther, detected
}

// isText: valid UTF-8 without NUL. A multi-byte rune cut by the end of a
// truncated window is not an error.
func isText(buf []byte, truncated bool) bool {
	if bytes.IndexByte(buf, 0) >= 0 {
		return false
	}
	if truncated {
		for k := 1; k < utf8.UTFMax && k <= len(buf); k++ {
			tail := buf[len(buf)-k:]
			if r, _ := utf8.DecodeRune(tail); r == utf8.RuneError && !utf8.FullRune(tail) {
				buf = buf[:len(buf)-k]
				break
			}
		}
	}
	return utf8.Valid(buf)
}

// extMismatch reports whether a known extension disagrees with the sniffed
// kind. An extension with no expectation never mismatches.
func extMismatch(ext, kind string) bool {
	want := ""
	switch ext {
	case ".pdf":
		want = KindPDF
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		want = KindImage
	case ".docx", ".xlsx", ".pptx":
		want = KindOffice
	case ".txt", ".md", ".csv", ".json", ".log":
		want = KindText
	}
	return want != "" && want != kind
}
