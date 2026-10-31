package permpolicy

import (
	"regexp"
	"strings"
)

// envRules runs the rule that keeps tests off the real home.
func (fs *floorSeg) envRules() (FloorHit, bool) {
	return fs.realHomeRules()
}

func isFetcher(fs *floorSeg) bool {
	if len(fs.argv) == 0 {
		return false
	}
	switch fs.pl.cmdName(fs.argv[0]) {
	case "curl", "wget", "fetch":
		return true
	case "iwr", "irm", "invoke-webrequest", "invoke-restmethod", "bitsadmin":
		return fs.pl.windows
	}
	return false
}

func isInterpreter(fs *floorSeg) bool {
	if len(fs.argv) == 0 {
		return false
	}
	switch name := fs.pl.cmdName(fs.argv[0]); name {
	case "sh", "bash", "zsh", "dash", "ksh", "node", "perl", "ruby", "source", ".":
		return true
	case "iex", "invoke-expression", "powershell", "pwsh", "cmd", "cscript", "wscript", "mshta":
		return fs.pl.windows
	default:
		return strings.HasPrefix(name, "python")
	}
}

// curlShell is the download-and-run pattern: a fetcher piped into an
// interpreter, or a fetcher anywhere in a command that also runs
// `interpreter -c <dynamic text>` (bash -c "$(curl ...)"). Fetching to a
// file and running it in a later step is not caught here; the file is
// inspectable between the two.
func curlShell(segs []*floorSeg) bool {
	if len(segs) > 0 && segs[0].pl.windows && winFetchAndEval(segs) {
		return true
	}
	fetches := false
	for i, fs := range segs {
		if !isFetcher(fs) {
			continue
		}
		fetches = true
		if fs.seg.PipeNext && i+1 < len(segs) && isInterpreter(segs[i+1]) {
			return true
		}
	}
	if !fetches {
		return false
	}
	for _, fs := range segs {
		if !isInterpreter(fs) || !fs.seg.Dynamic {
			continue
		}
		for _, a := range fs.argv[1:] {
			if a == "-c" || shortCluster(a, "c") {
				return true
			}
		}
	}
	return false
}

var (
	winFetchWord = regexp.MustCompile(`\b(iwr|irm|invoke-webrequest|invoke-restmethod|curl|wget|downloadstring)\b`)
	winEvalWord  = regexp.MustCompile(`\b(iex|invoke-expression)\b`)
)

// winFetchAndEval reports a fetch word and an iex word anywhere in the same
// command line, including inside a quoted -Command string: download-and-run.
func winFetchAndEval(segs []*floorSeg) bool {
	fetch, eval := false, false
	for _, fs := range segs {
		for _, w := range fs.argv {
			w = strings.ToLower(w)
			fetch = fetch || winFetchWord.MatchString(w)
			eval = eval || winEvalWord.MatchString(w)
		}
	}
	return fetch && eval
}

var homeVars = map[string]bool{"HOME": true, "SMIND_HOME": true}

// isHomeVar reports whether name is a variable that selects the home;
// Windows variable names are case-insensitive and USERPROFILE is its home.
func (fs *floorSeg) isHomeVar(name string) bool {
	if fs.pl.windows {
		return homeVars[strings.ToUpper(name)] || strings.EqualFold(name, "USERPROFILE")
	}
	return homeVars[name]
}

// realHomeRules denies pointing HOME or SMIND_HOME at the real home (or
// leaving it empty or unset, which falls back to it) for a command, through
// a prefix assignment, env, export or unset.
func (fs *floorSeg) realHomeRules() (FloorHit, bool) {
	assigns := fs.assigns
	if len(fs.argv) > 0 {
		name := fs.pl.cmdName(fs.argv[0])
		if fs.pl.windows && len(fs.argv[0]) > len("$env:") && strings.EqualFold(fs.argv[0][:len("$env:")], "$env:") && strings.Contains(fs.argv[0], "=") {
			assigns = append(assigns, fs.argv[0][len("$env:"):])
		}
		switch {
		case name == "export" || fs.pl.windows && (name == "set" || name == "setx"):
			for _, a := range fs.argv[1:] {
				if isEnvAssign(a) {
					assigns = append(assigns, a)
				}
			}
		case name == "unset":
			for _, a := range fs.argv[1:] {
				if fs.isHomeVar(a) {
					return floorHit("no-real-home-tests")
				}
			}
		}
	}
	for _, u := range fs.unsets {
		if fs.isHomeVar(u) {
			return floorHit("no-real-home-tests")
		}
	}
	for _, a := range assigns {
		name, val, _ := strings.Cut(a, "=")
		if fs.isHomeVar(name) && fs.realHomeValue(val) {
			return floorHit("no-real-home-tests")
		}
	}
	return FloorHit{}, false
}

// realHomeValue reports whether a HOME/SMIND_HOME value is, or may fall back
// to, the real home: empty, a ~ or $HOME form, or a path that resolves to the
// home or into its .smind. A dynamic value ($(mktemp -d)) is not guessed at.
func (fs *floorSeg) realHomeValue(val string) bool {
	if val == "" {
		return true
	}
	for _, p := range fs.pl.homeForms() {
		if val == p || (strings.HasPrefix(val, p) && (val[len(p)] == '/' || (fs.pl.windows && val[len(p)] == '\\'))) {
			return true
		}
	}
	resolved, ok := resolveFloor(fs.req, val, fs.cwd)
	if !ok {
		return false
	}
	home := floorHome(fs.req)
	if home == "" {
		return false
	}
	if h, ok := resolveFloor(fs.req, home, ""); ok && fs.pl.eq(resolved, h) {
		return true
	}
	roots := installRoots(fs.req)[:1] // Home/.smind
	if fs.req.SmindHome != "" {
		roots = append(roots, fs.req.SmindHome)
	}
	return fs.pl.within(resolved, roots)
}
