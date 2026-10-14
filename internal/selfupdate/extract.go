package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// maxBinarySize bounds the extracted binary (a Go build with the embedded web
// UI is tens of MiB); a larger member is a decompression bomb or a wrong file.
const maxBinarySize = 512 << 20

var (
	// ErrUnsafePath means an archive entry name is absolute or climbs out of
	// the extraction root. The whole archive is rejected, not just the entry.
	ErrUnsafePath = errors.New("unsafe path in archive")
	// ErrMissingBinary means the archive has no root-level binary member.
	ErrMissingBinary = errors.New("archive has no binary member")
	// ErrOversize means the binary member exceeds the size limit.
	ErrOversize = errors.New("binary member too large")
)

// BinaryName is the executable inside the release archive for a platform.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "smind.exe"
	}
	return "smind"
}

// ExtractBinary writes only the archive's root-level `member` (smind or
// smind.exe) to dst, mode 0755, and ignores every other entry (LICENSE). It
// fails closed: any entry whose name is absolute, contains `..`, a backslash
// or a drive/NUL byte rejects the archive; the member must be a regular file
// within maxBytes. dst is removed again on any error.
func ExtractBinary(archivePath, member, dst string, maxBytes int64) (err error) {
	defer func() {
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	switch {
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZip(archivePath, member, dst, maxBytes)
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return extractTarGz(archivePath, member, dst, maxBytes)
	}
	return fmt.Errorf("unsupported archive %q (want .tar.gz or .zip)", archivePath)
}

// safeEntryName normalizes an archive entry name to a slash path relative to
// the archive root, or reports it unsafe.
func safeEntryName(name string) (string, bool) {
	if name == "" || strings.ContainsAny(name, "\\\x00:") || strings.HasPrefix(name, "/") {
		return "", false
	}
	for _, el := range strings.Split(name, "/") {
		if el == ".." {
			return "", false
		}
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", true // the root directory entry itself
	}
	return clean, true
}

func extractTarGz(archivePath, member, dst string, maxBytes int64) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read %s: %w", path.Base(archivePath), err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path.Base(archivePath), err)
		}
		name, ok := safeEntryName(hdr.Name)
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnsafePath, hdr.Name)
		}
		if name != member {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("%s in the archive is not a regular file", member)
		}
		if found {
			return fmt.Errorf("archive lists %s twice", member)
		}
		if hdr.Size > maxBytes {
			return fmt.Errorf("%w: %d bytes (limit %d)", ErrOversize, hdr.Size, maxBytes)
		}
		if err := writeLimited(dst, tr, maxBytes); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrMissingBinary, member)
	}
	return nil
}

func extractZip(archivePath, member, dst string, maxBytes int64) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", path.Base(archivePath), err)
	}
	defer func() { _ = zr.Close() }()

	var target *zip.File
	for _, zf := range zr.File {
		name, ok := safeEntryName(zf.Name)
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnsafePath, zf.Name)
		}
		if name != member {
			continue
		}
		if !zf.Mode().IsRegular() {
			return fmt.Errorf("%s in the archive is not a regular file", member)
		}
		if target != nil {
			return fmt.Errorf("archive lists %s twice", member)
		}
		if zf.UncompressedSize64 > uint64(maxBytes) {
			return fmt.Errorf("%w: %d bytes (limit %d)", ErrOversize, zf.UncompressedSize64, maxBytes)
		}
		target = zf
	}
	if target == nil {
		return fmt.Errorf("%w: %s", ErrMissingBinary, member)
	}
	rc, err := target.Open()
	if err != nil {
		return fmt.Errorf("read %s: %w", member, err)
	}
	defer func() { _ = rc.Close() }()
	return writeLimited(dst, rc, maxBytes)
}

// writeLimited copies src to a new 0755 file at dst, failing if src yields
// more than maxBytes. The header's declared size is not trusted: the bound is
// enforced on the bytes actually decompressed.
func writeLimited(dst string, src io.Reader, maxBytes int64) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(src, maxBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("%w: more than %d bytes", ErrOversize, maxBytes)
	}
	return nil
}
