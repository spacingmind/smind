package permpolicy

import (
	"fmt"
	"strings"
)

// maxDecisionPaths caps Decision.Paths: the audit keeps a handful of
// resolved paths, not every word of a long command.
const maxDecisionPaths = 8

// unit is one thing a rule is matched against: a Bash segment, or the whole
// request of a non-Bash tool (seg == nil). cwd is the resolved directory the
// segment runs in, "" when it cannot be resolved.
type unit struct {
	seg   *Segment
	paths []string
	cwd   string
}

// Decide is the engine's pure decision for one request (ADR-0047 §2
// "Evaluation"). The floor runs first and cannot be overridden; anything the
// engine cannot fully judge is ask; precedence is deny > ask > allow, never
// file order; an allow needs a rule for every segment. pol == nil is the
// floor plus ask, which is what a missing or invalid policy file means.
func Decide(req Request, pol *Policy) Decision {
	d := Decision{Action: ActionAsk}
	if pol != nil {
		d.PolicySha = pol.Sha
	}
	if req.Truncated {
		d.Reasons = []string{"command truncated by provider"}
		return d
	}
	bash := req.Tool == "Bash" || (req.Tool == "" && req.Command != "")
	cwd := req.Cwd
	if cwd == "" {
		cwd = req.Worktree
	}

	var units []unit
	var segs []Segment
	if bash {
		p := splitCommand(req.Command, req.pl().windows)
		d.Normalized = p.Normalized
		segs = p.Segments
		if h, ok := CheckFloor(req, p); ok {
			return floorDecision(d, req, p, h)
		}
		if len(p.Unparsed) > 0 {
			d.Reasons = p.Unparsed
			return d
		}
		for _, s := range p.Segments {
			if fs := newFloorSeg(req, s, cwd); len(fs.argv) > 0 && isShellEval(fs.argv[0]) {
				d.Reasons = []string{fs.argv[0] + " runs text the engine cannot see"}
				return d
			}
		}
		var reason string
		if units, reason = bashUnits(req, p.Segments, cwd, &d); reason != "" {
			d.Reasons = []string{reason}
			return d
		}
	} else {
		if h, ok := CheckFloorPaths(req); ok {
			d.Action, d.FloorID, d.Why = ActionDeny, h.ID, h.Reason
			d.Reasons = []string{"floor " + h.ID + ": " + h.Reason}
			return d
		}
		u := unit{}
		for _, p := range req.Paths {
			r, err := req.pl().resolve(p, cwd)
			if err != nil {
				d.Reasons = []string{"cannot resolve path " + p}
				return d
			}
			u.paths = append(u.paths, r)
		}
		d.Paths = capPaths(u.paths)
		units = []unit{u}
	}
	if pol == nil {
		d.Segments = newSegmentDecisions(segs)
		d.Reasons = []string{"no policy loaded"}
		return d
	}
	return evaluateRules(d, req, pol, segs, units)
}

// floorDecision is a floor deny. Which segment tripped it is best effort
// (a rule that spans segments, like curl | sh, has no single culprit, so
// then every segment is marked).
func floorDecision(d Decision, req Request, p Parsed, h FloorHit) Decision {
	d.Action, d.FloorID, d.Why = ActionDeny, h.ID, h.Reason
	d.Reasons = []string{"floor " + h.ID + ": " + h.Reason}
	marked := make([]bool, len(p.Segments))
	found := false
	for i, s := range p.Segments {
		if hit, ok := CheckFloor(req, Parsed{Segments: []Segment{s}}); ok && hit.ID == h.ID {
			marked[i], found = true, true
		}
	}
	d.Segments = make([]SegmentDecision, len(p.Segments))
	for i, s := range p.Segments {
		sd := SegmentDecision{Index: i, Text: s.Raw, Action: ActionAsk}
		if marked[i] || !found {
			sd.Action, sd.FloorID = ActionDeny, h.ID
		}
		d.Segments[i] = sd
	}
	return d
}

// bashUnits resolves the paths of every segment, following `cd` so a later
// segment is judged from where it actually runs. A path that cannot be
// resolved makes the request ask (the reason is returned).
func bashUnits(req Request, segs []Segment, cwd string, d *Decision) ([]unit, string) {
	units := make([]unit, len(segs))
	var all []string
	for i := range segs {
		fs := newFloorSeg(req, segs[i], cwd)
		paths, err := segmentPaths(req.pl(), segs[i], cwd)
		if err != nil {
			return nil, "cannot resolve path: " + err.Error()
		}
		rc := ""
		if cwd != "" {
			if r, err := req.pl().resolve(cwd, ""); err == nil {
				rc = r
			}
		}
		units[i] = unit{seg: &segs[i], paths: paths, cwd: rc}
		all = append(all, paths...)
		cwd = fs.cwdAfter()
	}
	d.Paths = capPaths(all)
	return units, ""
}

func capPaths(paths []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] || len(out) == maxDecisionPaths {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func newSegmentDecisions(segs []Segment) []SegmentDecision {
	out := make([]SegmentDecision, len(segs))
	for i, s := range segs {
		out[i] = SegmentDecision{Index: i, Text: s.Raw, Action: ActionAsk}
	}
	return out
}

// evaluateRules applies the policy after the floor and the parse checks.
// A deny rule matching any segment denies, then an ask rule matching any
// segment asks, whatever their order in the file; only then can an allow
// answer, and only when every segment has an allow rule of its own.
func evaluateRules(d Decision, req Request, pol *Policy, segs []Segment, units []unit) Decision {
	sds := newSegmentDecisions(segs)
	for _, act := range []Action{ActionDeny, ActionAsk} {
		for _, r := range pol.Rules {
			if r.Action != act {
				continue
			}
			hit := false
			for i, u := range units {
				if matchRule(r, req, u.seg, u.paths, u.cwd) {
					hit = true
					if i < len(sds) {
						sds[i].Action, sds[i].RuleID = act, r.ID
					}
				}
			}
			if hit {
				d.Action, d.RuleID, d.Why = act, r.ID, r.Why
				d.Reasons = []string{fmt.Sprintf("%s rule %s", act, r.ID)}
				d.Segments = sds
				return d
			}
		}
	}
	d.Segments = sds
	covered := make([]*Rule, len(units))
	n := 0
	for i, u := range units {
		for j := range pol.Rules {
			r := &pol.Rules[j]
			if r.Action == ActionAllow && matchRule(*r, req, u.seg, u.paths, u.cwd) {
				covered[i] = r
				n++
				break
			}
		}
	}
	if n == 0 {
		d.Reasons = []string{"no rule matched"}
		return d
	}
	for i, u := range units {
		if covered[i] == nil {
			text := "the request"
			if u.seg != nil {
				text = fmt.Sprintf("segment %d (%s)", i, u.seg.Raw)
			}
			d.Reasons = []string{text + " is not covered by an allow rule"}
			return d
		}
	}
	for i, u := range units {
		switch {
		case u.seg != nil && u.seg.Dynamic:
			d.Reasons = []string{fmt.Sprintf("segment %d (%s) contains a dynamic word", i, u.seg.Raw)}
			return d
		case u.seg != nil && len(u.seg.Env) > 0:
			d.Reasons = []string{fmt.Sprintf("segment %d (%s) has an environment assignment", i, u.seg.Raw)}
			return d
		case u.seg != nil && len(u.seg.Redirects) > 0:
			d.Reasons = []string{fmt.Sprintf("segment %d (%s) has a redirect", i, u.seg.Raw)}
			return d
		}
	}
	if req.Worktree == "" {
		d.Reasons = []string{"no task worktree known for this request"}
		return d
	}
	var ids []string
	seen := map[string]bool{}
	for i, r := range covered {
		if i < len(sds) {
			sds[i].Action, sds[i].RuleID = ActionAllow, r.ID
		}
		if !seen[r.ID] {
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	d.Action, d.RuleID, d.Why = ActionAllow, strings.Join(ids, ","), covered[0].Why
	d.Reasons = []string{"allowed by rule " + d.RuleID}
	return d
}

// matchRule reports whether one rule fits one unit. A field a rule leaves
// empty matches anything; everything it names must hold.
func matchRule(r Rule, req Request, seg *Segment, segPaths []string, cwd string) bool {
	m := r.Match
	if len(m.Seats) > 0 && !containsWord(m.Seats, req.Seat) {
		return false
	}
	if len(m.Workspaces) > 0 && !containsWord(m.Workspaces, req.Workspace) {
		return false
	}
	if m.Tool != "" && m.Tool != req.Tool {
		return false
	}
	if m.Command != nil && (seg == nil || !matchCommand(m.Command, req, seg)) {
		return false
	}
	if m.Paths != nil && len(m.Paths.Within) > 0 {
		return withinScope(m.Paths.Within, req, seg, segPaths, cwd)
	}
	return true
}

// matchCommand matches a segment by argv0 exactly as written (a wrapped
// command does not match: `nohup git push` is not `git push`), then the
// subcommand words that must follow immediately (a flag before them, as in
// `git -C x push`, means no match), then the args.
func matchCommand(c *CommandMatch, req Request, seg *Segment) bool {
	argv := seg.Argv
	if len(argv) == 0 || argv[0] != c.Argv0 {
		return false
	}
	idx := 1
	for _, want := range strings.Fields(c.Subcommand) {
		if idx >= len(argv) || strings.HasPrefix(argv[idx], "-") || argv[idx] != want {
			return false
		}
		idx++
	}
	rest := argv[idx:]
	for _, w := range argv[1:] {
		for _, f := range c.Args.DenyFlags {
			if w == f || strings.HasPrefix(w, f+"=") {
				return false
			}
		}
	}
	if len(c.Args.AllowOnly) == 0 {
		return true
	}
	// A rule that names args does not cover a bare `git push`: the
	// destination would be whatever the repo's defaults say.
	if len(rest) == 0 {
		return false
	}
	allowed := map[string]bool{}
	for _, a := range c.Args.AllowOnly {
		exp, ok := expandPlaceholders(a, req)
		if !ok {
			continue
		}
		allowed[exp] = true
		if strings.Contains(a, "{taskBranch}") && !containsWord(rest, exp) {
			return false
		}
	}
	for _, w := range rest {
		if !allowed[w] {
			return false
		}
	}
	return true
}

// expandPlaceholders fills {taskBranch} and {worktree} from the request;
// an empty value reports false so an unknown branch matches nothing.
func expandPlaceholders(s string, req Request) (string, bool) {
	if strings.Contains(s, "{taskBranch}") {
		if req.TaskBranch == "" {
			return "", false
		}
		s = strings.ReplaceAll(s, "{taskBranch}", req.TaskBranch)
	}
	if strings.Contains(s, "{worktree}") {
		if req.Worktree == "" {
			return "", false
		}
		s = strings.ReplaceAll(s, "{worktree}", req.Worktree)
	}
	return s, true
}

// withinScope requires every resolved path (and, for a Bash segment, the
// directory it runs in) to be inside the resolved roots. A {worktree} root
// without a known worktree matches nothing, and a file tool that names no
// path cannot be shown to stay inside.
func withinScope(within []string, req Request, seg *Segment, segPaths []string, cwd string) bool {
	pl := req.pl()
	roots := make([]string, 0, len(within))
	for _, w := range within {
		if w == "{worktree}" {
			if req.Worktree == "" {
				return false
			}
			w = req.Worktree
		}
		if pl.windows {
			n, err := pl.normalize(w)
			if err != nil {
				continue
			}
			w = n
		}
		roots = append(roots, pl.clean(w))
	}
	if seg == nil && len(segPaths) == 0 {
		return false
	}
	if seg != nil && (cwd == "" || !pl.within(cwd, roots)) {
		return false
	}
	for _, p := range segPaths {
		if !pl.within(p, roots) {
			return false
		}
	}
	return true
}
