package gitsafe

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/gitsafe/gitsafetest"
)

// The real-git tests of the two modes, run against gitsafetest's armed
// repository. They must not call t.Parallel: the fixture owns the process
// environment (GIT_CONFIG_GLOBAL, GIT_CONFIG_NOSYSTEM) through t.Setenv.

const (
	// gitTimeout bounds every git call these tests make: nothing here may
	// hang, whatever the armed configuration does.
	gitTimeout = 20 * time.Second
	// sshTimeout is the tighter bound for the ssh call, whose failure mode
	// (a DNS lookup on a non-existent host) can be slow.
	sshTimeout = 10 * time.Second
)

// runGit runs `git argv...` in repo.Dir with env, feeding it stdin when
// non-empty, and returns the combined output. The run's error is
// deliberately dropped: git failing is fine in both modes (a neutralised
// merge conflicts, an armed program fails) — repo.Markers() is the
// verdict, not git's exit status.
func runGit(t *testing.T, repo *gitsafetest.StrippedRepo, timeout time.Duration, stdin string, argv []string, env []string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Dir = repo.Dir
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	return out.String()
}

// runDaemon builds the daemon-mode argv (StrippedArgs, against the
// repository itself) and runs it under StrippedEnv: what every
// daemon-initiated git call carries.
func runDaemon(t *testing.T, repo *gitsafetest.StrippedRepo, args ...string) string {
	return runDaemonFor(t, repo, gitTimeout, "", args...)
}

// runDaemonFor is runDaemon with an explicit timeout (the ssh call gets a
// shorter one) and stdin bytes (credential fill reads its request).
func runDaemonFor(t *testing.T, repo *gitsafetest.StrippedRepo, timeout time.Duration, stdin string, args ...string) string {
	t.Helper()
	argv, err := StrippedArgs(context.Background(), repo.Dir, args...)
	if err != nil {
		t.Fatalf("StrippedArgs(%q): %v", args, err)
	}
	return runGit(t, repo, timeout, stdin, argv, StrippedEnv(nil))
}

// runHuman builds the human-mode argv (the baseline Args) and runs it
// under Env: what a person's own git call carries.
func runHuman(t *testing.T, repo *gitsafetest.StrippedRepo, args ...string) string {
	return runHumanFor(t, repo, gitTimeout, "", args...)
}

// runHumanFor is runHuman with an explicit timeout and stdin bytes.
func runHumanFor(t *testing.T, repo *gitsafetest.StrippedRepo, timeout time.Duration, stdin string, args ...string) string {
	t.Helper()
	return runGit(t, repo, timeout, stdin, Args(args...), Env(nil))
}

// modeRuns bundles the two runner shapes one mode provides, so the two
// tests can share one list of operations.
type modeRuns struct {
	call    func(args ...string) string
	callFor func(timeout time.Duration, stdin string, args ...string) string
}

// daemonRuns is the daemon mode's runners.
func daemonRuns(t *testing.T, repo *gitsafetest.StrippedRepo) modeRuns {
	return modeRuns{
		call: func(args ...string) string { return runDaemon(t, repo, args...) },
		callFor: func(timeout time.Duration, stdin string, args ...string) string {
			return runDaemonFor(t, repo, timeout, stdin, args...)
		},
	}
}

// humanRuns is the human mode's runners.
func humanRuns(t *testing.T, repo *gitsafetest.StrippedRepo) modeRuns {
	return modeRuns{
		call: func(args ...string) string { return runHuman(t, repo, args...) },
		callFor: func(timeout time.Duration, stdin string, args ...string) string {
			return runHumanFor(t, repo, timeout, stdin, args...)
		},
	}
}

// armedOps returns the operations the daemon performs on a task worktree,
// run through one mode's runners: the checkpoint (modify, add, commit),
// the sync reads and writes (merge, fetch, credentials, ssh, a signed
// tag), status and worktree creation.
func armedOps(t *testing.T, repo *gitsafetest.StrippedRepo, mode modeRuns) []struct {
	name string
	run  func()
} {
	wt := filepath.Join(t.TempDir(), "wt2")
	return []struct {
		name string
		run  func()
	}{
		{"modify and add -A", func() {
			for _, f := range []struct{ name, content string }{
				{"a.flt", "flt armed\n"},
				{"a.fl2", "fl2 armed\n"},
				{"a.mrg", "mrg armed\n"},
			} {
				if err := os.WriteFile(filepath.Join(repo.Dir, f.name), []byte(f.content), 0o644); err != nil {
					t.Fatalf("write %s: %v", f.name, err)
				}
			}
			mode.call("add", "-A")
		}},
		{"commit", func() { mode.call("commit", "-m", "x") }},
		{"merge side", func() { mode.call("merge", "side") }},
		{"fetch origin", func() { mode.call("fetch", "origin") }},
		{"credential fill", func() {
			mode.callFor(gitTimeout, "protocol=https\nhost=example.com\n\n", "credential", "fill")
		}},
		{"ls-remote ssh", func() {
			mode.callFor(sshTimeout, "", "ls-remote", "ssh://nohost.invalid/x")
		}},
		{"tag -s", func() { mode.call("tag", "-s", "v1", "-m", "x") }},
		{"status", func() { mode.call("status") }},
		{"worktree add", func() { mode.call("worktree", "add", wt, "-b", "wt2") }},
	}
}

// Test_real_git_daemon_mode_runs_no_armed_program is AC1: in the armed
// repository, none of the operations the daemon starts runs any armed
// program. The git calls themselves may fail (a neutralised merge
// conflicts, a neutralised credential fill refuses to prompt): a program
// not running is the requirement, git succeeding is not.
func Test_real_git_daemon_mode_runs_no_armed_program(t *testing.T) {
	repo := gitsafetest.NewStrippedRepo(t)

	for _, op := range armedOps(t, repo, daemonRuns(t, repo)) {
		op.run()
		if got := repo.Markers(); len(got) != 0 {
			t.Logf("after %s: markers %q", op.name, got)
		}
	}
	if got := repo.Markers(); len(got) != 0 {
		t.Fatalf("daemon mode ran armed programs: %q", got)
	}
}

// Test_real_git_human_mode_runs_the_armed_programs is AC2: the same
// repository, the same operations, human mode: the repository's own
// programs run. Markers are cleared between operations, and the working
// state reset first, so each operation's markers are attributable (the
// armed signing program makes the commit fail and leave its staged
// changes behind, which would otherwise block the merge). askpass is not
// required: it only runs when a prompt is reached.
func Test_real_git_human_mode_runs_the_armed_programs(t *testing.T) {
	repo := gitsafetest.NewStrippedRepo(t)
	seen := map[string]bool{}
	for _, op := range armedOps(t, repo, humanRuns(t, repo)) {
		_, _ = repo.Run("merge", "--abort")
		_, _ = repo.Run("reset", "--hard", "HEAD")
		repo.ClearMarkers()
		op.run()
		got := repo.Markers()
		t.Logf("%s ran %q", op.name, got)
		for _, m := range got {
			seen[m] = true
		}
	}
	for _, want := range []string{
		"hook.ran", "filter.ran", "merge.ran", "cred.ran",
		"ssh.ran", "gpg.ran", "uploadpack.ran",
	} {
		if !seen[want] {
			t.Errorf("human mode never ran %s", want)
		}
	}
}
