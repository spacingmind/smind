package taskrunner

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/realpath"
)

// Caps on what a PermissionDetail carries, so a permission request stays a
// small, bounded message no matter how large the agent's write is.
const (
	detailPreviewMaxBytes = 4096
	detailPreviewMaxLines = 60
	detailDiffMaxBytes    = 8192
	detailDiffMaxLines    = 200
	detailCommandMaxBytes = 2048
	detailExistingMaxRead = 1 << 20
	detailLargeShrinkMin  = 20
	// detailDiffMaxCells bounds the LCS table so a pathological pair of
	// large files can't burn memory/time; beyond it the diff is a plain
	// delete-all/add-all.
	detailDiffMaxCells = 1_000_000
)

// PermissionDetail describes what an edit/execute tool call will do, so a
// human can tell a whole-file overwrite from an append. Everything derived
// from the EXISTING file's contents (Diff for a replacing write, existing
// stats) is only produced for non-sensitive paths inside the task worktree;
// otherwise Redacted is set and only counts of the new content are given.
type PermissionDetail struct {
	Kind             string `json:"kind"` // "write" | "edit" | "execute" | "other"
	Path             string `json:"path,omitempty"`
	NewBytes         int    `json:"newBytes,omitempty"`
	NewLines         int    `json:"newLines,omitempty"`
	ExistingBytes    *int   `json:"existingBytes,omitempty"`
	ExistingLines    *int   `json:"existingLines,omitempty"`
	ReplacesExisting bool   `json:"replacesExisting,omitempty"`
	// OutsideWorktree: the resolved target lies outside the task worktree
	// (absolute path, `..` escape or symlink out), whether or not it exists.
	OutsideWorktree bool `json:"outsideWorktree,omitempty"`
	// LargeShrink: existing >= 20 lines and the new content has under half
	// of the existing line count.
	LargeShrink bool   `json:"largeShrink,omitempty"`
	Preview     string `json:"preview,omitempty"` // head of NEW content (tool input only)
	Diff        string `json:"diff,omitempty"`
	Command     string `json:"command,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Redacted    bool   `json:"redacted,omitempty"`
	// GitRule is set when an execute command contains a non-read-only
	// git/gh operation (see ClassifyGitCommand).
	GitRule *GitRuleInfo `json:"gitRule,omitempty"`
	// Policy is set by internal/runs when the permission policy engine
	// decided or escalated the request (ADR-0047 §2): the matched rule
	// (or floor) id, its why, and the normalised, heredoc-elided command,
	// so the card can show what matched instead of a bare command line.
	Policy *PolicyInfo `json:"policy,omitempty"`
}

// PolicyInfo is the policy engine's footprint on a permission request
// (ADR-0047 §2): what matched and the normalised target it judged.
type PolicyInfo struct {
	RuleID     string `json:"ruleId,omitempty"`
	Why        string `json:"why,omitempty"`
	Normalized string `json:"normalized,omitempty"`
	Action     string `json:"action,omitempty"`
}

// BuildPermissionDetail derives a PermissionDetail from an ACP permission
// toolCall (raw ToolCallUpdate JSON). It returns nil when the call is not an
// edit/execute or cannot be parsed.
func BuildPermissionDetail(worktree string, toolCall json.RawMessage) *PermissionDetail {
	var tc struct {
		Title     string `json:"title"`
		Kind      string `json:"kind"`
		Locations []struct {
			Path string `json:"path"`
		} `json:"locations"`
		RawInput map[string]any `json:"rawInput"`
	}
	if err := json.Unmarshal(toolCall, &tc); err != nil {
		return nil
	}
	str := func(k string) (string, bool) {
		s, ok := tc.RawInput[k].(string)
		return s, ok
	}
	switch tc.Kind {
	case "execute":
		d := &PermissionDetail{Kind: "execute"}
		if cmd, ok := str("command"); ok {
			d.Command, d.Truncated = capBytes(cmd, detailCommandMaxBytes)
			d.GitRule = gitRuleInfo(worktree, cmd)
		}
		return d
	case "edit":
	default:
		return nil
	}

	path, _ := str("path")
	if path == "" && len(tc.Locations) > 0 {
		path = tc.Locations[0].Path
	}
	d := &PermissionDetail{Kind: "write", Path: path}

	if oldText, ok := str("old_text"); ok {
		newText, _ := str("new_text")
		d.Kind = "edit"
		d.NewBytes, d.NewLines = len(newText), countLines(newText)
		if isSensitiveName(path) {
			d.Redacted = true
			return d
		}
		d.Preview, d.Truncated = capPreview(newText)
		var tr bool
		d.Diff, tr = capDiff(unifiedDiff(oldText, newText))
		d.Truncated = d.Truncated || tr
		return d
	}

	content, ok := str("content")
	if !ok {
		return d
	}
	d.NewBytes, d.NewLines = len(content), countLines(content)
	sensitive := isSensitiveName(path)
	if sensitive {
		d.Redacted = true
	} else {
		d.Preview, d.Truncated = capPreview(content)
	}
	if path == "" {
		return d
	}

	resolved, inside, exists := resolveInWorktree(worktree, path)
	d.OutsideWorktree = !inside
	if !exists {
		return d
	}
	// Existence is not secret: an overwrite is always flagged, even when
	// the existing contents are hidden.
	d.ReplacesExisting = true
	fi, err := os.Stat(resolved)
	if !inside || sensitive || err != nil || !fi.Mode().IsRegular() {
		d.Redacted = true
		return d
	}
	if fi.Size() > detailExistingMaxRead {
		sz := int(fi.Size())
		d.ExistingBytes = &sz
		d.Truncated = true
		return d
	}
	f, err := os.Open(resolved)
	if err != nil {
		return d
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, detailExistingMaxRead))
	if err != nil {
		return d
	}
	old := string(data)
	eb, el := len(old), countLines(old)
	d.ExistingBytes, d.ExistingLines = &eb, &el
	d.LargeShrink = el >= detailLargeShrinkMin && d.NewLines*2 < el
	var tr bool
	d.Diff, tr = capDiff(unifiedDiff(old, content))
	d.Truncated = d.Truncated || tr
	return d
}

// capBytes cuts s to at most max bytes on a UTF-8 boundary.
func capBytes(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

func capLines(s string, maxLines int) (string, bool) {
	if strings.Count(s, "\n") < maxLines {
		return s, false
	}
	idx := 0
	for i := 0; i < maxLines; i++ {
		j := strings.IndexByte(s[idx:], '\n')
		if j < 0 {
			return s, false
		}
		idx += j + 1
	}
	if idx >= len(s) {
		return s, false
	}
	return s[:idx], true
}

func capText(s string, maxBytes, maxLines int) (string, bool) {
	s, t1 := capLines(s, maxLines)
	s, t2 := capBytes(s, maxBytes)
	return s, t1 || t2
}

func capPreview(s string) (string, bool) {
	return capText(s, detailPreviewMaxBytes, detailPreviewMaxLines)
}
func capDiff(s string) (string, bool) { return capText(s, detailDiffMaxBytes, detailDiffMaxLines) }

// isSensitiveName reports whether a file's base name looks like it holds
// secrets.
func isSensitiveName(path string) bool {
	b := strings.ToLower(filepath.Base(path))
	switch {
	case b == ".env", strings.HasPrefix(b, ".env."),
		strings.HasSuffix(b, ".pem"), strings.HasSuffix(b, ".key"),
		strings.HasPrefix(b, "id_rsa"), strings.HasPrefix(b, "id_ed25519"),
		strings.Contains(b, "credential"), strings.Contains(b, "secret"):
		return true
	}
	return false
}

// resolveInWorktree resolves path against worktree, following symlinks, and
// reports whether the result is inside the worktree and whether the file
// exists. It only Lstat/EvalSymlinks -- it never reads file contents.
func resolveInWorktree(worktree, path string) (resolved string, inside, exists bool) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(worktree, abs)
	}
	abs = filepath.Clean(abs)
	root, rerr := realpath.Resolve(worktree)
	if rerr != nil {
		root = filepath.Clean(worktree)
	}
	within := func(p string) bool {
		rel, err := filepath.Rel(root, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	if _, err := os.Lstat(abs); err != nil {
		// Not there yet: resolve the nearest existing ancestor and judge
		// the remainder lexically.
		anc, rest := abs, ""
		for {
			if rp, err := realpath.Resolve(anc); err == nil {
				anc = rp
				break
			}
			next := filepath.Dir(anc)
			if next == anc {
				break
			}
			rest = filepath.Join(filepath.Base(anc), rest)
			anc = next
		}
		return abs, within(filepath.Join(anc, rest)), false
	}
	if rerr != nil {
		return abs, false, true
	}
	real, err := realpath.Resolve(abs)
	if err != nil {
		return abs, false, true
	}
	return real, within(real), true
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// unifiedDiff renders a unified-style diff (3 lines of context) from a to b.
func unifiedDiff(a, b string) string {
	al, bl := splitLines(a), splitLines(b)
	type op struct {
		tag  byte
		text string
	}
	var ops []op
	// trim common prefix/suffix
	p := 0
	for p < len(al) && p < len(bl) && al[p] == bl[p] {
		p++
	}
	s := 0
	for s < len(al)-p && s < len(bl)-p && al[len(al)-1-s] == bl[len(bl)-1-s] {
		s++
	}
	for _, l := range al[:p] {
		ops = append(ops, op{' ', l})
	}
	am, bm := al[p:len(al)-s], bl[p:len(bl)-s]
	if len(am)*len(bm) > detailDiffMaxCells {
		for _, l := range am {
			ops = append(ops, op{'-', l})
		}
		for _, l := range bm {
			ops = append(ops, op{'+', l})
		}
	} else {
		n, m := len(am), len(bm)
		lcs := make([][]int32, n+1)
		for i := range lcs {
			lcs[i] = make([]int32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if am[i] == bm[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else if lcs[i+1][j] >= lcs[i][j+1] {
					lcs[i][j] = lcs[i+1][j]
				} else {
					lcs[i][j] = lcs[i][j+1]
				}
			}
		}
		i, j := 0, 0
		for i < n && j < m {
			switch {
			case am[i] == bm[j]:
				ops = append(ops, op{' ', am[i]})
				i++
				j++
			case lcs[i+1][j] >= lcs[i][j+1]:
				ops = append(ops, op{'-', am[i]})
				i++
			default:
				ops = append(ops, op{'+', bm[j]})
				j++
			}
		}
		for ; i < n; i++ {
			ops = append(ops, op{'-', am[i]})
		}
		for ; j < m; j++ {
			ops = append(ops, op{'+', bm[j]})
		}
	}
	for _, l := range al[len(al)-s:] {
		ops = append(ops, op{' ', l})
	}

	const ctx = 3
	keep := make([]bool, len(ops))
	changed := false
	for i, o := range ops {
		if o.tag == ' ' {
			continue
		}
		changed = true
		for k := max(0, i-ctx); k <= min(len(ops)-1, i+ctx); k++ {
			keep[k] = true
		}
	}
	if !changed {
		return ""
	}
	var sb strings.Builder
	skipped := false
	for i, o := range ops {
		if !keep[i] {
			skipped = true
			continue
		}
		if skipped {
			sb.WriteString("@@ ...\n")
			skipped = false
		}
		sb.WriteByte(o.tag)
		sb.WriteString(o.text)
		sb.WriteByte('\n')
	}
	return sb.String()
}
