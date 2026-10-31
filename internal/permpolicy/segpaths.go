package permpolicy

import "strings"

// SegmentPaths returns every path one simple command names, resolved the way
// the engine judges them (ResolvePath: absolute, links followed), so a rule
// can ask "did everything this command touches stay inside paths.within?".
//
// Dynamic words ($VAR, `cmd`, globs) are skipped rather than guessed at —
// the segment itself stays Dynamic, which no allow ever covers — and a word
// that fails to resolve aborts the whole call with that error, so the caller
// escalates instead of judging a subset of the paths.
func SegmentPaths(seg Segment, cwd string) ([]string, error) {
	return segmentPaths(hostPlatform(), seg, cwd)
}

func segmentPaths(pl *platform, seg Segment, cwd string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(resolved string) {
		if !seen[resolved] {
			seen[resolved] = true
			out = append(out, resolved)
		}
	}
	for i := 1; i < len(seg.Argv); i++ {
		w := seg.Argv[i]
		// A flag word contributes nothing of its own; --flag=value names the
		// value only.
		if strings.HasPrefix(w, "-") {
			eq := strings.IndexByte(w, '=')
			if eq < 0 {
				continue
			}
			w = w[eq+1:]
		}
		if w == "" || pl.dynamicWord(w) {
			continue
		}
		resolved, err := pl.resolve(w, cwd)
		if err != nil {
			return nil, err
		}
		add(resolved)
	}
	for _, rd := range seg.Redirects {
		// fd duplications and closes live in the target slot only in forms
		// the splitter records separately; /dev/null writes nowhere real.
		switch rd.Target {
		case "", "&1", "&2", "-", "/dev/null":
			continue
		}
		if pl.isNullDevice(rd.Target) || pl.dynamicWord(rd.Target) {
			continue
		}
		resolved, err := pl.resolve(rd.Target, cwd)
		if err != nil {
			return nil, err
		}
		add(resolved)
	}
	return out, nil
}

// dynamicWord matches the characters that made the splitter call a word
// dynamic: $ and ` expand, and * ? [ glob, so the literal the engine sees is
// not the path the shell will act on.
func dynamicWord(w string) bool {
	return strings.ContainsAny(w, "$`*?[")
}

// dynamicWord is the flavour's view: on Windows a %VAR% expansion is dynamic too.
func (pl *platform) dynamicWord(w string) bool {
	return dynamicWord(w) || (pl.windows && hasPercentVar(w))
}

// hasPercentVar reports a '%' followed later by another '%' (%TEMP%).
func hasPercentVar(w string) bool {
	i := strings.IndexByte(w, '%')
	return i >= 0 && strings.IndexByte(w[i+1:], '%') >= 0
}
