package arm

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, p, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// realRepo builds a source repo with a hook, a remote, a symlink, an
// executable and a second (later) commit, and returns it and the FIRST
// commit's sha.
func realRepo(t *testing.T) (repo, base string) {
	t.Helper()
	repo = t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	write(t, filepath.Join(repo, "README.md"), "hello\n", 0o644)
	write(t, filepath.Join(repo, "cmd", "run.sh"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(repo, ".gitignore"), "ignored.txt\n", 0o644)
	if err := os.Symlink("/etc/passwd", filepath.Join(repo, "escape")); err != nil {
		t.Skip("symlinks unavailable")
	}
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "one")
	base = git(t, repo, "rev-parse", "HEAD")
	write(t, filepath.Join(repo, "later.txt"), "later\n", 0o644)
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "two")
	git(t, repo, "remote", "add", "origin", "https://example.invalid/secret/repo.git")
	write(t, filepath.Join(repo, ".git", "hooks", "post-commit"), "#!/bin/sh\ntouch /tmp/should-not-run\n", 0o755)
	return repo, base
}

// treeHash hashes names, modes and contents under dir, skipping skip.
func treeHash(t *testing.T, dir, skip string) string {
	t.Helper()
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, p)
		if skip != "" && (rel == skip || strings.HasPrefix(rel, skip+string(filepath.Separator))) {
			return nil
		}
		fi, _ := os.Lstat(p)
		h.Write([]byte(rel + "|" + fi.Mode().String() + "|"))
		if fi.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			h.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func TestMaterialise_ExportsBaseSHA(t *testing.T) {
	repo, base := realRepo(t)
	dir := filepath.Join(t.TempDir(), "arm-1")
	res, err := Materialise(context.Background(), Spec{Repo: repo, BaseSHA: base, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "hello\n" {
		t.Errorf("README.md = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "later.txt")); err == nil {
		t.Error("file from a commit after the base sha was exported")
	}
	if fi, err := os.Stat(filepath.Join(dir, "cmd", "run.sh")); err != nil || fi.Mode()&0o100 == 0 {
		t.Errorf("executable bit lost: %v %v", fi, err)
	}
	if res.Files != 3 || res.SkippedLinks != 1 {
		t.Errorf("result = %+v, want 3 files and 1 skipped link", res)
	}
	if _, err := os.Lstat(filepath.Join(dir, "escape")); err == nil {
		t.Error("a symlink was exported into the arm")
	}
	if n := git(t, dir, "rev-list", "--count", "HEAD"); n != "1" {
		t.Errorf("arm has %s commits, want 1", n)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != res.Head {
		t.Errorf("Result.Head = %s, repo HEAD = %s", res.Head, got)
	}
	if st := git(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("arm is not clean after materialise: %q", st)
	}
}

func TestMaterialise_NoRemoteNoCredentialsNoRealGit(t *testing.T) {
	repo, base := realRepo(t)
	dir := filepath.Join(t.TempDir(), "arm-1")
	if _, err := Materialise(context.Background(), Spec{Repo: repo, BaseSHA: base, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if r := git(t, dir, "remote"); r != "" {
		t.Errorf("arm has a remote: %q", r)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
	for _, bad := range []string{"example.invalid", "secret", "url", "credential", "origin"} {
		if strings.Contains(string(cfg), bad) {
			t.Errorf("arm .git/config mentions %q: %s", bad, cfg)
		}
	}
	// No object or ref of the real repo: the arm has exactly one commit and
	// the real second commit's sha is absent from its object store.
	realHead := git(t, repo, "rev-parse", "HEAD")
	cmd := exec.Command("git", "cat-file", "-e", realHead)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	if cmd.Run() == nil {
		t.Error("the real repo's HEAD object is reachable from the arm")
	}
	if fi, err := os.Lstat(filepath.Join(dir, ".git")); err != nil || !fi.IsDir() {
		t.Errorf(".git must be the arm's own directory (not a gitfile/link): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "post-commit")); err == nil {
		t.Error("the real repo's hook was carried into the arm")
	}
	if _, err := os.Stat("/tmp/should-not-run"); err == nil {
		t.Error("a hook of the real repo ran")
	}
}

func TestMaterialise_RefusesDirtyOrUnknownSHA(t *testing.T) {
	repo, base := realRepo(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		spec func(string) Spec
		want error
	}{
		"dirty":      {func(d string) Spec { return Spec{Repo: repo, BaseSHA: base, Dirty: true, Dir: d} }, ErrDirty},
		"unknown":    {func(d string) Spec { return Spec{Repo: repo, BaseSHA: strings.Repeat("a", 40), Dir: d} }, ErrUnknownSHA},
		"ref name":   {func(d string) Spec { return Spec{Repo: repo, BaseSHA: "HEAD", Dir: d} }, ErrBadSHA},
		"option":     {func(d string) Spec { return Spec{Repo: repo, BaseSHA: "--output=/tmp/x", Dir: d} }, ErrBadSHA},
		"short":      {func(d string) Spec { return Spec{Repo: repo, BaseSHA: base[:12], Dir: d} }, ErrBadSHA},
		"relative":   {func(string) Spec { return Spec{Repo: repo, BaseSHA: base, Dir: "arm-rel"} }, ErrBadDir},
		"empty sha":  {func(d string) Spec { return Spec{Repo: repo, BaseSHA: "", Dir: d} }, ErrBadSHA},
		"unclean":    {func(d string) Spec { return Spec{Repo: repo, BaseSHA: base, Dir: d + "/../x"} }, ErrBadDir},
		"no parent":  {func(d string) Spec { return Spec{Repo: repo, BaseSHA: base, Dir: d + "/nope/arm-1"} }, ErrBadDir},
		"nonemptyOK": {func(d string) Spec { return Spec{Repo: repo, BaseSHA: base, Dir: d} }, nil},
	} {
		t.Run(name, func(t *testing.T) {
			d := filepath.Join(t.TempDir(), "arm-1")
			if name == "nonemptyOK" {
				// An existing EMPTY dir is fine; fill it to check the refusal below.
				if err := os.Mkdir(d, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Materialise(ctx, tc.spec(d))
			if !errors.Is(err, tc.want) && !(tc.want == nil && err == nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if _, serr := os.Stat(d); serr == nil {
					t.Error("a refused export left a directory behind")
				}
			}
		})
	}
	t.Run("non-empty dir refused and kept", func(t *testing.T) {
		d := filepath.Join(t.TempDir(), "arm-1")
		write(t, filepath.Join(d, "precious"), "x", 0o644)
		_, err := Materialise(ctx, Spec{Repo: repo, BaseSHA: base, Dir: d})
		if !errors.Is(err, ErrBadDir) {
			t.Fatalf("err = %v", err)
		}
		if _, serr := os.Stat(filepath.Join(d, "precious")); serr != nil {
			t.Error("a pre-existing file was deleted by the failure cleanup")
		}
	})
}

func TestMaterialise_RealRepoUnchanged(t *testing.T) {
	repo, base := realRepo(t)
	before := treeHash(t, repo, "")
	headBefore := git(t, repo, "rev-parse", "HEAD")
	refsBefore := git(t, repo, "for-each-ref")
	dir := filepath.Join(t.TempDir(), "arm-1")
	if _, err := Materialise(context.Background(), Spec{Repo: repo, BaseSHA: base, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	// Writing in the arm must not reach the real repo either.
	write(t, filepath.Join(dir, "README.md"), "changed in the arm\n", 0o644)
	git(t, dir, "-c", "user.name=a", "-c", "user.email=a@a", "commit", "-qam", "in arm")
	if got := treeHash(t, repo, ""); got != before {
		t.Error("real repo files/.git changed after Materialise and arm edits")
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Error("real HEAD moved")
	}
	if got := git(t, repo, "for-each-ref"); got != refsBefore {
		t.Error("real refs changed")
	}
	if st := git(t, repo, "status", "--porcelain"); st != "" {
		t.Errorf("real worktree became dirty: %q", st)
	}
}

func TestMaterialise_ExtractRejectsTraversal(t *testing.T) {
	// extract is private; a crafted archive must not write outside dir.
	dir := t.TempDir()
	sub := filepath.Join(dir, "arm-1")
	_ = os.Mkdir(sub, 0o700)
	for _, name := range []string{"../escape.txt", "/abs.txt", "a/../../b.txt"} {
		if _, err := extract(tarOf(t, name, "x"), sub); err == nil {
			t.Errorf("entry %q accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Error("traversal wrote outside the arm")
	}
}

func TestMaterialise_ExtractRejectsDotGit(t *testing.T) {
	for _, name := range []string{".git/config", ".git/hooks/pre-commit", "sub/.git/HEAD", ".GIT/config", "a/.Git/x", ".git", "x/.GiT"} {
		dir := filepath.Join(t.TempDir(), "arm-1")
		_ = os.Mkdir(dir, 0o700)
		if _, err := extract(tarOf(t, name, "x"), dir); !errors.Is(err, ErrUnsafeEntry) {
			t.Errorf("entry %q: err = %v, want ErrUnsafeEntry", name, err)
		}
		if ents, _ := os.ReadDir(dir); len(ents) != 0 {
			t.Errorf("entry %q: wrote %v before refusing", name, ents)
		}
	}
	// Names that merely start with .git are ordinary files.
	dir := filepath.Join(t.TempDir(), "arm-1")
	_ = os.Mkdir(dir, 0o700)
	for _, name := range []string{".gitignore", ".github/workflows/ci.yml", "dir/.gitattributes"} {
		if _, err := extract(tarOf(t, name, "x"), dir); err != nil {
			t.Errorf("entry %q refused: %v", name, err)
		}
	}
	// A traversal entry is the same typed error.
	if _, err := extract(tarOf(t, "../x", "x"), dir); !errors.Is(err, ErrUnsafeEntry) {
		t.Errorf("traversal err = %v, want ErrUnsafeEntry", err)
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func TestMaterialise_ExtractStopsAtTheByteLimitWithoutReadingEverything(t *testing.T) {
	// An endless archive of 1 KiB files, produced on demand.
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		body := bytes.Repeat([]byte("x"), 1024)
		for i := 0; ; i++ {
			if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("f%d", i), Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				return
			}
			if _, err := tw.Write(body); err != nil {
				return
			}
		}
	}()
	defer pr.Close()
	cr := &countingReader{r: pr}
	dir := filepath.Join(t.TempDir(), "arm-1")
	_ = os.Mkdir(dir, 0o700)
	const limit = 1 << 20
	_, err := extractLimit(cr, dir, limit, 1<<30)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// Content over the limit is refused as soon as the budget is spent; the raw
	// stream is cut off shortly after, never read to the end (there is none).
	if got := cr.n.Load(); got > 3*limit {
		t.Errorf("read %d bytes past a %d limit", got, limit)
	}
}

func TestMaterialise_ExtractRefusesALyingHeaderBeforeReadingIt(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	_ = tw.WriteHeader(&tar.Header{Name: "big", Mode: 0o644, Size: 1 << 40, Typeflag: tar.TypeReg})
	cr := &countingReader{r: io.MultiReader(&b, zeroReader{})}
	dir := filepath.Join(t.TempDir(), "arm-1")
	_ = os.Mkdir(dir, 0o700)
	if _, err := extractLimit(cr, dir, 1<<20, 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if got := cr.n.Load(); got > 1<<16 {
		t.Errorf("read %d bytes for a header that already exceeds the limit", got)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
