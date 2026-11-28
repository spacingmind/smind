package gitsafe

import (
	"bytes"
	"context"
	"fmt"
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

// runGit runs `git argv...` in dir with env, feeding it stdin when
// non-empty, and returns stdout. The run's error is deliberately dropped
// (stderr with it): git failing is fine in both modes (a neutralised
// merge conflicts, an armed program fails) — repo.Markers() is the
// verdict, not git's exit status.
func runGit(t *testing.T, dir string, timeout time.Duration, stdin string, argv []string, env []string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Dir = dir
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
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
	return runGit(t, repo.Dir, timeout, stdin, argv, StrippedEnv(nil))
}

// runHuman builds the human-mode argv (the baseline Args) and runs it
// under Env: what a person's own git call carries.
func runHuman(t *testing.T, repo *gitsafetest.StrippedRepo, args ...string) string {
	return runHumanFor(t, repo, gitTimeout, "", args...)
}

// runHumanFor is runHuman with an explicit timeout and stdin bytes.
func runHumanFor(t *testing.T, repo *gitsafetest.StrippedRepo, timeout time.Duration, stdin string, args ...string) string {
	t.Helper()
	return runGit(t, repo.Dir, timeout, stdin, Args(args...), Env(nil))
}

// runDaemonIn is runDaemon for a plain repository directory: the eol
// attributes test runs against a fresh repository, not the armed fixture.
func runDaemonIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv, err := StrippedArgs(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("StrippedArgs(%q): %v", args, err)
	}
	return runGit(t, dir, gitTimeout, "", argv, StrippedEnv(nil))
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

// Test_real_git_daemon_mode_keeps_a_global_filter is AC3: only the
// repository-controlled configuration is neutralised. A filter and a
// credential helper defined in the person's global configuration keep
// running in daemon mode, and the repository's own helper does not.
func Test_real_git_daemon_mode_keeps_a_global_filter(t *testing.T) {
	repo := gitsafetest.NewStrippedRepo(t)

	// The person's global configuration: a filter for *.glb and a
	// credential helper, each a script that touches its own marker.
	script := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	glob := filepath.Join(repo.MarkerDir, "glob.sh")
	gcred := filepath.Join(repo.MarkerDir, "gcred.sh")
	script(glob, fmt.Sprintf("#!/bin/sh\ntouch %q\ncat\n", filepath.Join(repo.MarkerDir, "glob.ran")))
	script(gcred, fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 0\n", filepath.Join(repo.MarkerDir, "gcred.ran")))
	global := fmt.Sprintf("[filter \"glob\"]\n\tclean = %s\n[credential]\n\thelper = %s\n", glob, gcred)
	f, err := os.OpenFile(repo.Global, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", repo.Global, err)
	}
	if _, err := f.WriteString(global); err != nil {
		t.Fatalf("append %s: %v", repo.Global, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", repo.Global, err)
	}

	// The *.glb attribute comes from the repository, the filter it names
	// from the person's own configuration.
	attrs := filepath.Join(repo.Dir, ".git", "info", "attributes")
	f, err = os.OpenFile(attrs, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", attrs, err)
	}
	if _, err := f.WriteString("*.glb filter=glob\n"); err != nil {
		t.Fatalf("append %s: %v", attrs, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", attrs, err)
	}

	// a.glb, added in daemon mode, runs the person's global clean filter.
	if err := os.WriteFile(filepath.Join(repo.Dir, "a.glb"), []byte("glb\n"), 0o644); err != nil {
		t.Fatalf("write a.glb: %v", err)
	}
	runDaemon(t, repo, "add", "a.glb")
	if _, err := os.Stat(filepath.Join(repo.MarkerDir, "glob.ran")); err != nil {
		t.Fatalf("the global filter did not run in daemon mode: %v", err)
	}

	// A daemon-mode credential fill runs the global helper only: the
	// repository's own helper is reset away, the person's is re-added.
	repo.ClearMarkers()
	runDaemonFor(t, repo, gitTimeout, "protocol=https\nhost=example.com\n\n", "credential", "fill")
	if _, err := os.Stat(filepath.Join(repo.MarkerDir, "gcred.ran")); err != nil {
		t.Fatalf("the global credential helper did not run in daemon mode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.MarkerDir, "cred.ran")); err == nil {
		t.Fatal("the repository's credential helper ran in daemon mode")
	}
}

// Test_real_git_eol_attributes_do_not_show_spurious_changes is AC5: the
// stripped mode must not drop the repository's attributes — the reason a
// blanket --attr-source was rejected. A file the `text eol=crlf` attribute
// governs, committed through the same conversion, shows no change in
// daemon mode: git still converts it exactly the way it was committed.
func Test_real_git_eol_attributes_do_not_show_spurious_changes(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	plain := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	plain("init", "-b", "main")
	plain("config", "user.name", "smind-test")
	plain("config", "user.email", "smind-test@example.com")
	writeFile := func(name string, content []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeFile(".gitattributes", []byte("* text eol=crlf\n"))
	// The working-tree copy carries CRLF; committing through plain git
	// runs the clean conversion, so the index holds LF.
	writeFile("crlf.txt", []byte("one\r\ntwo\r\n"))
	plain("add", "-A")
	plain("commit", "-m", "init")

	if got := runDaemonIn(t, dir, "status", "--porcelain"); got != "" {
		t.Fatalf("daemon-mode status --porcelain printed %q, want nothing: the eol attribute must still apply", got)
	}
}

// labRepo makes a plain repository (no armed fixture) with an initial
// commit and returns its directory, a plain-git runner for setup and a
// marker path; the user's git configuration is ignored.
func labRepo(t *testing.T) (dir string, git func(args ...string), marker string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir = t.TempDir()
	marker = filepath.Join(t.TempDir(), "marker")
	git = func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "smind-test")
	git("config", "user.email", "smind-test@example.com")
	return dir, git, marker
}

// labScript writes a script that touches marker then runs tail.
func labScript(t *testing.T, marker, tail string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.sh")
	if err := os.WriteFile(p, []byte(fmt.Sprintf("#!/bin/sh\ntouch %q\n%s\n", marker, tail)), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// Test_real_git_daemon_mode_equals_in_a_subsection_never_runs_the_program:
// the armed filter whose name contains '=' cannot be neutralised, so
// StrippedArgs refuses and no git call is made.
func Test_real_git_daemon_mode_equals_in_a_subsection_never_runs_the_program(t *testing.T) {
	dir, git, marker := labRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "init")
	git("config", "filter.a=b.clean", labScript(t, marker, "cat"))
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt filter=a=b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := StrippedArgs(context.Background(), dir, "status"); err == nil {
		t.Fatal("StrippedArgs accepted a key with '=' in its subsection")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the filter ran")
	}
}

// Test_real_git_daemon_mode_never_runs_an_ext_transport_program: a local
// protocol.ext.allow=always with an ext:: remote URL would run a program on
// fetch and ls-remote; daemon mode turns the transport off.
func Test_real_git_daemon_mode_never_runs_an_ext_transport_program(t *testing.T) {
	dir, git, marker := labRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "init")
	script := labScript(t, marker, "exit 1")
	git("config", "protocol.ext.allow", "always")
	git("config", "remote.o.url", "ext::"+script+" %S")

	for _, args := range [][]string{{"fetch", "o"}, {"ls-remote", "o"}} {
		runDaemonIn(t, dir, args...)
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("daemon-mode %v ran the ext program", args)
		}
	}
	for _, args := range [][]string{{"fetch", "o"}, {"ls-remote", "o"}} {
		runGit(t, dir, gitTimeout, "", Args(args...), Env(nil))
	}
	if _, err := os.Stat(marker); err != nil {
		t.Log("human mode did not run the ext program on this platform's git; the lab is not armed")
	}
}
