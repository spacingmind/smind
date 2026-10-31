package steward

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	detailMax   = 300
	shortSHALen = 7
)

var conventionalTitle = regexp.MustCompile(`^(feat|fix|docs|chore|refactor|test|perf|build|ci|style|revert)(\([^)]+\))?!?: .+`)

// ComputeGates returns exactly one Gate per id in GateIDs, in that order
// (ADR-0047 §3.3). It is pure: everything it needs is in Inputs.
func ComputeGates(in Inputs) []Gate {
	paths := changedPaths(in)
	computed := map[string]Gate{
		"ci":                  gateCI(in),
		"validator":           gateValidator(in),
		"frames_fresh":        gateFrames(in, paths),
		"no_binaries":         gateNoBinaries(in),
		"no_churn":            gateNoChurn(paths),
		"no_secrets":          gateNoSecrets(in),
		"not_stacked":         gateNotStacked(in),
		"pr_hygiene":          gatePRHygiene(in),
		"scope":               gateScope(in, paths),
		"mutations_committed": gateMutationsCommitted(in),
		"peer_claims":         gatePeerClaims(in),
	}
	out := make([]Gate, 0, len(GateIDs))
	for _, id := range GateIDs {
		g := computed[id]
		g.ID = id
		g.Detail = capDetail(g.Detail)
		out = append(out, g)
	}
	return out
}

// GateByID finds a gate by id in the GateIDs order.
func GateByID(gs []Gate, id string) (Gate, bool) {
	for _, g := range gs {
		if g.ID == id {
			return g, true
		}
	}
	return Gate{}, false
}

type numstatRow struct {
	Added   int
	Deleted int
	Binary  bool
	Path    string
}

// parseNumstat parses raw `git diff --numstat` text: "added\tdeleted\tpath",
// "-" for a binary row, "a => b" / "p/{a => b}/s" renames taken at their new
// path. Blank lines are ignored.
func parseNumstat(s string) []numstatRow {
	var rows []numstatRow
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		row := numstatRow{Path: renamePath(parts[2])}
		if parts[0] == "-" || parts[1] == "-" {
			row.Binary = true
		} else {
			row.Added, _ = strconv.Atoi(parts[0])
			row.Deleted, _ = strconv.Atoi(parts[1])
		}
		rows = append(rows, row)
	}
	return rows
}

// renamePath takes the new path of a numstat rename: "old => new" whole-path
// or "prefix/{old => new}/suffix" partial.
func renamePath(p string) string {
	if i := strings.Index(p, "{"); i >= 0 {
		if j := strings.Index(p, "}"); j > i {
			inner := p[i+1 : j]
			if k := strings.Index(inner, " => "); k >= 0 {
				return p[:i] + inner[k+4:] + p[j+1:]
			}
		}
	}
	if i := strings.Index(p, " => "); i >= 0 {
		return p[i+4:]
	}
	return p
}

// changedPaths lists the diff's paths, de-duplicated, in numstat order.
func changedPaths(in Inputs) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, row := range parseNumstat(in.Numstat) {
		if row.Path == "" || seen[row.Path] {
			continue
		}
		seen[row.Path] = true
		paths = append(paths, row.Path)
	}
	return paths
}

func gateCI(in Inputs) Gate {
	switch in.CI.State {
	case "success":
		return Gate{State: GatePass}
	case "failure":
		return Gate{State: GateFail, Detail: strings.Join(in.CI.Failing, ", ")}
	case "pending":
		return Gate{State: GateUnknown}
	case "none", "":
		return Gate{State: GateUnknown, Detail: "no CI result yet"}
	default:
		return Gate{State: GateUnknown}
	}
}

func gateValidator(in Inputs) Gate {
	if !in.HasContract {
		return Gate{State: GateNA}
	}
	v := in.Validation
	if v == nil {
		return Gate{State: GateFail, Detail: "no validation"}
	}
	if v.CommitSHA != in.Task.HeadSHA {
		return Gate{State: GateFail, Detail: fmt.Sprintf("stale (judged %s, head %s)", shortSHA(v.CommitSHA), shortSHA(in.Task.HeadSHA))}
	}
	if v.Status != "pass" {
		return Gate{State: GateFail, Detail: "status " + v.Status}
	}
	if v.ScopeViolation {
		return Gate{State: GateFail, Detail: "scope violation"}
	}
	return Gate{State: GatePass}
}

var frameMatrix = []struct {
	Viewport int
	Theme    string
}{
	{1280, "light"},
	{1280, "dark"},
	{390, "light"},
	{390, "dark"},
}

func gateFrames(in Inputs, paths []string) Gate {
	required := false
	for _, p := range paths {
		if strings.HasPrefix(p, "web/") || strings.HasPrefix(p, "mobile/") {
			required = true
			break
		}
	}
	if !required {
		return Gate{State: GateNA}
	}
	if len(in.Frames) == 0 {
		return Gate{State: GateFail, Detail: "no frames evidence"}
	}
	for _, f := range in.Frames {
		if f.CapturedSHA != in.Task.HeadSHA {
			return Gate{State: GateFail, Detail: fmt.Sprintf("frame %s captured at %s", f.Path, shortSHA(f.CapturedSHA))}
		}
	}
	seen := make(map[string]bool, len(in.Frames))
	for _, f := range in.Frames {
		seen[fmt.Sprintf("%d/%s", f.Viewport, f.Theme)] = true
	}
	for _, cell := range frameMatrix {
		key := fmt.Sprintf("%d/%s", cell.Viewport, cell.Theme)
		if !seen[key] {
			return Gate{State: GateFail, Detail: "missing " + key}
		}
	}
	return Gate{State: GatePass}
}

func gateNoBinaries(in Inputs) Gate {
	var bins []string
	for _, row := range parseNumstat(in.Numstat) {
		if row.Binary {
			bins = append(bins, row.Path)
		}
	}
	if len(bins) == 0 {
		return Gate{State: GatePass}
	}
	return Gate{State: GateFail, Detail: strings.Join(bins, ", ")}
}

var lockfiles = map[string]bool{
	"package-lock.json": true,
	"bun.lock":          true,
	"bun.lockb":         true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
}

var buildRoots = []string{"web/", "mobile/", "internal/server/"}
var buildSegments = map[string]bool{"dist": true, "build": true, "out": true}

func gateNoChurn(paths []string) Gate {
	changed := make(map[string]bool, len(paths))
	for _, p := range paths {
		changed[p] = true
	}
	var churned []string
	for _, p := range paths {
		if isChurn(p, changed) {
			churned = append(churned, p)
		}
	}
	if len(churned) == 0 {
		return Gate{State: GatePass}
	}
	return Gate{State: GateFail, Detail: strings.Join(firstN(churned, 5), ", ")}
}

func isChurn(path string, changed map[string]bool) bool {
	if path == "go.sum" {
		return false
	}
	if strings.Contains(path, "gen/schemas/") ||
		strings.Contains(path, "/node_modules/") ||
		strings.HasPrefix(path, "node_modules/") ||
		baseName(path) == ".DS_Store" ||
		hasBuildSegment(path) {
		return true
	}
	if !lockfiles[baseName(path)] {
		return false
	}
	manifest := "package.json"
	if i := strings.LastIndex(path, "/"); i >= 0 {
		manifest = path[:i] + "/package.json"
	}
	return !changed[manifest]
}

func hasBuildSegment(path string) bool {
	for _, root := range buildRoots {
		if !strings.HasPrefix(path, root) {
			continue
		}
		for _, seg := range strings.Split(strings.TrimPrefix(path, root), "/") {
			if buildSegments[seg] {
				return true
			}
		}
	}
	return false
}

func baseName(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func gateNoSecrets(in Inputs) Gate {
	total := 0
	labels := make([]string, 0, len(in.SecretHits))
	for label := range in.SecretHits {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		n := in.SecretHits[label]
		total += n
		parts = append(parts, fmt.Sprintf("%s=%d", label, n))
	}
	if total == 0 {
		return Gate{State: GatePass}
	}
	return Gate{State: GateFail, Detail: fmt.Sprintf("%d hit(s): %s", total, strings.Join(parts, ", "))}
}

func gateNotStacked(in Inputs) Gate {
	if in.PR != nil && in.PR.Base != "develop" {
		return Gate{State: GateFail, Detail: "PR base is " + in.PR.Base}
	}
	if len(in.Stacking.ForeignCommits) > 0 {
		return Gate{State: GateFail, Detail: fmt.Sprintf("carries %d commit(s) of another PR", len(in.Stacking.ForeignCommits))}
	}
	if !in.Stacking.BaseReachableFromDevelop {
		return Gate{State: GateFail, Detail: "base not reachable from origin/develop"}
	}
	return Gate{State: GatePass}
}

func gatePRHygiene(in Inputs) Gate {
	if in.PR == nil {
		return Gate{State: GateFail, Detail: "no PR"}
	}
	if !conventionalTitle.MatchString(in.PR.Title) {
		return Gate{State: GateFail, Detail: "title is not Conventional Commits"}
	}
	if !bodyHasValidated(in.PR.Body) || !bodyHasNotDone(in.PR.Body) {
		return Gate{State: GateFail, Detail: "body lacks validated / not done sections"}
	}
	return Gate{State: GatePass}
}

// bodyHasValidated reports whether the PR body claims a validation section.
func bodyHasValidated(body string) bool {
	return strings.Contains(strings.ToLower(body), "validat")
}

// bodyHasNotDone reports whether the PR body admits a not-done or
// not-verified section.
func bodyHasNotDone(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, "not done") || strings.Contains(b, "not verified")
}

func gateScope(in Inputs, paths []string) Gate {
	if !in.HasContract {
		return Gate{State: GateNA}
	}
	outside := outsideScope(in, paths)
	if len(outside) == 0 {
		return Gate{State: GatePass}
	}
	return Gate{State: GateFail, Detail: strings.Join(firstN(outside, 5), ", ")}
}

// outsideScope lists the changed paths the write scope (plus the plan file)
// does not cover; anything under docs/plans/ is always allowed.
func outsideScope(in Inputs, paths []string) []string {
	allowed := append([]string{}, in.WriteScope...)
	if in.PlanFile != "" {
		allowed = append(allowed, in.PlanFile)
	}
	var outside []string
	for _, p := range paths {
		if !strings.HasPrefix(p, "docs/plans/") && !scopeMatched(p, allowed) {
			outside = append(outside, p)
		}
	}
	return outside
}

// scopeMatched says whether any writeScope entry covers path: exact, a
// directory entry ending "/" or "/**", or a "/*" entry covering direct
// children only.
func scopeMatched(path string, allowed []string) bool {
	for _, entry := range allowed {
		if entry == path {
			return true
		}
		switch {
		case strings.HasSuffix(entry, "/**"):
			if dir := strings.TrimSuffix(entry, "/**"); dir != "" && strings.HasPrefix(path, dir+"/") {
				return true
			}
		case strings.HasSuffix(entry, "/"):
			if dir := strings.TrimSuffix(entry, "/"); dir != "" && strings.HasPrefix(path, dir+"/") {
				return true
			}
		case strings.HasSuffix(entry, "/*"):
			dir := strings.TrimSuffix(entry, "/*")
			if dir != "" && strings.HasPrefix(path, dir+"/") && !strings.Contains(strings.TrimPrefix(path, dir+"/"), "/") {
				return true
			}
		}
	}
	return false
}

func gateMutationsCommitted(in Inputs) Gate {
	if !in.HasContract || in.Validation == nil {
		return Gate{State: GateNA}
	}
	if in.Validation.CommitSHA != in.Task.HeadSHA {
		return Gate{State: GateFail, Detail: "mutations judged at " + shortSHA(in.Validation.CommitSHA) + " not head"}
	}
	for _, m := range in.Validation.Mutations {
		if !m.Restored || m.Status != "pass" {
			return Gate{State: GateFail, Detail: "mutation " + m.ID + " not restored/failed"}
		}
	}
	return Gate{State: GatePass}
}

func gatePeerClaims(in Inputs) Gate {
	if len(in.Handoffs) == 0 {
		return Gate{State: GateNA}
	}
	for _, h := range in.Handoffs {
		if h.Status == "blocked" || h.Status == "failed" {
			return Gate{State: GateFail, Detail: "handoff status " + h.Status}
		}
	}
	body := ""
	if in.PR != nil {
		body = in.PR.Body
	}
	normBody := normalise(body)
	for _, h := range in.Handoffs {
		for _, nv := range h.NotVerified {
			if !strings.Contains(normBody, normalise(nv)) {
				return Gate{State: GateFail, Detail: "not_verified item not repeated in the PR body: " + firstChars(nv, 60)}
			}
		}
	}
	return Gate{State: GatePass}
}

// normalise lowercases and collapses whitespace for substring claims.
func normalise(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func firstChars(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func firstN(paths []string, n int) []string {
	if len(paths) <= n {
		return paths
	}
	return paths[:n]
}

func shortSHA(sha string) string {
	if len(sha) <= shortSHALen {
		return sha
	}
	return sha[:shortSHALen]
}

// capDetail truncates s to at most detailMax bytes without splitting a rune.
func capDetail(s string) string {
	if len(s) <= detailMax {
		return s
	}
	trunc := s[:detailMax]
	for len(trunc) > 0 && !utf8.ValidString(trunc) {
		trunc = trunc[:len(trunc)-1]
	}
	return trunc
}
