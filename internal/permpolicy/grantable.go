package permpolicy

import (
	"regexp"
	"slices"
	"strings"
)

// Saved grants (ADR-0055 section 8, docs/plans/active/saved-grants.md): a
// human may save "allow this exact command in this project". Whether a
// request may be OFFERED that choice, and whether a stored grant may be
// APPLIED to it, is one predicate, decided here beside the rules it must never
// outrank: Decision.Grantable. The engine sets it only on an `ask` that is
// plain (no rule matched, or a segment no allow rule covers) for a command it
// parsed completely, and only when EVERY segment is on the allow-list below.
//
// The list is an allow-list on purpose (decision G8, reversed after review: a
// deny-list leaked for case variants, interpreters, git configuration and
// environment prefixes). A program nobody listed asks every time. A segment
// qualifies only if it has:
//   - a bare program name on the list, spelled exactly (no path, no case
//     variant, no wrapper peeled off), so `./make`, `Make` and `env make` do not;
//   - no environment assignment, no redirect and no dynamic word;
//   - arguments that are plain words (no `..`, `@` or `=`) or flags on that
//     program's own short list: there is no "any arguments" form.
//
// Programs that run project-defined code (make, task, npm run, go test) are on
// the list deliberately: a saved grant for `make build` is the feature, and a
// project whose Makefile an agent can edit is a limit the plan states.

// grantProg constrains one program's arguments: every argument is either a
// flag in okFlags (or a short flag with a number attached, -j4) or a plain
// word. There is no "any arguments" form, so a flag nobody listed (an output
// file, a plugin, a config, a program to run) is refused.
type grantProg struct {
	// verbs, when set, restricts the first argument to this set (a subcommand);
	// the verb's own flags are okFlags[verb] when present, else okFlags[""].
	verbs []string
	// okFlags are the flags allowed after the verb.
	okFlags []string
}

// plainWord is a path, a target, a script or a number: it starts with a letter,
// digit, underscore, dot or slash and holds no `@` (a module version), `=`
// (an assignment) or shell-significant character.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./][A-Za-z0-9_./:,+~-]*$`)

var grantAllow = map[string]grantProg{
	// build and test drivers: the target or script is a plain word
	"make":  {okFlags: []string{"-j", "-k", "-s", "-n"}},
	"task":  {},
	"go":    {verbs: []string{"test", "build", "vet", "fmt"}, okFlags: []string{"-v", "-race", "-short", "-count", "-run", "-cover", "-json", "-timeout", "-tags", "-n", "-x"}},
	"cargo": {verbs: []string{"test", "build", "check", "clippy", "fmt", "doc"}, okFlags: []string{"-q", "-v", "--all", "--workspace", "--release", "--all-targets", "--all-features", "--check", "--no-run", "--lib", "--bins", "--tests"}},
	"npm":   {verbs: []string{"test", "run", "t"}},
	"pnpm":  {verbs: []string{"test", "run", "t"}},
	"yarn":  {verbs: []string{"test"}},
	// test and lint tools: the few flags that only change what is printed or selected
	"pytest":   {okFlags: []string{"-q", "-v", "-vv", "-vvv", "-x", "-s", "-k", "--tb", "--maxfail", "--lf", "--ff", "--collect-only", "-ra", "-rA"}},
	"tsc":      {okFlags: []string{"--noEmit"}},
	"eslint":   {},
	"prettier": {okFlags: []string{"--check"}},
	"vitest":   {verbs: []string{"run"}},
	// read-only inspection (the floor already judges the paths they name)
	"ls":   {okFlags: []string{"-l", "-a", "-la", "-al", "-lh", "-lah", "-1", "-R", "-h", "-d", "-t"}},
	"pwd":  {},
	"cat":  {okFlags: []string{"-n"}},
	"head": {okFlags: []string{"-n"}},
	"tail": {okFlags: []string{"-n"}},
	"wc":   {okFlags: []string{"-l", "-w", "-c"}},
	"grep": {okFlags: []string{"-n", "-i", "-w", "-l", "-c", "-v", "-F", "-E", "-r", "-R", "-H", "-h", "-s", "-A", "-B", "-C", "-m"}},
	"rg":   {okFlags: []string{"-n", "-i", "-w", "-l", "-c", "-v", "-F", "-S", "-s", "-A", "-B", "-C", "-m", "--hidden", "--no-heading"}},
	"diff": {okFlags: []string{"-u", "-q", "-r", "-w"}},
	"echo": {},
	"true": {},
	// git: read-only subcommands only, no global option before the verb, and
	// no flag that writes a file, runs a program or reaches outside the repo
	"git": {verbs: []string{"status", "diff", "log", "show", "rev-parse", "ls-files", "blame", "describe"}, okFlags: []string{"-s", "-b", "--short", "--branch", "--porcelain", "--stat",
		"--name-only", "--name-status", "--cached", "--staged", "--oneline", "--graph", "--decorate", "--no-color", "-n", "-p", "--abbrev-ref", "--show-toplevel", "-U", "--summary", "--shortstat"}},
}

// segmentGrantable reports whether one command segment may be covered by a grant.
func segmentGrantable(s Segment, fs *floorSeg) bool {
	if s.Dynamic || s.Inspected || len(s.Argv) == 0 || len(s.Env) > 0 || len(s.Redirects) > 0 || len(fs.assigns) > 0 {
		return false
	}
	// A wrapper peeled off argv means the command runs something else.
	if len(fs.argv) != len(s.Argv) {
		return false
	}
	prog := s.Argv[0]
	if strings.ContainsAny(prog, `/\`) { // a path-qualified program runs that file, whatever it is called
		return false
	}
	p, ok := grantAllow[prog] // exact, case-sensitive
	if !ok || fs.name() != prog {
		return false
	}
	args := s.Argv[1:]
	if len(p.verbs) > 0 {
		if len(args) == 0 || !slices.Contains(p.verbs, args[0]) {
			return false
		}
		args = args[1:]
	}
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-"):
			if !flagAllowed(a, p.okFlags) {
				return false
			}
		case !plainWord.MatchString(a) || hasParentComponent(a):
			return false
		}
	}
	return true
}

// hasParentComponent reports a `..` path component: the grant must not point a
// plain command at a directory outside the project it was saved for.
func hasParentComponent(a string) bool {
	return slices.Contains(strings.Split(a, "/"), "..")
}

// flagAllowed reports whether flag (maybe `-count=1`, `-j4`) is one of ok.
func flagAllowed(flag string, ok []string) bool {
	name, _, _ := strings.Cut(flag, "=")
	for _, o := range ok {
		if name == o || (len(o) == 2 && strings.HasPrefix(name, o) && name != o && isDigits(name[2:])) {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// grantableSegments reports whether every segment of a plain-ask command may be
// covered by a saved grant (see the package comment above).
func grantableSegments(req Request, segs []Segment, cwd string) bool {
	if len(segs) == 0 || req.Worktree == "" {
		return false
	}
	for _, s := range segs {
		if len(s.Argv) == 0 {
			return false
		}
		if !segmentGrantable(s, newFloorSeg(req, s, cwd)) {
			return false
		}
	}
	return true
}
