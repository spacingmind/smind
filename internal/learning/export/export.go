// Package export copies ONE approved learned skill to a repo or a user-level
// agent skills directory, on explicit human request only. It reuses the
// internal/skills install machinery (manifest, never clobber a changed file)
// and never writes through a symlink. smind keeps no record afterwards: the
// exported file is the user's, and the ledger entry is the caller's job.
package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/skills"
	"github.com/spacingmind/smind/internal/store"
)

// Target is where a skill is exported to.
type Target string

const (
	TargetRepo   Target = "repo"
	TargetClaude Target = "claude"
	TargetAgents Target = "agents"
	TargetCodex  Target = "codex"
)

// Request describes one export.
type Request struct {
	Skill    store.LearnedSkill
	Target   Target
	RepoPath string // workspace path; required for TargetRepo
	Home     string // default os.UserHomeDir()
	Force    bool   // overwrite a file smind did not write or that was edited
}

// Result describes the outcome of an export.
type Result struct {
	Dir     string
	Files   []string // files written by this call
	Status  string   // skills.StatusInstalled, or skills.StatusModified when something was left alone
	Skipped []string // existing files that differ and were not overwritten
}

// Export writes <name>/SKILL.md into the chosen target.
func Export(r Request) (Result, error) {
	var res Result
	switch r.Skill.Status {
	case "active", "disabled", "archived":
	default:
		return res, fmt.Errorf("skill %q has status %q; only active, disabled or archived skills can be exported", r.Skill.Name, r.Skill.Status)
	}
	if fs := gate.CheckName(r.Skill.Name); len(fs) > 0 {
		return res, fmt.Errorf("invalid skill name %q", r.Skill.Name)
	}
	if strings.TrimSpace(r.Skill.Body) == "" {
		return res, errors.New("skill body is empty")
	}
	sk := skills.Skill{Name: r.Skill.Name, Files: map[string][]byte{"SKILL.md": []byte(gate.Render(gate.Skill{
		Name:        r.Skill.Name,
		Category:    r.Skill.Category,
		Description: r.Skill.Description,
		Body:        r.Skill.Body,
	}))}}
	opts := skills.Options{Skills: []skills.Skill{sk}, Force: r.Force}
	switch r.Target {
	case TargetRepo:
		if !filepath.IsAbs(r.RepoPath) {
			return res, fmt.Errorf("repo path %q must be absolute", r.RepoPath)
		}
		if fi, err := os.Stat(r.RepoPath); err != nil || !fi.IsDir() {
			return res, fmt.Errorf("repo path %s is not an existing directory", r.RepoPath)
		}
		opts.Root = r.RepoPath
		opts.RepoSubdir = repoSubdir(r.RepoPath)
	case TargetClaude, TargetAgents, TargetCodex:
		opts.Home = r.Home
		opts.Targets = []string{string(r.Target)}
	default:
		return res, fmt.Errorf("unknown export target %q (want repo, claude, agents or codex)", r.Target)
	}
	rs, err := skills.Install(opts)
	if err != nil {
		if errors.Is(err, skills.ErrSymlinkedTarget) {
			return res, fmt.Errorf("%w; export to a real directory (e.g. create a real .agents/skills) or use a user-level target", err)
		}
		return res, err
	}
	if len(rs) != 1 {
		return res, fmt.Errorf("unexpected install result count %d", len(rs))
	}
	return Result{Dir: rs[0].Dir, Files: rs[0].Written, Status: rs[0].Status, Skipped: rs[0].Skipped}, nil
}

// repoSubdir prefers .agents/skills; when only .claude/skills (or a .claude
// symlink) exists it picks that so the installer can refuse it if symlinked.
func repoSubdir(root string) string {
	exists := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
		return err == nil
	}
	if exists(".agents/skills") {
		return ".agents/skills"
	}
	if fi, err := os.Lstat(filepath.Join(root, ".claude")); err == nil && fi.Mode()&os.ModeSymlink != 0 || exists(".claude/skills") {
		return ".claude/skills"
	}
	return ".agents/skills"
}
