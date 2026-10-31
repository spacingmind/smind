package permpolicy

import "strings"

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

var homeVars = map[string]bool{"HOME": true, "SMIND_HOME": true}

// realHomeRules denies pointing HOME or SMIND_HOME at the real home (or
// leaving it empty or unset, which falls back to it) for a command, through
// a prefix assignment, env, export or unset.
func (fs *floorSeg) realHomeRules() (FloorHit, bool) {
	assigns := fs.assigns
	if len(fs.argv) > 0 {
		switch fs.pl.cmdName(fs.argv[0]) {
		case "export":
			for _, a := range fs.argv[1:] {
				if isEnvAssign(a) {
					assigns = append(assigns, a)
				}
			}
		case "unset":
			for _, a := range fs.argv[1:] {
				if homeVars[a] {
					return floorHit("no-real-home-tests")
				}
			}
		}
	}
	for _, u := range fs.unsets {
		if homeVars[u] {
			return floorHit("no-real-home-tests")
		}
	}
	for _, a := range assigns {
		name, val, _ := strings.Cut(a, "=")
		if homeVars[name] && fs.realHomeValue(val) {
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
