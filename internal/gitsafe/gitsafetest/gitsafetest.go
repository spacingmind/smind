// Package gitsafetest provides the real-git fixture the gitsafe driver
// tests run against: a repository whose configuration an agent could have
// poisoned, arming a textconv driver, an external diff program and an
// fsmonitor program. Each of the three is a small script that touches a
// marker file when git actually runs it, so a test that runs a hardened git
// call in this repository and then sees no marker has proven the hardening.
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
)

// The marker files each armed driver writes into MarkerDir when git runs it.
const (
	textconvMarker = "textconv.ran"
	extMarker      = "ext.ran"
	fsmMarker      = "fsm.ran"
)

// markerNames lists every marker file, sorted: what Markers can return.
var markerNames = []string{textconvMarker, extMarker, fsmMarker}

// Repo is a git repository with all three diff/status driver classes armed:
// .gitattributes maps *.txt onto a diff driver whose textconv program, the
// repo's diff.external program and its core.fsmonitor program each touch
// their marker file in MarkerDir when git runs them. Dir is the repository;
// MarkerDir holds both the driver scripts and the markers they write.
type Repo struct {
	Dir       string
	MarkerDir string
}

// NewDriverRepo builds a repository that looks exactly like one an agent
// has prepared for the daemon: a normal commit of text and binary files,
// a committed .gitattributes mapping *.txt to the "evil" diff driver, then
// the local config naming the textconv, external diff and fsmonitor
// programs. Each program is a script in MarkerDir that touches its marker
// file when git runs it. An unstaged modification of a.txt and bin.dat is
// left behind so every diff has content to show.
//
// The setup itself never runs the drivers: everything that invokes git
// happens before the config is armed, and what follows the arming only
// writes files. ClearMarkers is called before returning, so a fresh repo
// reports no markers.
//
// NewDriverRepo ignores the user's own git configuration for the whole test
// process (GIT_CONFIG_GLOBAL=/dev/null, GIT_CONFIG_NOSYSTEM=1), which it
// sets with t.Setenv: a test that uses NewDriverRepo must not call
// t.Parallel.
func NewDriverRepo(t testing.TB) *Repo {
	t.Helper()
	r := &Repo{Dir: t.TempDir(), MarkerDir: t.TempDir()}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	r.Git(t, "init", "-b", "main")
	r.Git(t, "config", "user.name", "smind-test")
	r.Git(t, "config", "user.email", "smind-test@example.com")

	r.writeFile(t, "a.txt", []byte("one\n"))
	r.writeFile(t, "b.txt", []byte("keep\n"))
	r.writeFile(t, "bin.dat", []byte{0x00, 0x01, 0x02, 0xff})
	r.Git(t, "add", "-A")
	r.Git(t, "commit", "-m", "init")

	r.writeFile(t, ".gitattributes", []byte("*.txt diff=evil\n"))
	r.Git(t, "add", "-A")
	r.Git(t, "commit", "-m", "arm textconv via attributes")

	// Arming comes last, so nothing above could have run the scripts.
	r.writeScript(t, "textconv.sh", fmt.Sprintf(
		"#!/bin/sh\ntouch %q\ncat \"$1\"\n", r.markerPath(textconvMarker)))
	r.writeScript(t, "ext.sh", fmt.Sprintf(
		"#!/bin/sh\ntouch %q\necho 'external diff output'\n", r.markerPath(extMarker)))
	r.writeScript(t, "fsm.sh", fmt.Sprintf(
		"#!/bin/sh\ntouch %q\nexit 0\n", r.markerPath(fsmMarker)))
	r.Git(t, "config", "diff.evil.textconv", filepath.Join(r.MarkerDir, "textconv.sh"))
	r.Git(t, "config", "diff.external", filepath.Join(r.MarkerDir, "ext.sh"))
	r.Git(t, "config", "core.fsmonitor", filepath.Join(r.MarkerDir, "fsm.sh"))

	r.writeFile(t, "a.txt", []byte("two\n"))
	r.appendFile(t, "bin.dat", []byte{0x41})

	r.ClearMarkers()
	return r
}

// Markers returns the sorted names of the marker files currently in
// MarkerDir, one per armed driver git has run since the last
// ClearMarkers: "ext.ran", "fsm.ran", "textconv.ran". Empty means no
// driver has run.
func (r *Repo) Markers() []string {
	var names []string
	for _, m := range markerNames {
		if _, err := os.Stat(r.markerPath(m)); err == nil {
			names = append(names, m)
		}
	}
	sort.Strings(names)
	return names
}

// ClearMarkers removes every marker file, so a following Markers call
// reports only what git runs from that point on.
func (r *Repo) ClearMarkers() {
	for _, m := range markerNames {
		os.Remove(r.markerPath(m))
	}
}

// Git runs plain git (no hardening of any kind) in Dir and fails the test
// on error, returning git's trimmed stdout. It exists for SETUP ONLY: the
// git call a driver test is measuring must never go through it, or the
// test would be measuring this helper instead of the hardening under test.
//
// The subprocess runs with the user's git configuration ignored
// (GIT_CONFIG_GLOBAL=/dev/null, GIT_CONFIG_NOSYSTEM=1), with no inherited
// GIT_* variable that could steer git's behavior (a stray GIT_EXTERNAL_DIFF
// in the developer's shell would silently decide which driver runs), with
// GIT_TERMINAL_PROMPT=0 so nothing can hang on a prompt, and with a fixed
// author and committer so commits need no configuration of their own.
func (r *Repo) Git(t testing.TB, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = gitEnv()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

// markerPath is where the named driver's marker file lives.
func (r *Repo) markerPath(name string) string {
	return filepath.Join(r.MarkerDir, name)
}

// writeFile writes content as a file of the repository.
func (r *Repo) writeFile(t testing.TB, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, name), content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// appendFile appends content to a file of the repository.
func (r *Repo) appendFile(t testing.TB, name string, content []byte) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(r.Dir, name), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		t.Fatalf("append %s: %v", name, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", name, err)
	}
}

// writeScript writes an executable (0755) shell script into MarkerDir.
func (r *Repo) writeScript(t testing.TB, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.MarkerDir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// gitEnv is the environment Repo.Git runs git with: the process's
// environment without any GIT_* variable, plus the fixed isolation and
// identity documented on Git.
func gitEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_") {
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=smind-test",
		"GIT_AUTHOR_EMAIL=smind-test@example.com",
		"GIT_COMMITTER_NAME=smind-test",
		"GIT_COMMITTER_EMAIL=smind-test@example.com",
	)
}
