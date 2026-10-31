package permpolicy

import (
	"os"
	"path"
	"strings"
)

// FloorHit names the compiled-in floor rule that denied a request. The floor
// is not configurable (ADR-0047 §2.1): no policy file can allow what it
// denies, so a hit wins over every rule.
type FloorHit struct{ ID, Reason string }

var floorReasons = map[string]string{
	"no-stash":               "git stash hides work from the task branch",
	"no-force-push":          "force pushes and ref deletions rewrite shared history",
	"no-merge":               "merging, tagging and releasing belong to the human",
	"no-install-state":       "the install state, credentials and the daemon are off limits",
	"no-secrets":             "secrets and environment dumps must not reach the transcript",
	"no-destructive-outside": "destructive commands must stay inside the task worktree",
	"no-curl-sh":             "downloaded code must not run unreviewed",
	"no-real-home-tests":     "tests must run against a temporary HOME, not the real one",
	"no-add-all":             "stage named files, never the whole tree",
}

func floorHit(id string) (FloorHit, bool) {
	return FloorHit{ID: id, Reason: floorReasons[id]}, true
}

// CheckFloor runs every floor rule over every segment of a Bash command. It
// is called even when p.Unparsed is non-empty: the segments are best effort,
// but a hit among them is still a hit, and a deny must not be downgraded to
// an ask just because another part of the line could not be parsed.
func CheckFloor(req Request, p Parsed) (FloorHit, bool) {
	cwd := req.Cwd
	if cwd == "" {
		cwd = req.Worktree
	}
	// Compared with resolved roots later, so the start must be resolved too.
	if cwd != "" {
		cwd, _ = resolveFloor(req, cwd, "")
	}
	segs := make([]*floorSeg, len(p.Segments))
	for i, seg := range p.Segments {
		segs[i] = newFloorSeg(req, seg, cwd)
	}
	if curlShell(segs) {
		return floorHit("no-curl-sh")
	}
	for _, seg := range p.Segments {
		fs := newFloorSeg(req, seg, cwd)
		if h, ok := fs.gitRules(); ok {
			return h, true
		}
		if h, ok := fs.pathRules(); ok {
			return h, true
		}
		if h, ok := fs.envRules(); ok {
			return h, true
		}
		cwd = fs.cwdAfter()
	}
	return FloorHit{}, false
}

// floorSeg is one segment seen through its wrappers: argv is the effective
// command, assigns the NAME=val words that apply to it (from the segment and
// from env), unsets the names env -u removes, wrappers the commands peeled
// off in order. cwd is the working directory the segment runs in, "" when a
// `cd` to something unresolvable made it unknown.
type floorSeg struct {
	req      Request
	seg      Segment
	argv     []string
	assigns  []string
	unsets   []string
	wrappers []string
	cwd      string
}

func newFloorSeg(req Request, seg Segment, cwd string) *floorSeg {
	fs := &floorSeg{req: req, seg: seg, cwd: cwd, argv: seg.Argv}
	fs.assigns = append(fs.assigns, seg.Env...)
	fs.unwrap()
	return fs
}

// unwrap peels wrappers off argv repeatedly so `nohup env A=1 git stash`
// is judged as `git stash`. env -S carries a whole command line in one
// word, which is re-split and unwrapped again.
func (fs *floorSeg) unwrap() {
	for n := 0; n < 16 && len(fs.argv) > 0; n++ {
		name := path.Base(fs.argv[0])
		rest := fs.argv[1:]
		switch name {
		case "env":
			rest = fs.unwrapEnv(rest)
		case "command", "nohup", "time", "builtin":
			rest = skipFlags(rest, nil)
		case "exec":
			rest = skipFlags(rest, map[string]bool{"-a": true})
		case "nice":
			rest = skipFlags(rest, map[string]bool{"-n": true, "--adjustment": true})
		case "timeout":
			rest = skipFlags(rest, map[string]bool{"-s": true, "-k": true})
			if len(rest) > 0 {
				rest = rest[1:] // the duration
			}
		case "xargs":
			rest = skipFlags(rest, map[string]bool{
				"-I": true, "-n": true, "-L": true, "-P": true, "-d": true,
				"-E": true, "-s": true, "-a": true, "-R": true, "-S": true,
			})
		default:
			return
		}
		fs.wrappers = append(fs.wrappers, name)
		fs.argv = rest
	}
}

// unwrapEnv consumes env's options and NAME=val words and returns what is
// left to run.
func (fs *floorSeg) unwrapEnv(rest []string) []string {
	for len(rest) > 0 {
		w := rest[0]
		switch {
		case w == "--":
			return rest[1:]
		case w == "-u" || w == "--unset":
			if len(rest) > 1 {
				fs.unsets = append(fs.unsets, rest[1])
			}
			rest = rest[min(2, len(rest)):]
		case strings.HasPrefix(w, "--unset="):
			fs.unsets = append(fs.unsets, w[len("--unset="):])
			rest = rest[1:]
		case strings.HasPrefix(w, "-u") && !strings.HasPrefix(w, "--"):
			fs.unsets = append(fs.unsets, w[2:])
			rest = rest[1:]
		case w == "-C" || w == "--chdir":
			rest = rest[min(2, len(rest)):]
		case w == "-S" || w == "--split-string":
			if len(rest) < 2 {
				return nil
			}
			return append(strings.Fields(rest[1]), rest[2:]...)
		case strings.HasPrefix(w, "-"):
			rest = rest[1:]
		case isEnvAssign(w):
			fs.assigns = append(fs.assigns, w)
			rest = rest[1:]
		default:
			return rest
		}
	}
	return nil
}

// skipFlags drops leading option words; an option listed in withValue also
// consumes the word after it. "--" ends the options.
func skipFlags(rest []string, withValue map[string]bool) []string {
	for len(rest) > 0 {
		w := rest[0]
		if w == "--" {
			return rest[1:]
		}
		if !strings.HasPrefix(w, "-") || w == "-" {
			return rest
		}
		if withValue[w] {
			rest = rest[min(2, len(rest)):]
		} else {
			rest = rest[1:]
		}
	}
	return rest
}

// cwdAfter is the working directory for the next segment: `cd X` moves it,
// to "" (unknown) when X is dynamic or unresolvable, because a later
// destructive check then cannot prove it stays inside the worktree.
func (fs *floorSeg) cwdAfter() string {
	if len(fs.argv) == 0 || fs.argv[0] != "cd" {
		return fs.cwd
	}
	args := nonFlags(fs.argv[1:])
	if len(args) == 0 {
		return floorHome(fs.req)
	}
	return fs.chdir(fs.cwd, args[0])
}

// floorHome is the human's real home: the request's, else the process's.
func floorHome(req Request) string {
	if req.Home != "" {
		return req.Home
	}
	home, _ := os.UserHomeDir()
	return home
}

// nonFlags returns the words that are not options; everything after a bare
// "--" counts as a word.
func nonFlags(args []string) []string {
	var out []string
	rest := false
	for _, a := range args {
		switch {
		case rest:
			out = append(out, a)
		case a == "--":
			rest = true
		case !strings.HasPrefix(a, "-") || a == "-":
			out = append(out, a)
		}
	}
	return out
}

// gitCall is a git command line with its global options stripped: sub is the
// subcommand, args what follows it, cwd the directory it acts in (-C moves it).
type gitCall struct {
	sub  string
	args []string
	cwd  string
}

// parseGit finds the subcommand after git's global options, which may carry
// a value in the next word (-C, -c, --git-dir X, ...).
func (fs *floorSeg) parseGit() (gitCall, bool) {
	if len(fs.argv) == 0 || path.Base(fs.argv[0]) != "git" {
		return gitCall{}, false
	}
	g := gitCall{cwd: fs.cwd}
	rest := fs.argv[1:]
	for len(rest) > 0 {
		w := rest[0]
		switch {
		case w == "-C":
			if len(rest) > 1 {
				g.cwd = fs.chdir(g.cwd, rest[1])
			}
			rest = rest[min(2, len(rest)):]
		case w == "-c" || w == "--git-dir" || w == "--work-tree" || w == "--namespace" || w == "--exec-path":
			rest = rest[min(2, len(rest)):]
		case strings.HasPrefix(w, "-"):
			rest = rest[1:]
		default:
			g.sub, g.args = w, rest[1:]
			return g, true
		}
	}
	return g, false
}

// chdir applies a -C style directory word to cwd; "" is unknown.
func (fs *floorSeg) chdir(cwd, dir string) string {
	resolved, ok := resolveFloor(fs.req, dir, cwd)
	if !ok {
		return ""
	}
	return resolved
}

// longOpt reports whether arg is a long option that names (or abbreviates,
// git accepts unique prefixes) one of names.
func longOpt(arg string, names ...string) bool {
	if !strings.HasPrefix(arg, "--") || len(arg) == 2 {
		return false
	}
	name, _, _ := strings.Cut(arg[2:], "=")
	for _, n := range names {
		if strings.HasPrefix(n[2:], name) {
			return true
		}
	}
	return false
}

// shortCluster reports whether arg is a short-option cluster containing one
// of letters (-fu contains f).
func shortCluster(arg, letters string) bool {
	return len(arg) > 1 && arg[0] == '-' && arg[1] != '-' && strings.ContainsAny(arg[1:], letters)
}

func (fs *floorSeg) gitRules() (FloorHit, bool) {
	if h, ok := fs.ghRules(); ok {
		return h, true
	}
	if h, ok := fs.publishRules(); ok {
		return h, true
	}
	g, ok := fs.parseGit()
	if !ok {
		return FloorHit{}, false
	}
	switch g.sub {
	case "stash":
		return floorHit("no-stash")
	case "merge":
		return floorHit("no-merge")
	case "push":
		if forcePush(g.args) {
			return floorHit("no-force-push")
		}
		if pushesProtected(g.args) {
			return floorHit("no-merge")
		}
	case "tag":
		if createsTag(g.args) {
			return floorHit("no-merge")
		}
	case "add":
		if addsAll(g.args) {
			return floorHit("no-add-all")
		}
	}
	return FloorHit{}, false
}

func forcePush(args []string) bool {
	for _, a := range args {
		switch {
		case longOpt(a, "--force", "--force-with-lease", "--force-if-includes", "--mirror", "--delete"):
			return true
		case shortCluster(a, "fd"):
			return true
		case strings.HasPrefix(a, "+"):
			return true
		case strings.HasPrefix(a, ":") && len(a) > 1:
			return true
		}
	}
	return false
}

// pushesProtected is a push that lands on develop, master or main, or that
// publishes tags. Every non-flag word is judged as a refspec, remote
// included: a bare word is its own destination.
func pushesProtected(args []string) bool {
	for _, a := range args {
		if longOpt(a, "--tags", "--follow-tags") {
			return true
		}
	}
	for _, a := range nonFlags(args) {
		if a == "tag" || strings.Contains(a, "refs/tags/") {
			return true
		}
		a = strings.TrimPrefix(a, "+")
		if i := strings.LastIndexByte(a, ':'); i >= 0 {
			a = a[i+1:]
		}
		switch strings.TrimPrefix(a, "refs/heads/") {
		case "develop", "master", "main":
			return true
		}
	}
	return false
}

// createsTag is a git tag that makes a tag: any positional argument, unless
// an option turns the command into a listing, verification or deletion.
func createsTag(args []string) bool {
	for _, a := range args {
		if a == "-l" || a == "-d" || a == "-v" || strings.HasPrefix(a, "-n") ||
			longOpt(a, "--list", "--delete", "--verify", "--contains", "--no-contains", "--merged", "--no-merged", "--points-at") {
			return false
		}
	}
	return len(nonFlags(args)) > 0
}

func addsAll(args []string) bool {
	for _, a := range args {
		if longOpt(a, "--all", "--update") || shortCluster(a, "Au") {
			return true
		}
	}
	for _, a := range nonFlags(args) {
		switch a {
		case ".", "./", ":/", "*":
			return true
		}
	}
	return false
}

// ghRules covers the gh commands that merge or release. Words are compared
// after dropping options, so `gh pr merge --squash 5` and `gh -R x pr merge`
// are both seen.
func (fs *floorSeg) ghRules() (FloorHit, bool) {
	if len(fs.argv) == 0 || path.Base(fs.argv[0]) != "gh" {
		return FloorHit{}, false
	}
	// The value of a global flag (-R owner/repo) is a non-flag word, so the
	// verbs are looked for among all words, not just the first.
	words := nonFlags(fs.argv[1:])
	switch {
	case containsWord(words, "pr") && containsWord(words, "merge"):
		return floorHit("no-merge")
	case containsWord(words, "release"):
		return floorHit("no-merge")
	case containsWord(words, "api"):
		for _, a := range fs.argv[1:] {
			if strings.Contains(a, "/merge") {
				return floorHit("no-merge")
			}
		}
	case containsWord(words, "workflow") && containsWord(words, "run"):
		for _, a := range fs.argv[1:] {
			if strings.Contains(strings.ToLower(a), "release") {
				return floorHit("no-merge")
			}
		}
	}
	return FloorHit{}, false
}

// publishRules covers package publishing; `yarn npm publish` puts the verb
// second, so the first two words are checked.
func (fs *floorSeg) publishRules() (FloorHit, bool) {
	if len(fs.argv) == 0 {
		return FloorHit{}, false
	}
	switch path.Base(fs.argv[0]) {
	case "npm", "yarn", "pnpm", "bun", "cargo":
		words := nonFlags(fs.argv[1:])
		if containsWord(words[:min(2, len(words))], "publish") {
			return floorHit("no-merge")
		}
	}
	return FloorHit{}, false
}

func containsWord(words []string, want string) bool {
	for _, w := range words {
		if w == want {
			return true
		}
	}
	return false
}
