package sandbox

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// PolicyRecord is one can_use_tool request the CLI routed to Policy and what
// was decided. Input values are not stored, only the resolved path.
type PolicyRecord struct {
	Tool    string
	Path    string
	Allowed bool
	Reason  string
}

// Counts summarises the requests a Policy has decided.
type Counts struct{ Allowed, Denied int }

// Policy is the permission decider for the probe arm (a
// claudecode.PermissionPolicy). It allows the built-in file tools only on
// paths that resolve (symlinks followed) inside ArmDir (read and write) or
// ReadOnlyDirs (Read/Glob/Grep only), denies every other tool, and records
// every request so a test can tell whether a call reached the decider at all.
// AllowBash, when set, lets a Bash request through if it returns true for the
// command; the default denies Bash (autoAllowBashIfSandboxed already runs
// sandboxed commands without asking, so a Bash request means the CLI wants to
// leave the sandbox). P0 measured that headless the CLI never consults it:
// it is belt and braces, and Decide never blocks or prompts a human.
type Policy struct {
	ArmDir       string
	ReadOnlyDirs []string
	AllowBash    func(command string) bool

	mu  sync.Mutex
	log []PolicyRecord
}

var (
	policyReadTools  = map[string]bool{"Read": true, "Glob": true, "Grep": true}
	policyWriteTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true, "MultiEdit": true}
)

// Records returns a copy of every request seen so far.
func (p *Policy) Records() []PolicyRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]PolicyRecord(nil), p.log...)
}

// Counts returns how many requests were allowed and denied so far.
func (p *Policy) Counts() Counts {
	p.mu.Lock()
	defer p.mu.Unlock()
	var c Counts
	for _, r := range p.log {
		if r.Allowed {
			c.Allowed++
		} else {
			c.Denied++
		}
	}
	return c
}

// Denials is the number of denied requests: a denied attempt is the sandbox
// working, so it is counted, not treated as a tripwire finding.
func (p *Policy) Denials() int { return p.Counts().Denied }

// Decide implements claudecode.PermissionPolicy.
func (p *Policy) Decide(_ context.Context, req claudecode.CanUseToolRequest) (bool, map[string]any, string, []claudecode.PermissionUpdate, bool, error) {
	allow, path, reason := p.decide(req)
	p.mu.Lock()
	p.log = append(p.log, PolicyRecord{Tool: req.ToolName, Path: path, Allowed: allow, Reason: reason})
	p.mu.Unlock()
	if !allow {
		return false, nil, "denied by the probe arm policy: " + reason, nil, false, nil
	}
	return true, req.Input, "", nil, false, nil
}

func (p *Policy) decide(req claudecode.CanUseToolRequest) (allow bool, path, reason string) {
	if req.ToolName == "Bash" {
		cmd, _ := req.Input["command"].(string)
		if p.AllowBash != nil && p.AllowBash(cmd) {
			return true, "", "bash command allowed by caller"
		}
		return false, "", "bash outside the sandbox is never allowed"
	}
	read, write := policyReadTools[req.ToolName], policyWriteTools[req.ToolName]
	if !read && !write {
		return false, "", "tool " + req.ToolName + " is not allowed"
	}
	var paths []string
	for _, k := range []string{"file_path", "path", "notebook_path"} {
		if v, ok := req.Input[k].(string); ok && v != "" {
			paths = append(paths, v)
		}
	}
	if pat, ok := req.Input["pattern"].(string); ok && req.ToolName == "Glob" {
		paths = append(paths, globPrefix(pat))
	}
	if len(paths) == 0 {
		paths = []string{p.ArmDir} // Glob/Grep without a path search the cwd
	}
	for _, raw := range paths {
		resolved := p.resolve(raw)
		path = resolved
		roots := []string{p.ArmDir}
		if read {
			roots = append(roots, p.ReadOnlyDirs...)
		}
		ok := false
		for _, root := range roots {
			if within(resolved, p.resolve(root)) {
				ok = true
				break
			}
		}
		if !ok {
			return false, resolved, "path is outside the arm"
		}
	}
	return true, path, "inside the arm"
}

// globPrefix is the literal directory part of a glob pattern, so an
// absolute or "../" pattern is judged by where it starts.
func globPrefix(pattern string) string {
	if i := strings.IndexAny(pattern, "*?[{"); i >= 0 {
		pattern = pattern[:i]
	}
	if pattern == "" {
		return "."
	}
	return pattern
}

// resolve makes raw absolute (relative to ArmDir), cleans it and follows
// symlinks through the longest existing prefix.
func (p *Policy) resolve(raw string) string {
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(p.ArmDir, raw)
	}
	raw = filepath.Clean(raw)
	rest := ""
	for cur := raw; ; cur = filepath.Dir(cur) {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		if cur == filepath.Dir(cur) {
			return raw
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
