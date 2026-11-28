// Package gitsafe is the one hardening every daemon-side git call carries
// (P0 A2): the flags and environment that keep git from running a program
// the repository's own configuration names.
//
// The daemon runs git inside task worktrees whose configuration an agent
// can edit (through the worktree an agent reaches the shared .git/config,
// .git/info/attributes and the in-tree .gitattributes). git executes, as
// the daemon, programs named in that configuration: diff.<driver>.textconv
// and diff.external run while producing a diff, core.fsmonitor runs on
// status and diff. Args and Env close exactly those three doors, so no
// call site has to remember them individually.
package gitsafe

import (
	"os"
	"strings"
)

// globalOptionWithValue lists git's global options that take their value as
// the next element; when searching for the subcommand, that value is a
// value, not the subcommand.
var globalOptionWithValue = map[string]bool{
	"-c":          true,
	"-C":          true,
	"--git-dir":   true,
	"--work-tree": true,
	"--namespace": true,
}

// diffSubcommand lists the subcommands the daemon uses that render a diff and
// therefore would run a configured diff driver: an external diff program
// (diff.external) or a textconv filter (diff.<driver>.textconv named from
// .gitattributes). `blame`, `reflog -p`, `diff-tree`, `diff-index`,
// `format-patch` and `stash show` can render patches too (blame and reflog run
// textconv) but take no --no-ext-diff / --no-textconv in the same shape and the
// daemon calls none of them; a new use must be hardened where it is added (the
// call-site audit in audit_test.go names every spawn of git).
var diffSubcommand = map[string]bool{
	"diff":        true,
	"show":        true,
	"log":         true,
	"whatchanged": true,
}

// droppedEnv is the environment variables Env removes, with "=" so a
// prefix match never hits a longer name.
var droppedEnv = []string{
	"GIT_EXTERNAL_DIFF=",
	"GIT_PAGER=",
	"GIT_TERMINAL_PROMPT=",
	"GIT_OPTIONAL_LOCKS=",
}

// Args returns args carrying the hardening every daemon-side git call
// needs, as a new slice; args itself is never modified.
//
// It prepends "-c core.fsmonitor=false" (unless args already contains that
// exact pair): core.fsmonitor is a program the repository's config can
// name and git runs it on status and diff, so it is forced off. It is a
// global option, so it must come before the subcommand.
//
// For the diff family of subcommands (diff, show, log, whatchanged) it
// inserts "--no-ext-diff" and "--no-textconv" immediately after the
// subcommand, each only when that exact flag is not already present:
// external diff and textconv are the two ways a repo's config can name a
// program to run while producing a diff. Only the arguments before a bare
// "--" are inspected; everything after "--" is a pathspec and is never
// read or touched.
//
// The subcommand is found by walking the leading global options: -c, -C,
// --git-dir, --work-tree and --namespace each consume the next element as
// their value, any other element starting with "-" is a lone global option
// (--no-optional-locks, --git-dir=/x, --no-pager); the first element that
// does not start with "-" is the subcommand. When there is none, the
// result is just the prefix followed by the input unchanged. Every other
// subcommand and every other argument is left exactly as given, in order.
func Args(args ...string) []string {
	out := make([]string, 0, len(args)+4)
	// Only the arguments before a bare "--" are options: a pathspec that happens to read
	// `-c core.fsmonitor=false` must not suppress the prefix (git reads it as a path).
	if !hasPair(beforePathspecs(args), "-c", "core.fsmonitor=false") {
		out = append(out, "-c", "core.fsmonitor=false")
	}
	sub := subcommandIndex(args)
	if sub < 0 || !diffSubcommand[args[sub]] {
		return append(out, args...)
	}
	searched := beforePathspecs(args)
	out = append(out, args[:sub+1]...)
	if !hasFlag(searched, "--no-ext-diff") {
		out = append(out, "--no-ext-diff")
	}
	if !hasFlag(searched, "--no-textconv") {
		out = append(out, "--no-textconv")
	}
	return append(out, args[sub+1:]...)
}

// Env returns base (os.Environ() when base is nil) as a new slice with
// every GIT_EXTERNAL_DIFF, GIT_PAGER, GIT_TERMINAL_PROMPT and
// GIT_OPTIONAL_LOCKS entry removed, everything else in its original order,
// and GIT_PAGER=cat, GIT_TERMINAL_PROMPT=0 and GIT_OPTIONAL_LOCKS=0
// appended. base itself is never modified.
//
// GIT_EXTERNAL_DIFF names an external diff program, so whatever the
// daemon's own environment carries must not reach the subprocess.
// GIT_PAGER=cat keeps git from waiting on an interactive pager its output
// is piped to. GIT_TERMINAL_PROMPT=0 makes a credential prompt fail instead
// of hanging the daemon. GIT_OPTIONAL_LOCKS=0 keeps read-only commands
// from taking the worktree's index.lock while an agent is working in it.
func Env(base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base)+3)
outer:
	for _, e := range base {
		for _, d := range droppedEnv {
			if strings.HasPrefix(e, d) {
				continue outer
			}
		}
		out = append(out, e)
	}
	return append(out, "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
}

// subcommandIndex returns the index of args' subcommand, or -1 when args
// holds global options only.
func subcommandIndex(args []string) int {
	for i := 0; i < len(args); i++ {
		if globalOptionWithValue[args[i]] {
			i++ // the option's value, not the subcommand
			continue
		}
		if strings.HasPrefix(args[i], "-") {
			continue // a lone global option
		}
		return i
	}
	return -1
}

// hasPair reports whether args contains flag immediately followed by value.
func hasPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// hasFlag reports whether args contains flag exactly.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// beforePathspecs returns args up to, not including, the first bare "--"
// (after which every element is a pathspec); all of args when there is no
// bare "--".
func beforePathspecs(args []string) []string {
	for i, a := range args {
		if a == "--" {
			return args[:i]
		}
	}
	return args
}
