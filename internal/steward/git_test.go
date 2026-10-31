package steward

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolateGitConfig keeps the tests independent of the user's git config:
// no global/system config, identity and signing forced per command.
func isolateGitConfig(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// testGit runs a git command in dir with a forced identity, failing the test
// on error, and returns stdout.
func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"--no-optional-locks", "-C", dir,
		"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}
	cmd := exec.Command("git", append(base, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, stderr.String())
	}
	return stdout.String()
}

// newRepo creates a repository with one initial commit on main and returns
// its dir plus a commit helper that writes path, commits it and returns the
// new head sha.
func newRepo(t *testing.T) (string, func(t *testing.T, dir, path, content, msg string) string) {
	t.Helper()
	isolateGitConfig(t)
	dir := t.TempDir()
	commit := func(t *testing.T, dir, path, content, msg string) string {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		testGit(t, dir, "add", "--", path)
		testGit(t, dir, "commit", "-m", msg)
		return strings.TrimSpace(testGit(t, dir, "rev-parse", "HEAD"))
	}
	testGit(t, dir, "init", "-b", "main")
	commit(t, dir, "readme.md", "readme\n", "initial commit")
	return dir, commit
}

func head(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(testGit(t, dir, "rev-parse", "HEAD"))
}

func TestCollectGit_NumstatBinaryRow(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	base := head(t, dir)
	commit(t, dir, "logo.png", "\x00\x01\x02", "add logo")
	got, err := CollectGit(context.Background(), dir, base, base, head(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Numstat, "-\t-\tlogo.png") {
		t.Errorf("Numstat = %q, want a binary row \"-\\t-\\tlogo.png\"", got.Numstat)
	}
}

func TestCollectGit_CommitsOldestFirstAndAttribution(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	base := head(t, dir)
	first := commit(t, dir, "a.txt", "a\n", "first")
	second := commit(t, dir, "b.txt", "b\n", "second\n\nCo-Authored-By: X <x@x>")
	got, err := CollectGit(context.Background(), dir, base, base, head(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Commits) != 2 {
		t.Fatalf("Commits = %+v, want 2", got.Commits)
	}
	if got.Commits[0].SHA != first || got.Commits[1].SHA != second {
		t.Errorf("Commits order = [%s %s], want oldest first [%s %s]",
			got.Commits[0].SHA, got.Commits[1].SHA, first, second)
	}
	if got.Commits[0].HasAttribution {
		t.Errorf("Commits[0].HasAttribution = true, want false")
	}
	if !got.Commits[1].HasAttribution {
		t.Errorf("Commits[1].HasAttribution = false, want true for the Co-Authored-By trailer")
	}
	if got.Commits[1].Subject != "second" {
		t.Errorf("Commits[1].Subject = %q, want %q", got.Commits[1].Subject, "second")
	}
}

func TestCollectGit_StackedBranchForeignCommits(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	main := head(t, dir)
	testGit(t, dir, "update-ref", "refs/remotes/origin/develop", main)
	testGit(t, dir, "checkout", "-b", "pr1")
	a := commit(t, dir, "a.txt", "A\n", "A")
	testGit(t, dir, "checkout", "-b", "pr2")
	b := commit(t, dir, "b.txt", "B\n", "B")
	// pr2 was based on pr1, so diffBase and baseSHA are pr1's head.
	got, err := CollectGit(context.Background(), dir, a, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stacking.BaseReachableFromDevelop {
		t.Errorf("BaseReachableFromDevelop = true, want false (base is another PR)")
	}
	if len(got.Stacking.ForeignCommits) != 1 || got.Stacking.ForeignCommits[0] != a {
		t.Errorf("ForeignCommits = %v, want [%s]", got.Stacking.ForeignCommits, a)
	}
}

func TestCollectGit_BaseReachableWhenBranchedFromDevelop(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	develop := head(t, dir)
	testGit(t, dir, "update-ref", "refs/remotes/origin/develop", develop)
	testGit(t, dir, "checkout", "-b", "feature")
	c := commit(t, dir, "c.txt", "C\n", "C")
	got, err := CollectGit(context.Background(), dir, develop, develop, c)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stacking.BaseReachableFromDevelop {
		t.Errorf("BaseReachableFromDevelop = false, want true")
	}
	if len(got.Stacking.ForeignCommits) != 0 {
		t.Errorf("ForeignCommits = %v, want empty", got.Stacking.ForeignCommits)
	}
}

func TestCollectGit_NoOriginDevelop(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	base := head(t, dir)
	h := commit(t, dir, "x.txt", "x\n", "x")
	got, err := CollectGit(context.Background(), dir, base, base, h)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stacking.BaseReachableFromDevelop {
		t.Errorf("BaseReachableFromDevelop = true, want false without origin/develop")
	}
	if len(got.Stacking.ForeignCommits) != 0 {
		t.Errorf("ForeignCommits = %v, want empty", got.Stacking.ForeignCommits)
	}
}

func TestCollectGit_RejectsBadRefs(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	base := head(t, dir)
	commit(t, dir, "x.txt", "x\n", "x")
	h := head(t, dir)
	for _, tc := range []struct {
		name     string
		diffBase string
		baseSHA  string
		head     string
	}{
		{"option-shaped diffBase", "--output=x", base, h},
		{"diffBase with whitespace", "a b", base, h},
		{"short head", base, base, "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CollectGit(context.Background(), dir, tc.diffBase, tc.baseSHA, tc.head); err == nil {
				t.Errorf("CollectGit(diffBase=%q, head=%q) succeeded, want error", tc.diffBase, tc.head)
			}
		})
	}
}

func TestSecretScan_CountsOnlyNeverValues(t *testing.T) {
	const awsKey = "AKIAABCDEFGHIJKLMNOP"
	const assignment = "abcdefghijklmnop"
	lines := map[string][]string{
		"foo.go": {
			`key := "` + awsKey + `"`,
			"-----BEGIN RSA PRIVATE KEY-----",
			`token = "` + assignment + `"`,
		},
	}
	got := SecretScan(lines)
	want := map[string]int{"aws-key": 1, "private-key": 1, "assignment": 1}
	for label, n := range want {
		if got[label] != n {
			t.Errorf("SecretScan[%s] = %d, want %d", label, got[label], n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("SecretScan = %v, want exactly %v", got, want)
	}
	s := fmt.Sprint(got)
	for _, secret := range []string{awsKey, "BEGIN RSA PRIVATE KEY", assignment} {
		if strings.Contains(s, secret) {
			t.Errorf("SecretScan leaked a value: %q contains %q", s, secret)
		}
	}
}

func TestCollectGit_SecretSkipsTestFiles(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	base := head(t, dir)
	commit(t, dir, "foo_test.go", "k := \"AKIAABCDEFGHIJKLMNOP\"\n", "add test")
	commit(t, dir, "foo.go", "k := \"AKIAABCDEFGHIJKLMNOP\"\n", "add prod")
	got, err := CollectGit(context.Background(), dir, base, base, head(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if n := got.SecretHits["aws-key"]; n != 1 {
		t.Errorf("SecretHits[aws-key] = %d, want 1 (test files skipped): %v", n, got.SecretHits)
	}
}

func TestCollectGit_ContextCancel(t *testing.T) {
	isolateGitConfig(t)
	dir, commit := newRepo(t)
	base := head(t, dir)
	commit(t, dir, "x.txt", "x\n", "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CollectGit(ctx, dir, base, base, head(t, dir))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("CollectGit error = %v, want context.Canceled", err)
	}
}
