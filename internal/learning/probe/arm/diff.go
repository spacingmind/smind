package arm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const truncatedMarker = "\n[truncated]\n"

// Diff renders what the arm changed relative to its one base commit: the
// tracked changes of `git diff HEAD` plus every untracked, non-ignored
// regular file as a `new file` entry (each read at most maxBytes). It never
// mutates the arm repository (no `add -N`), and runs git with the scrubbed
// gitEnv and the arm-writable config neutralised (no hooks, no fsmonitor, no
// external diff or textconv). The text is clipped to maxBytes with a
// "[truncated]" marker; n is the size before clipping (a lower bound once
// the untracked files alone exceed maxBytes, where only their sizes are
// summed).
func Diff(ctx context.Context, dir string, maxBytes int) (diff string, n int, err error) {
	if !filepath.IsAbs(dir) {
		return "", 0, fmt.Errorf("arm: diff dir %q is not absolute", dir)
	}
	if maxBytes <= 0 {
		return "", 0, fmt.Errorf("arm: diff maxBytes %d", maxBytes)
	}
	tracked, err := runGit(ctx, dir, nil, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false",
		"diff", "--no-ext-diff", "--no-textconv", "--no-color", "HEAD")
	if err != nil {
		return "", 0, err
	}
	names, err := runGit(ctx, dir, nil, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false",
		"ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", 0, err
	}

	var out bytes.Buffer
	out.Write(tracked)
	n = out.Len()
	for _, name := range strings.Split(string(names), "\x00") {
		if name == "" {
			continue
		}
		entry, size := untrackedEntry(dir, name, maxBytes, n < maxBytes)
		n += size
		if n-size < maxBytes { // keep text only while it can still be shown
			out.WriteString(entry)
		}
	}

	text := out.String()
	if len(text) > maxBytes {
		text = strings.ToValidUTF8(text[:maxBytes], "") + truncatedMarker
	}
	return text, n, nil
}

// untrackedEntry renders one untracked file. size is the rendered length (or
// the file's size when read is false and nothing is rendered).
func untrackedEntry(dir, name string, maxBytes int, read bool) (string, int) {
	full := filepath.Join(dir, filepath.FromSlash(name))
	fi, err := os.Lstat(full)
	if err != nil || !fi.Mode().IsRegular() { // links are never followed
		return "", 0
	}
	header := fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100%o\n", name, name, fi.Mode().Perm())
	if !read {
		return "", len(header) + int(min(fi.Size(), int64(1<<40)))
	}
	f, err := os.Open(full)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return "", 0
	}
	if bytes.IndexByte(data, 0) >= 0 {
		e := header + fmt.Sprintf("Binary files /dev/null and b/%s differ\n", name)
		return e, len(e)
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("--- /dev/null\n+++ b/" + name + "\n")
	if len(data) > 0 {
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, l := range lines {
			b.WriteString("+" + l + "\n")
		}
		if !bytes.HasSuffix(data, []byte("\n")) {
			b.WriteString("\\ No newline at end of file\n")
		}
	}
	if fi.Size() > int64(len(data)) {
		b.WriteString("[file truncated]\n")
	}
	e := b.String()
	return e, len(e)
}
