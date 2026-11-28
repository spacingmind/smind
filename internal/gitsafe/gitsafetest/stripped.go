// The stripped-mode fixture: a repository armed with every program class
// the stripped mode must neutralise (P0 "daemon-initiated git runs
// stripped"). Where NewDriverRepo arms the three A2 diff/status classes,
// this arms hooks, a clean/smudge filter, a process filter, a merge
// driver, a credential helper, an ssh command, a signing program, an
// uploadpack program and an askpass program: each is a small script in
// MarkerDir that touches its marker file when git runs it, so a test that
// runs a stripped git call in this repository and sees no marker has
// proven the neutralisation, and one that runs a plain (human-mode) call
// and sees the marker has proven the class was left alone.
//
// It is a normal (non _test.go) package so the tests of other packages can
// import it; it must therefore never be imported from production code, or
// the testing package would ship in the daemon.
package gitsafetest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// strippedMarkerNames lists every marker file the armed programs write
// into MarkerDir, sorted: what Markers can report.
var strippedMarkerNames = []string{
	"askpass.ran",
	"cred.ran",
	"filter.ran",
	"gpg.ran",
	"hook.ran",
	"merge.ran",
	"ssh.ran",
	"uploadpack.ran",
}

// StrippedRepo is a git repository that looks exactly like one an agent
// has prepared for the daemon: hooks in .git/hooks, filter and merge
// attributes in .git/info/attributes and a committed .gitattributes, and
// local configuration naming a program for each class. Dir is the
// repository; MarkerDir holds the armed scripts and the markers they
// write; Bare is a local bare repository wired up as remote origin (so a
// fetch needs no network); Global is the person's global configuration
// file, which starts EMPTY so a test may append its own entries to it
// (GIT_CONFIG_GLOBAL points at it, and the system configuration is off).
type StrippedRepo struct {
	Dir       string
	MarkerDir string
	Bare      string
	Global    string
}

// NewStrippedRepo builds the armed repository.
//
// The setup itself never runs an armed program: every git call happens
// before the configuration is armed, and what follows the arming only
// writes files. ClearMarkers is called before returning, so a fresh repo
// reports no markers.
//
// It ignores the user's own git configuration for the whole test process
// (GIT_CONFIG_GLOBAL=Global, GIT_CONFIG_NOSYSTEM=1, both through
// t.Setenv), so a test that uses it must not call t.Parallel.
func NewStrippedRepo(t testing.TB) *StrippedRepo {
	t.Helper()
	r := &StrippedRepo{
		Dir:       t.TempDir(),
		MarkerDir: t.TempDir(),
		Bare:      t.TempDir(),
		Global:    filepath.Join(t.TempDir(), "gitconfig"),
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := os.WriteFile(r.Global, nil, 0o644); err != nil {
		t.Fatalf("write %s: %v", r.Global, err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", r.Global)

	r.Git(t, "init", "-b", "main")
	r.Git(t, "config", "user.name", "smind-test")
	r.Git(t, "config", "user.email", "smind-test@example.com")

	// The initial commit carries the three files the tests modify, and the
	// committed .gitattributes naming the process filter: an undefined
	// filter is a no-op, so arming the config later is what makes it live.
	r.writeFile(t, "a.flt", []byte("flt\n"))
	r.writeFile(t, "a.mrg", []byte("base\n"))
	r.writeFile(t, "a.fl2", []byte("fl2\n"))
	r.writeFile(t, ".gitattributes", []byte("*.fl2 filter=evilp\n"))
	r.Git(t, "add", "-A")
	r.Git(t, "commit", "-m", "init")

	// A local bare remote, so `fetch origin` and `ls-remote` contact
	// something without a network.
	r.Git(t, "init", "--bare", r.Bare)
	r.Git(t, "remote", "add", "origin", r.Bare)

	// `side` changes a.mrg differently from main, so `git merge side`
	// needs the merge driver (the attribute is armed below).
	r.Git(t, "checkout", "-q", "-b", "side")
	r.writeFile(t, "a.mrg", []byte("side\n"))
	r.Git(t, "add", "-A")
	r.Git(t, "commit", "-m", "side")
	r.Git(t, "checkout", "-q", "main")
	r.writeFile(t, "a.mrg", []byte("main\n"))
	r.Git(t, "add", "-A")
	r.Git(t, "commit", "-m", "main change")
	r.Git(t, "push", "-q", "origin", "main")

	// The commits above leave racily clean index entries (a file committed
	// in the same second the index was written), and a racy entry makes a
	// later `git status` re-run the clean filter just to re-check it, which
	// would touch a marker without anything really happening. Give the
	// working files an mtime strictly older than the index and refresh
	// once, with nothing armed yet.
	past := time.Now().Add(-time.Hour)
	for _, name := range []string{"a.flt", "a.mrg", "a.fl2", ".gitattributes"} {
		if err := os.Chtimes(filepath.Join(r.Dir, name), past, past); err != nil {
			t.Fatalf("chtime %s: %v", name, err)
		}
	}
	r.Git(t, "status", "--porcelain")

	// Arming comes last, so nothing above could have run the programs.
	hook := fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 0\n", r.markerPath("hook.ran"))
	r.writeExec(t, filepath.Join(".git", "hooks", "pre-commit"), hook)
	r.writeExec(t, filepath.Join(".git", "hooks", "pre-push"), hook)
	r.writeExec(t, filepath.Join(".git", "hooks", "post-checkout"), hook)
	r.writeFile(t, filepath.Join(".git", "info", "attributes"), []byte("*.flt filter=evil\n*.mrg merge=evilm\n"))

	// The clean and smudge filters cat stdin to stdout; the process filter
	// does the same (it does not speak the filter protocol, so a call that
	// runs it fails — that is fine, the marker is the point).
	filter := fmt.Sprintf("#!/bin/sh\ntouch %q\ncat\n", r.markerPath("filter.ran"))
	r.writeScript(t, "clean.sh", filter)
	r.writeScript(t, "smudge.sh", filter)
	r.writeScript(t, "process.sh", filter)
	// The merge driver: %O %A %B arrive as $1 $2 $3, and the driver must
	// leave the merged result in %A, the second argument, then exit 0.
	r.writeScript(t, "merge.sh", fmt.Sprintf("#!/bin/sh\ntouch %q\ncp \"$2\" \"$1\"\nexit 0\n", r.markerPath("merge.ran")))
	// The credential helper prints nothing and exits 0.
	r.writeScript(t, "cred.sh", fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 0\n", r.markerPath("cred.ran")))
	// The ssh script, the uploadpack program, the signing program and the
	// askpass program just fail: exiting 1 makes the git call fail without
	// hanging, and the marker still lands.
	fail := func(marker string) string {
		return fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 1\n", r.markerPath(marker))
	}
	r.writeScript(t, "ssh.sh", fail("ssh.ran"))
	r.writeScript(t, "uploadpack.sh", fail("uploadpack.ran"))
	r.writeScript(t, "gpg.sh", fail("gpg.ran"))
	r.writeScript(t, "askpass.sh", fail("askpass.ran"))

	r.Git(t, "config", "filter.evil.clean", r.scriptPath("clean.sh"))
	r.Git(t, "config", "filter.evil.smudge", r.scriptPath("smudge.sh"))
	r.Git(t, "config", "filter.evilp.process", r.scriptPath("process.sh"))
	r.Git(t, "config", "merge.evilm.driver", r.scriptPath("merge.sh"))
	r.Git(t, "config", "credential.helper", r.scriptPath("cred.sh"))
	r.Git(t, "config", "core.sshCommand", r.scriptPath("ssh.sh"))
	r.Git(t, "config", "core.askPass", r.scriptPath("askpass.sh"))
	r.Git(t, "config", "commit.gpgsign", "true")
	r.Git(t, "config", "gpg.program", r.scriptPath("gpg.sh"))
	r.Git(t, "config", "remote.origin.uploadpack", r.scriptPath("uploadpack.sh"))

	r.ClearMarkers()
	return r
}

// Markers returns the sorted names of the marker files currently in
// MarkerDir, one per armed program git has run since the last
// ClearMarkers. Empty means nothing has run.
func (r *StrippedRepo) Markers() []string {
	var names []string
	for _, m := range strippedMarkerNames {
		if _, err := os.Stat(r.markerPath(m)); err == nil {
			names = append(names, m)
		}
	}
	sort.Strings(names)
	return names
}

// ClearMarkers removes every marker file, so a following Markers call
// reports only what git runs from that point on.
func (r *StrippedRepo) ClearMarkers() {
	for _, m := range strippedMarkerNames {
		os.Remove(r.markerPath(m))
	}
}

// Git runs plain git (no hardening of any kind) in Dir and fails the test
// on error, returning git's trimmed stdout. It exists for SETUP ONLY: the
// git call a stripped-mode test is measuring must never go through it, or
// the test would be measuring this helper instead of the mode under test.
//
// The subprocess runs with the fixture environment documented on env.
func (r *StrippedRepo) Git(t testing.TB, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = r.env()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

// Run is Git without the test failure: it returns git's stdout and the
// error, because a neutralised (or an armed) program often makes git
// itself fail, and the markers are what the caller wants to judge.
func (r *StrippedRepo) Run(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = r.env()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("git %v: %w\n%s", args, err, errb.String())
	}
	return out.Bytes(), err
}

// env is the environment Git and Run run git with: the process's
// environment without any GIT_* variable (a stray GIT_SSH_COMMAND or
// GIT_EXTERNAL_DIFF would silently decide which program runs), with the
// fixture's own global configuration file, the system configuration off,
// no terminal prompt and a fixed author and committer.
func (r *StrippedRepo) env() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_") {
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"GIT_CONFIG_GLOBAL="+r.Global,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=smind-test",
		"GIT_AUTHOR_EMAIL=smind-test@example.com",
		"GIT_COMMITTER_NAME=smind-test",
		"GIT_COMMITTER_EMAIL=smind-test@example.com",
	)
}

// markerPath is where the named marker file lives.
func (r *StrippedRepo) markerPath(name string) string {
	return filepath.Join(r.MarkerDir, name)
}

// scriptPath is where the named armed script lives.
func (r *StrippedRepo) scriptPath(name string) string {
	return filepath.Join(r.MarkerDir, name)
}

// writeFile writes content as a file of the repository (0644), name
// relative to Dir.
func (r *StrippedRepo) writeFile(t testing.TB, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// writeExec writes an executable (0755) file into the repository, name
// relative to Dir: the hooks.
func (r *StrippedRepo) writeExec(t testing.TB, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// writeScript writes an executable (0755) shell script into MarkerDir.
func (r *StrippedRepo) writeScript(t testing.TB, name, body string) {
	t.Helper()
	if err := os.WriteFile(r.scriptPath(name), []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
