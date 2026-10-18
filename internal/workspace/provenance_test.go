package workspace

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// gitT runs git in dir with the user identity passed via `-c` (never the
// ambient global config) and GIT_CONFIG_GLOBAL pointed at /dev/null, so
// the test repo is isolated from the developer's real git configuration.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
}

// newProvenanceRepo returns a temp git repo with one committed file.
func newProvenanceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitT(t, dir, "init")
	gitT(t, dir, "commit", "--allow-empty", "-m", "initial commit")
	return dir
}

func TestGitHead_GitDirty(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		dir := newProvenanceRepo(t)
		head, err := GitHead(context.Background(), dir)
		if err != nil {
			t.Fatalf("GitHead() error = %v", err)
		}
		if len(head) != 40 {
			t.Fatalf("GitHead() = %q, want a full 40-char sha", head)
		}
		dirty, err := GitDirty(context.Background(), dir)
		if err != nil {
			t.Fatalf("GitDirty() error = %v", err)
		}
		if dirty {
			t.Fatal("GitDirty() = true on a clean repo, want false")
		}
	})

	t.Run("modified tracked file", func(t *testing.T) {
		dir := newProvenanceRepo(t)
		f := filepath.Join(dir, "README.md")
		if err := os.WriteFile(f, []byte("changed\n"), 0o644); err != nil {
			t.Fatalf("write README: %v", err)
		}
		dirty, err := GitDirty(context.Background(), dir)
		if err != nil {
			t.Fatalf("GitDirty() error = %v", err)
		}
		if !dirty {
			t.Fatal("GitDirty() = false with a modified tracked file, want true")
		}
	})

	t.Run("untracked file", func(t *testing.T) {
		dir := newProvenanceRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write new.txt: %v", err)
		}
		dirty, err := GitDirty(context.Background(), dir)
		if err != nil {
			t.Fatalf("GitDirty() error = %v", err)
		}
		if !dirty {
			t.Fatal("GitDirty() = false with an untracked file, want true")
		}
	})

	t.Run("ignored file is clean", func(t *testing.T) {
		dir := newProvenanceRepo(t)
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
			t.Fatalf("write .gitignore: %v", err)
		}
		gitT(t, dir, "add", ".gitignore")
		gitT(t, dir, "commit", "-m", "gitignore")
		if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write ignored.txt: %v", err)
		}
		dirty, err := GitDirty(context.Background(), dir)
		if err != nil {
			t.Fatalf("GitDirty() error = %v", err)
		}
		if dirty {
			t.Fatal("GitDirty() = true with only an ignored file, want false")
		}
	})

	t.Run("non-git dir", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := GitHead(context.Background(), dir); err == nil {
			t.Fatal("GitHead() error = nil on a non-git dir, want error")
		}
		if _, err := GitDirty(context.Background(), dir); err == nil {
			t.Fatal("GitDirty() error = nil on a non-git dir, want error")
		}
	})

	// Not parallel: it empties PATH for the whole process.
	t.Run("git missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := GitHead(context.Background(), t.TempDir()); err == nil {
			t.Fatal("GitHead() error = nil with git missing, want error")
		}
		if _, err := GitDirty(context.Background(), t.TempDir()); err == nil {
			t.Fatal("GitDirty() error = nil with git missing, want error")
		}
	})

	t.Run("no commits yet", func(t *testing.T) {
		dir := t.TempDir()
		gitT(t, dir, "init")
		if _, err := GitHead(context.Background(), dir); err == nil {
			t.Fatal("GitHead() error = nil before the first commit, want error")
		}
		// No commits yet says nothing about dirtiness; GitDirty must still
		// answer (an empty repo is clean).
		dirty, err := GitDirty(context.Background(), dir)
		if err != nil {
			t.Fatalf("GitDirty() error = %v", err)
		}
		if dirty {
			t.Fatal("GitDirty() = true on an empty repo, want false")
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		dir := newProvenanceRepo(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := GitHead(ctx, dir); err == nil {
			t.Fatal("GitHead() error = nil with a cancelled context, want error")
		}
		if _, err := GitDirty(ctx, dir); err == nil {
			t.Fatal("GitDirty() error = nil with a cancelled context, want error")
		}
	})
}

// fakeGitOnPath puts a `git` shim first on PATH that runs body (a shell
// snippet). The real git stays reachable behind it for any later call.
func fakeGitOnPath(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell shim")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A git that spawns a child which outlives it (an fsmonitor hook, a
// credential helper) keeps the output pipes open; the ctx deadline must
// still return promptly instead of waiting for that child.
func TestGitHead_HungGitReturnsAtDeadline(t *testing.T) {
	// Not parallel: PATH. The shim's `sleep` is a child that outlives the
	// killed shell, and ends by itself after 4s.
	dir := newProvenanceRepo(t)
	fakeGitOnPath(t, "sleep 4")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := GitHead(ctx, dir); err == nil {
		t.Fatal("GitHead() error = nil, want an error from the hung git")
	}
	if _, err := GitDirty(ctx, dir); err == nil {
		t.Fatal("GitDirty() error = nil, want an error from the hung git")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("took %v, want return shortly after the 300ms deadline", took)
	}
}

// The repo config is editable by the agent, so provenance must not run an
// fsmonitor hook it names.
func TestGitDirty_DoesNotRunFsmonitorHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hook")
	}
	dir := newProvenanceRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "config", "core.fsmonitor", hook)

	if _, err := GitDirty(context.Background(), dir); err != nil {
		t.Fatalf("GitDirty() error = %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("fsmonitor hook ran under GitDirty")
	}

	// Control: plain `git status` does run it (else this test proves nothing).
	gitT(t, dir, "status")
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git does not run the configured fsmonitor hook on status; hardening untestable here")
	}
}

// A directory that lost its own .git must not read as the parent repo.
func TestGitHead_NotARepoInsideParentRepo(t *testing.T) {
	parent := newProvenanceRepo(t)
	sub := filepath.Join(parent, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if sha, err := GitHead(context.Background(), sub); err == nil {
		t.Fatalf("GitHead(sub of a repo) = %q, want an error (not a repo)", sha)
	}
	if _, err := GitDirty(context.Background(), sub); err == nil {
		t.Fatal("GitDirty(sub of a repo) error = nil, want an error")
	}
}
