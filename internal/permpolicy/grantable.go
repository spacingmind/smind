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
//   - arguments within that program's own constraints (plain words, no flag
//     that loads or runs code).
//
// Programs that run project-defined code (make, task, npm run, go test) are on
// the list deliberately: a saved grant for `make build` is the feature, and a
// project whose Makefile an agent can edit is a limit the plan states.

// grantProg constrains one program's arguments.
type grantProg struct {
	// verbs, when set, restricts the first argument to this set (a subcommand).
	verbs []string
	// deny adds flags refused for this program on top of codeFlags.
	deny []string
	// okFlags are the only flags allowed after the verb when anyArgs is false;
	// every other argument must be a plain word.
	okFlags []string
	// anyArgs allows any arguments except the denied flags (read-only tools).
	anyArgs bool
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./][A-Za-z0-9_./:@,+~-]*$`)

// codeFlags are long and Go-style flags that load or run other code or reach
// outside the project; they are refused for every program on the list.
var codeFlags = []string{"-exec", "--exec", "-toolexec", "-vettool", "-overlay", "--pre", "--compress-program", "--config", "--exec-path", "--ext-diff", "--textconv",
	"--output", "--plugin", "--require", "--import", "--loader", "--eval", "--upload-pack", "--receive-pack", "--manifest-path", "--git-dir", "--work-tree", "--no-index", "--rootdir"}

var grantAllow = map[string]grantProg{
	// build and test drivers: the target or script is a plain word
	"make":  {okFlags: []string{"-j", "-k", "-s", "-n"}},
	"task":  {},
	"go":    {verbs: []string{"test", "build", "vet", "fmt", "list"}, okFlags: []string{"-v", "-race", "-short", "-count", "-run", "-cover", "-json", "-timeout", "-tags", "-l", "-d", "-w", "-m", "-f"}},
	"cargo": {verbs: []string{"test", "build", "check", "clippy", "fmt", "doc"}, okFlags: []string{"-q", "-v", "--all", "--workspace", "--release", "--all-targets", "--all-features", "--check", "--no-run", "--lib", "--bins", "--tests"}},
	"npm":   {verbs: []string{"test", "run", "t"}},
	"pnpm":  {verbs: []string{"test", "run", "t"}},
	"yarn":  {verbs: []string{"test", "run"}},
	// test and lint tools: any plain arguments; -p and -c load plugins and config
	"pytest":   {anyArgs: true, deny: []string{"-p", "-c"}},
	"tsc":      {anyArgs: true},
	"eslint":   {anyArgs: true, deny: []string{"-c"}},
	"prettier": {anyArgs: true},
	"vitest":   {verbs: []string{"run"}},
	// read-only inspection (the floor already judges the paths they name)
	"ls": {anyArgs: true}, "pwd": {anyArgs: true}, "cat": {anyArgs: true}, "head": {anyArgs: true}, "tail": {anyArgs: true}, "wc": {anyArgs: true},
	"grep": {anyArgs: true}, "rg": {anyArgs: true}, "diff": {anyArgs: true}, "echo": {anyArgs: true},
	"true": {anyArgs: true}, "which": {anyArgs: true}, "date": {anyArgs: true}, "stat": {anyArgs: true}, "file": {anyArgs: true}, "tree": {anyArgs: true},
	// git: read-only subcommands only, and no global option before the verb
	"git": {verbs: []string{"status", "diff", "log", "show", "rev-parse", "ls-files", "blame", "describe", "shortlog"}, anyArgs: true},
}

// denied reports whether argument a is the flag d, `d=value`, or (for a short
// flag) d with its value attached.
func denied(a, d string) bool {
	if a == d || strings.HasPrefix(a, d+"=") {
		return true
	}
	return len(d) == 2 && !strings.HasPrefix(a, "--") && strings.HasPrefix(a, d) && len(a) > 2
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
	for _, a := range args {
		for _, d := range slices.Concat(codeFlags, p.deny) {
			if denied(a, d) {
				return false
			}
		}
	}
	if len(p.verbs) > 0 {
		if len(args) == 0 || !slices.Contains(p.verbs, args[0]) {
			return false
		}
		args = args[1:]
	}
	if p.anyArgs {
		return true
	}
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-"):
			if !flagAllowed(a, p.okFlags) {
				return false
			}
		case !plainWord.MatchString(a):
			return false
		}
	}
	return true
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
