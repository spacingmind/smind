package taskrunner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

const (
	gitStagedMaxFiles = 50
	gitStagedTimeout  = 2 * time.Second
)

// GitRuleInfo is the wire form of a GitRule carried on a PermissionDetail, so
// a permission card can say why a git/gh operation always needs approval and
// show its target. PerCall is always true: a rule is never a standing allow.
type GitRuleInfo struct {
	ID              string   `json:"id"`
	Class           string   `json:"class"` // "always-ask" | "commit" | "unparsed-git"
	Label           string   `json:"label"`
	Command         string   `json:"command"`
	Remote          string   `json:"remote,omitempty"`
	Branch          string   `json:"branch,omitempty"`
	PerCall         bool     `json:"perCall"`
	StagedFiles     []string `json:"stagedFiles,omitempty"`
	StagedTruncated bool     `json:"stagedTruncated,omitempty"`
	Note            string   `json:"note,omitempty"`
}

const unparsedGitNote = "could not fully parse this command; treated as a git operation"

// gitRuleInfo classifies cmd and returns the wire form of its rule, or nil
// when cmd has no git/gh operation or only a read-only one. For a commit it
// lists the files staged in cwd (best effort: empty on any failure).
func gitRuleInfo(cwd, cmd string) *GitRuleInfo {
	r := ClassifyGitCommand(cmd)
	if r == nil || r.Class == GitClassReadOnly {
		return nil
	}
	info := &GitRuleInfo{
		ID:      r.ID,
		Class:   string(r.Class),
		Label:   r.Label,
		Command: r.Command,
		Remote:  r.Remote,
		Branch:  r.Branch,
		PerCall: true,
	}
	switch r.Class {
	case GitClassUnparsedGit:
		info.Note = unparsedGitNote
	case GitClassCommit:
		info.StagedFiles, info.StagedTruncated = stagedFiles(cwd)
	}
	return info
}

// stagedFiles lists `git diff --cached --name-only` in cwd, capped at
// gitStagedMaxFiles. Any failure yields no files.
func stagedFiles(cwd string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitStagedTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "-C", cwd, "diff", "--cached", "--name-only")
	c.Env = append(c.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	c.Stdout = &out
	if err := c.Run(); err != nil {
		return nil, false
	}
	var files []string
	for _, l := range strings.Split(out.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	if len(files) > gitStagedMaxFiles {
		return files[:gitStagedMaxFiles], true
	}
	return files, false
}
