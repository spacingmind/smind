package permpolicy

import (
	"path"
	"regexp"
	"strings"
)

// resolveFloor resolves a word to an absolute path for the floor. Unlike
// SegmentPaths it expands a leading ~, $HOME or ${HOME} from the request's
// Home (the human's real home, not the process's), so `rm -rf ~/.ssh` is
// seen; any other dynamic word, an unresolvable one, or a relative word with
// no known cwd reports false and the engine asks about it.
func resolveFloor(req Request, w, cwd string) (string, bool) {
	if w == "" {
		return "", false
	}
	pl := req.pl()
	home := floorHome(req)
	for _, prefix := range pl.homeForms() {
		if w == prefix || (strings.HasPrefix(w, prefix) && (w[len(prefix)] == '/' || (pl.windows && w[len(prefix)] == '\\'))) {
			if home == "" {
				return "", false
			}
			w = home + w[len(prefix):]
			break
		}
	}
	if pl.dynamicWord(w) {
		return "", false
	}
	resolved, err := pl.resolve(w, cwd)
	if err != nil {
		return "", false
	}
	return resolved, true
}

// wordPath is a command word and, when it names a path, where it resolves.
type wordPath struct {
	raw, resolved string
	ok            bool
}

// wordPaths lists every non-flag word after argv0 (and the value of
// --flag=value) of the effective command, plus redirect targets, each
// resolved against the segment's cwd.
func (fs *floorSeg) wordPaths() []wordPath {
	var out []wordPath
	add := func(w string) {
		if w == "" || fs.pl.isNullDevice(w) {
			return
		}
		r, ok := resolveFloor(fs.req, w, fs.cwd)
		out = append(out, wordPath{raw: w, resolved: r, ok: ok})
	}
	for i := 1; i < len(fs.argv); i++ {
		w := fs.argv[i]
		if strings.HasPrefix(w, "-") {
			_, v, found := strings.Cut(w, "=")
			if !found {
				continue
			}
			w = v
		}
		add(w)
	}
	for _, rd := range fs.seg.Redirects {
		switch rd.Target {
		case "", "&1", "&2", "-":
			continue
		}
		if fs.pl.isNullDevice(rd.Target) {
			continue
		}
		add(rd.Target)
	}
	return out
}

// installRoots are the places no task may touch: the human's install state
// and credentials, and the daemon home. The task worktree lives under
// Home/.smind/worktrees, so it is carved out by the caller.
func installRoots(req Request) []string {
	pl := req.pl()
	home := floorHome(req)
	roots := []string{
		pl.join(home, ".smind"),
		pl.join(home, ".claude"),
		pl.join(home, ".claude.json"),
		pl.join(home, ".config"),
		pl.join(home, ".ssh"),
		pl.join(home, ".gnupg"),
	}
	if pl.windows {
		roots = append(roots, pl.join(home, "AppData"))
	} else {
		roots = append(roots,
			pl.join(home, "Library", "Keychains"),
			pl.join(home, "Library", "LaunchAgents"),
		)
	}
	if req.SmindHome != "" {
		roots = append(roots, req.SmindHome)
	}
	return roots
}

// inInstallState reports whether a resolved path is protected install state.
// An empty home would make every root relative and meaningless, so without
// one nothing is claimed here.
func inInstallState(req Request, resolved string) bool {
	pl := req.pl()
	if req.Worktree != "" && pl.within(resolved, []string{req.Worktree}) {
		return false
	}
	if floorHome(req) == "" {
		return false
	}
	return pl.within(resolved, installRoots(req))
}

// tempRoots is the OS temp zone, where scratch writes are allowed.
func tempRoots(pl *platform) []string { return pl.tempDirs() }

// outsideWorktree is true for a resolved path that is neither inside the
// task worktree nor in the OS temp zone nor a harmless device.
func outsideWorktree(req Request, resolved string) bool {
	pl := req.pl()
	if !pl.windows {
		switch resolved {
		case "/dev/null", "/dev/stdout", "/dev/stderr":
			return false
		}
	}
	if req.Worktree != "" && pl.within(resolved, []string{req.Worktree}) {
		return false
	}
	return !pl.within(resolved, tempRoots(pl))
}

var daemonPort = regexp.MustCompile(`(^|[:/])(4648|4870)($|[/?])`)

// pathRules runs the install-state, secrets and destructive-outside rules.
func (fs *floorSeg) pathRules() (FloorHit, bool) {
	words := fs.wordPaths()
	if h, ok := fs.installRules(words); ok {
		return h, true
	}
	if h, ok := fs.secretRules(words); ok {
		return h, true
	}
	return fs.destructiveRules(words)
}

func (fs *floorSeg) installRules(words []wordPath) (FloorHit, bool) {
	if len(fs.argv) > 0 {
		switch fs.pl.cmdName(fs.argv[0]) {
		case "pkill", "killall", "kill":
			return floorHit("no-install-state")
		case "smind":
			if w := nonFlags(fs.argv[1:]); len(w) > 0 && w[0] == "service" {
				return floorHit("no-install-state")
			}
		case "launchctl", "systemctl", "service":
			if mentionsSmind(fs.argv[1:]) {
				return floorHit("no-install-state")
			}
		case "brew":
			if containsWord(fs.argv[1:], "services") && mentionsSmind(fs.argv[1:]) {
				return floorHit("no-install-state")
			}
		}
	}
	for _, w := range fs.seg.Argv {
		if daemonPort.MatchString(w) {
			return floorHit("no-install-state")
		}
	}
	for _, w := range words {
		if w.ok && inInstallState(fs.req, w.resolved) {
			return floorHit("no-install-state")
		}
	}
	return FloorHit{}, false
}

// mentionsSmind reports whether any argument names the smind service (a
// launchd label, a unit, a plist path), case-insensitively.
func mentionsSmind(args []string) bool {
	for _, a := range args {
		if strings.Contains(strings.ToLower(a), "smind") {
			return true
		}
	}
	return false
}

var procEnviron = regexp.MustCompile(`^/proc/[^/]+/environ$`)

// secretName matches the file names that hold credentials, compared on the
// last path element so `./.env` and `/home/u/.ssh/id_rsa` both match while
// `env` and `.environment` do not.
func secretName(w string) bool { return hostPlatform().secretName(w) }

func (pl *platform) secretName(w string) bool {
	var b string
	if pl.windows {
		b = strings.ToLower(w[strings.LastIndexAny(w, `/\`)+1:])
	} else {
		if procEnviron.MatchString(path.Clean(w)) {
			return true
		}
		b = path.Base(path.Clean(w))
	}
	switch {
	case b == ".env", b == ".netrc", b == "credentials":
		return true
	case strings.HasPrefix(b, ".env."), strings.HasSuffix(b, ".pem"), strings.HasSuffix(b, ".key"):
		return true
	case strings.HasPrefix(b, "id_rsa"), strings.HasPrefix(b, "id_ed25519"):
		return true
	}
	return false
}

var secretReaders = map[string]bool{
	"cat": true, "less": true, "more": true, "head": true, "tail": true, "grep": true,
	"sed": true, "awk": true, "cp": true, "base64": true, "xxd": true, "strings": true, "bat": true,
}

func (fs *floorSeg) secretRules(words []wordPath) (FloorHit, bool) {
	if len(fs.argv) == 0 {
		if len(fs.wrappers) > 0 && fs.wrappers[len(fs.wrappers)-1] == "env" {
			return floorHit("no-secrets") // bare env prints the environment
		}
		return FloorHit{}, false
	}
	args := fs.argv[1:]
	switch name := fs.pl.cmdName(fs.argv[0]); name {
	case "printenv":
		return floorHit("no-secrets")
	case "set", "export":
		if len(args) == 0 || (name == "export" && containsWord(args, "-p")) {
			return floorHit("no-secrets")
		}
	case "declare", "typeset":
		for _, a := range args {
			if shortCluster(a, "xp") {
				return floorHit("no-secrets")
			}
		}
	case "gh":
		w := nonFlags(args)
		if len(w) >= 2 && w[0] == "auth" {
			if w[1] == "token" {
				return floorHit("no-secrets")
			}
			if w[1] == "status" && (containsWord(args, "-t") || containsWord(args, "--show-token")) {
				return floorHit("no-secrets")
			}
		}
	case "security":
		if w := nonFlags(args); len(w) > 0 && (strings.HasPrefix(w[0], "find-") || w[0] == "dump-keychain") {
			return floorHit("no-secrets")
		}
	default:
		if !secretReaders[name] {
			return FloorHit{}, false
		}
		for _, w := range words {
			if fs.pl.secretName(w.raw) || (w.ok && fs.pl.secretName(w.resolved)) {
				return floorHit("no-secrets")
			}
		}
	}
	return FloorHit{}, false
}

// mutators change or remove what their targets name; reads and the source of
// a copy are not destructive, so cp, install and ln are judged by their
// destination only (see destinations).
var mutators = map[string]bool{
	"cp": true, "mv": true, "rm": true, "tee": true, "touch": true, "mkdir": true,
	"ln": true, "chmod": true, "chown": true, "dd": true, "install": true, "truncate": true,
	"rsync": true, "sed": true, "perl": true, "curl": true, "wget": true, "tar": true, "unzip": true,
}

func (fs *floorSeg) destructiveRules(words []wordPath) (FloorHit, bool) {
	if len(fs.argv) == 0 {
		return fs.redirectRules()
	}
	name := fs.pl.cmdName(fs.argv[0])
	if name == "sudo" || name == "doas" {
		return floorHit("no-destructive-outside")
	}
	if mutators[name] {
		for _, w := range fs.destinations(name, words) {
			if !w.ok || outsideWorktree(fs.req, w.resolved) {
				if !w.ok && fs.pl.dynamicWord(w.raw) && !strings.HasPrefix(w.raw, "$HOME") && !strings.HasPrefix(w.raw, "${HOME}") {
					continue // dynamic: the engine asks
				}
				return floorHit("no-destructive-outside")
			}
		}
	}
	if g, ok := fs.parseGit(); ok && fs.destroysTree(g) && (g.cwd == "" || outsideWorktree(fs.req, g.cwd)) {
		return floorHit("no-destructive-outside")
	}
	return fs.redirectRules()
}

// destinations picks the words a mutator writes to.
func (fs *floorSeg) destinations(name string, words []wordPath) []wordPath {
	var plain []wordPath
	for _, w := range words {
		if !strings.HasPrefix(w.raw, "-") {
			plain = append(plain, w)
		}
	}
	switch name {
	case "cp", "install", "ln":
		for i, a := range fs.argv {
			if (a == "-t" || a == "--target-directory") && i+1 < len(fs.argv) {
				r, ok := resolveFloor(fs.req, fs.argv[i+1], fs.cwd)
				return []wordPath{{raw: fs.argv[i+1], resolved: r, ok: ok}}
			}
		}
		if len(plain) > 0 {
			return plain[len(plain)-1:]
		}
		return nil
	case "rsync":
		if len(plain) > 0 {
			return plain[len(plain)-1:]
		}
		return nil
	case "sed", "perl":
		return fs.inPlaceTargets(plain)
	case "curl":
		return fs.flagTargets([]string{"-o"}, []string{"--output"})
	case "wget":
		return fs.flagTargets([]string{"-O", "-P"}, []string{"--output-document", "--directory-prefix"})
	case "tar":
		return fs.flagTargets([]string{"-C"}, []string{"--directory"})
	case "unzip":
		return fs.flagTargets([]string{"-d"}, nil)
	case "dd":
		var out []wordPath
		for _, a := range fs.argv[1:] {
			if v, found := strings.CutPrefix(a, "of="); found {
				r, ok := resolveFloor(fs.req, v, fs.cwd)
				out = append(out, wordPath{raw: v, resolved: r, ok: ok})
			}
		}
		return out
	}
	return plain
}

// inPlaceTargets are the files of `sed -i` / `perl -i`: every plain word
// except the script. The script is the first plain word unless -e/-f gives
// it as an option, whose value is skipped. Without -i nothing is written.
func (fs *floorSeg) inPlaceTargets(plain []wordPath) []wordPath {
	inPlace, scriptOpt := false, false
	skip := map[string]bool{}
	for i, a := range fs.argv[1:] {
		switch {
		case a == "--in-place" || strings.HasPrefix(a, "--in-place="):
			inPlace = true
		case a == "-e" || a == "-f" || a == "--expression" || a == "--file":
			scriptOpt = true
			if i+2 < len(fs.argv) {
				skip[fs.argv[i+2]] = true
			}
		case shortCluster(a, "i"):
			inPlace = true
			if shortCluster(a, "e") || shortCluster(a, "f") {
				scriptOpt = true
			}
		}
	}
	if !inPlace {
		return nil
	}
	var out []wordPath
	first := !scriptOpt
	for _, w := range plain {
		if skip[w.raw] {
			continue
		}
		if first {
			first = false
			continue
		}
		out = append(out, w)
	}
	return out
}

// flagTargets collects the values of output-directing flags: `-o f`,
// `-of`, `--output f` and `--output=f` all name the same file.
func (fs *floorSeg) flagTargets(shorts, longs []string) []wordPath {
	var out []wordPath
	add := func(v string) {
		r, ok := resolveFloor(fs.req, v, fs.cwd)
		out = append(out, wordPath{raw: v, resolved: r, ok: ok})
	}
	args := fs.argv[1:]
	for i, a := range args {
		for _, f := range shorts {
			if a == f && i+1 < len(args) {
				add(args[i+1])
			} else if len(a) > len(f) && strings.HasPrefix(a, f) && !strings.HasPrefix(a, "--") {
				add(a[len(f):])
			}
		}
		for _, f := range longs {
			if a == f && i+1 < len(args) {
				add(args[i+1])
			} else if v, found := strings.CutPrefix(a, f+"="); found {
				add(v)
			}
		}
	}
	return out
}

// destroysTree is a git command that discards the working tree.
func (fs *floorSeg) destroysTree(g gitCall) bool {
	switch g.sub {
	case "clean":
		return true
	case "reset":
		return containsWord(g.args, "--hard")
	case "checkout", "restore":
		return containsWord(nonFlags(g.args), ".")
	}
	return false
}

func (fs *floorSeg) redirectRules() (FloorHit, bool) {
	for _, rd := range fs.seg.Redirects {
		if !strings.Contains(rd.Op, ">") {
			continue
		}
		switch rd.Target {
		case "", "&1", "&2", "-":
			continue
		}
		if fs.pl.isNullDevice(rd.Target) {
			continue
		}
		r, ok := resolveFloor(fs.req, rd.Target, fs.cwd)
		if !ok {
			if fs.pl.dynamicWord(rd.Target) && !strings.HasPrefix(rd.Target, "$HOME") && !strings.HasPrefix(rd.Target, "${HOME}") {
				continue
			}
			return floorHit("no-destructive-outside")
		}
		if outsideWorktree(fs.req, r) {
			return floorHit("no-destructive-outside")
		}
	}
	return FloorHit{}, false
}

// CheckFloorPaths applies the path rules to a non-Bash tool request (Read,
// Edit, Write, MultiEdit, NotebookEdit, ...): secrets and install state for
// every tool, and the outside-worktree rule for the tools that write.
func CheckFloorPaths(req Request) (FloorHit, bool) {
	if req.Tool == "Bash" {
		return FloorHit{}, false
	}
	cwd := req.Cwd
	if cwd == "" {
		cwd = req.Worktree
	}
	pl := req.pl()
	writes := false
	switch req.Tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		writes = true
	}
	for _, p := range req.Paths {
		r, ok := resolveFloor(req, p, cwd)
		if pl.secretName(p) || (ok && pl.secretName(r)) {
			return floorHit("no-secrets")
		}
		if !ok {
			if writes && pl.windows && !pl.dynamicWord(p) && !pl.isNullDevice(p) {
				return floorHit("no-destructive-outside") // an unplaceable write destination is never judged against the cwd
			}
			continue // unresolvable: the engine asks
		}
		if inInstallState(req, r) {
			return floorHit("no-install-state")
		}
		if writes && outsideWorktree(req, r) {
			return floorHit("no-destructive-outside")
		}
	}
	return FloorHit{}, false
}
