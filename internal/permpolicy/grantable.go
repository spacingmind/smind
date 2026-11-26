package permpolicy

import (
	"regexp"
	"slices"
	"strings"
)

// Saved grants (ADR-0055 section 8, docs/plans/completed/saved-grants.md): a
// human may save "allow this exact command in this project". Whether a
// request may be OFFERED that choice, and whether a stored grant may be
// APPLIED to it, is one predicate, decided here beside the rules it must never
// outrank: Decision.Grantable. The engine sets it only on an `ask` that is
// plain (no rule matched, or a segment no allow rule covers) for a command it
// parsed completely, and only when EVERY segment is on the allow-list below;
// the allow-list has two classes (Decision.GrantClass): read-only inspection, and
// runners of project-defined code that a grant may cover only with an opt-in.
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
// Programs that run project-defined code (make, task, npm run, go test) are the
// runner class: the Human decided (2026-11-15) that a grant is read-only unless
// that specific grant opts in to running project code.

// Grant classes. The daemon decides which class a command belongs to from the
// compiled lists below; nothing a caller sends can change it.
const (
	// GrantReadOnly: the command only inspects (git read verbs, ls, cat, grep,
	// find without -exec or -delete, ...). A grant may cover it with no opt-in.
	GrantReadOnly = "readonly"
	// GrantRunner: the command runs code the PROJECT defines (make, task,
	// npm run, go test, cargo, pytest, tsc, ...). A grant may cover it only with
	// an explicit opt-in stored on that grant; without it the command is asked
	// every time. A human-approved `make build` is arbitrary code execution if
	// the agent later edits the Makefile.
	GrantRunner = "runner"
)

// grantProg constrains one program's arguments: every argument is either a
// flag in okFlags (or a short flag with a number attached, -j4) or a plain
// word. There is no "any arguments" form, so a flag nobody listed (an output
// file, a plugin, a config, a program to run) is refused.
type grantProg struct {
	class string
	// verbs, when set, restricts the first argument to this set (a subcommand).
	verbs []string
	// okFlags are the flags allowed after the verb.
	okFlags []string
	// patternFlags are okFlags whose next argument is a glob pattern (find -name).
	patternFlags []string
}

// plainWord is a path, a target, a script or a number: it starts with a letter,
// digit, underscore, dot or slash and holds no `@` (a module version), `=`
// (an assignment) or shell-significant character.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./][A-Za-z0-9_./:,+~-]*$`)

// patternWord is a quoted glob for find -name: plainWord plus `*` and `?`.
var patternWord = regexp.MustCompile(`^[A-Za-z0-9_./*?][A-Za-z0-9_./*?:,+~-]*$`)

var grantAllow = map[string]grantProg{
	// READ-ONLY: inspection only (the floor already judges the paths they name)
	"ls":   {class: GrantReadOnly, okFlags: []string{"-l", "-a", "-la", "-al", "-lh", "-lah", "-1", "-R", "-h", "-d", "-t"}},
	"pwd":  {class: GrantReadOnly},
	"cat":  {class: GrantReadOnly, okFlags: []string{"-n"}},
	"head": {class: GrantReadOnly, okFlags: []string{"-n"}},
	"tail": {class: GrantReadOnly, okFlags: []string{"-n"}},
	"wc":   {class: GrantReadOnly, okFlags: []string{"-l", "-w", "-c"}},
	"grep": {class: GrantReadOnly, okFlags: []string{"-n", "-i", "-w", "-l", "-c", "-v", "-F", "-E", "-r", "-R", "-H", "-h", "-s", "-A", "-B", "-C", "-m"}},
	"rg":   {class: GrantReadOnly, okFlags: []string{"-n", "-i", "-w", "-l", "-c", "-v", "-F", "-S", "-s", "-A", "-B", "-C", "-m", "--hidden", "--no-heading"}},
	"diff": {class: GrantReadOnly, okFlags: []string{"-u", "-q", "-r", "-w"}},
	"echo": {class: GrantReadOnly},
	"true": {class: GrantReadOnly},
	// find: only the predicates that read; never -exec, -execdir, -ok, -okdir,
	// -delete, -fprint*, -fls (none is listed, so each is refused)
	"find": {class: GrantReadOnly, okFlags: []string{"-name", "-iname", "-path", "-ipath", "-type", "-maxdepth", "-mindepth", "-size", "-mtime", "-mmin", "-newer", "-empty", "-print", "-not"},
		patternFlags: []string{"-name", "-iname", "-path", "-ipath"}},
	// git: read-only subcommands only, no global option before the verb, and
	// no flag that writes a file, runs a program or reaches outside the repo
	"git": {class: GrantReadOnly, verbs: []string{"status", "diff", "log", "show", "rev-parse", "ls-files", "blame", "describe"}, okFlags: []string{"-s", "-b", "--short", "--branch", "--porcelain", "--stat",
		"--name-only", "--name-status", "--cached", "--staged", "--oneline", "--graph", "--decorate", "--no-color", "-n", "-p", "--abbrev-ref", "--show-toplevel", "-U", "--summary", "--shortstat"}},

	// RUNNER: runs project-defined code; needs a per-grant opt-in
	"make":     {class: GrantRunner, okFlags: []string{"-j", "-k", "-s", "-n"}},
	"task":     {class: GrantRunner},
	"go":       {class: GrantRunner, verbs: []string{"test", "build", "run", "vet", "fmt"}, okFlags: []string{"-v", "-race", "-short", "-count", "-run", "-cover", "-json", "-timeout", "-tags", "-n", "-x"}},
	"cargo":    {class: GrantRunner, verbs: []string{"test", "build", "run", "check", "clippy", "fmt", "doc"}, okFlags: []string{"-q", "-v", "--all", "--workspace", "--release", "--all-targets", "--all-features", "--check", "--no-run", "--lib", "--bins", "--tests"}},
	"npm":      {class: GrantRunner, verbs: []string{"test", "run", "t"}},
	"pnpm":     {class: GrantRunner, verbs: []string{"test", "run", "t"}},
	"yarn":     {class: GrantRunner, verbs: []string{"test"}},
	"bun":      {class: GrantRunner, verbs: []string{"test", "run"}},
	"pytest":   {class: GrantRunner, okFlags: []string{"-q", "-v", "-vv", "-vvv", "-x", "-s", "-k", "--tb", "--maxfail", "--lf", "--ff", "--collect-only", "-ra", "-rA"}},
	"tsc":      {class: GrantRunner, okFlags: []string{"--noEmit"}},
	"eslint":   {class: GrantRunner},
	"prettier": {class: GrantRunner, okFlags: []string{"--check"}},
	"vitest":   {class: GrantRunner, verbs: []string{"run"}},
}

// segmentClass returns the class of one command segment, "" when it may not be
// covered by a grant at all.
func segmentClass(s Segment, fs *floorSeg) string {
	if s.Dynamic || s.Inspected || len(s.Argv) == 0 || len(s.Env) > 0 || len(s.Redirects) > 0 || len(fs.assigns) > 0 {
		return ""
	}
	// A wrapper peeled off argv means the command runs something else.
	if len(fs.argv) != len(s.Argv) {
		return ""
	}
	prog := s.Argv[0]
	if strings.ContainsAny(prog, `/\`) { // a path-qualified program runs that file, whatever it is called
		return ""
	}
	p, ok := grantAllow[prog] // exact, case-sensitive
	if !ok || fs.name() != prog {
		return ""
	}
	args := s.Argv[1:]
	if len(p.verbs) > 0 {
		if len(args) == 0 || !slices.Contains(p.verbs, args[0]) {
			return ""
		}
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "-"):
			if !flagAllowed(a, p.okFlags) {
				return ""
			}
			if slices.Contains(p.patternFlags, a) { // the next word is a glob pattern
				i++
				if i >= len(args) || !patternWord.MatchString(args[i]) || hasParentComponent(args[i]) {
					return ""
				}
			}
		case !plainWord.MatchString(a) || hasParentComponent(a):
			return ""
		}
	}
	return p.class
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

// hasParentComponent reports a `..` path component: the grant must not point a
// plain command at a directory outside the project it was saved for.
func hasParentComponent(a string) bool {
	return slices.Contains(strings.Split(a, "/"), "..")
}

// grantClass returns the class of a whole plain-ask command: "" when any
// segment may not be covered (never grantable), GrantRunner when any segment
// runs project code, GrantReadOnly when every segment only inspects.
func grantClass(req Request, segs []Segment, cwd string) string {
	if len(segs) == 0 || req.Worktree == "" {
		return ""
	}
	class := GrantReadOnly
	for _, s := range segs {
		c := segmentClass(s, newFloorSeg(req, s, cwd))
		switch c {
		case "":
			return ""
		case GrantRunner:
			class = GrantRunner
		}
	}
	return class
}
