package steward

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// GitFacts is what the collector reads from the repository, read-only.
type GitFacts struct {
	Numstat    string         // `git diff --numstat <diffBase>...<head>` raw text
	Commits    []CommitInfo   // oldest first, from `git log --reverse <baseSHA>..<head>`
	Stacking   StackInfo      // stacked-on-another-PR detection against origin/develop
	SecretHits map[string]int // label -> count over ADDED lines, never values
}

// developRef is the branch a task is normally based on.
const developRef = "origin/develop"

// gitCallTimeout bounds every single git invocation.
const gitCallTimeout = 60 * time.Second

var sha40Re = regexp.MustCompile(`^[0-9a-f]{40}$`)

// CollectGit gathers the git-side facts for the digest. diffBase is the ref
// the diff is taken against (usually the PR base); baseSHA and head are full
// commit ids. Every git call is read-only (--no-optional-locks, no index
// writes) and inherits no GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE from the
// caller's environment, so `-C dir` always decides the repository.
func CollectGit(ctx context.Context, dir, diffBase, baseSHA, head string) (GitFacts, error) {
	if err := ctx.Err(); err != nil {
		return GitFacts{}, err
	}
	if !sha40Re.MatchString(head) {
		return GitFacts{}, fmt.Errorf("head %q is not a full 40-hex commit id", head)
	}
	if !sha40Re.MatchString(baseSHA) {
		return GitFacts{}, fmt.Errorf("baseSHA %q is not a full 40-hex commit id", baseSHA)
	}
	if !safeRef(diffBase) {
		return GitFacts{}, fmt.Errorf("diffBase %q is not a ref or sha", diffBase)
	}
	rng := diffBase + "..." + head
	numstat, err := gitOut(ctx, dir, "diff", "--numstat", "-M", rng)
	if err != nil {
		return GitFacts{}, err
	}
	commits, err := gitCommits(ctx, dir, baseSHA+".."+head)
	if err != nil {
		return GitFacts{}, err
	}
	stacking, err := gitStacking(ctx, dir, diffBase, baseSHA, head)
	if err != nil {
		return GitFacts{}, err
	}
	added, err := gitAddedLines(ctx, dir, rng)
	if err != nil {
		return GitFacts{}, err
	}
	return GitFacts{
		Numstat:    numstat,
		Commits:    commits,
		Stacking:   stacking,
		SecretHits: SecretScan(added),
	}, nil
}

// safeRef reports whether ref can be passed to git as a revision: not empty,
// not option-shaped (leading "-") and free of whitespace and NUL.
func safeRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	return !strings.ContainsFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || r == 0 })
}

func gitCommits(ctx context.Context, dir, rng string) ([]CommitInfo, error) {
	out, err := gitOut(ctx, dir, "log", "--reverse", "--format=%H%x1f%s%x1f%B%x1e", rng)
	if err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		commits = append(commits, CommitInfo{
			SHA:            parts[0],
			Subject:        parts[1],
			HasAttribution: hasAttribution(parts[2]),
		})
	}
	return commits, nil
}

// hasAttribution reports whether any line of the commit message starts with
// a Co-Authored-By trailer (case-insensitive).
func hasAttribution(msg string) bool {
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(strings.ToLower(line), "co-authored-by:") {
			return true
		}
	}
	return false
}

// gitStacking decides whether the task's base is reachable from develop and
// which commits on head are foreign (on the branch but neither on develop
// nor among the task's own commits). Without an origin/develop ref there is
// nothing to compare against and the zero StackInfo is returned.
func gitStacking(ctx context.Context, dir, diffBase, baseSHA, head string) (StackInfo, error) {
	if err := gitRun(ctx, dir, "rev-parse", "--verify", "--quiet", developRef+"^{commit}"); err != nil {
		if ctx.Err() != nil {
			return StackInfo{}, ctx.Err()
		}
		return StackInfo{}, nil
	}
	reachable := false
	mb, mbErr := gitOut(ctx, dir, "merge-base", diffBase, head)
	if mbErr != nil {
		if ctx.Err() != nil {
			return StackInfo{}, ctx.Err()
		}
	} else {
		ok, err := gitIsAncestor(ctx, dir, baseSHA, developRef)
		if err != nil {
			return StackInfo{}, err
		}
		if !ok {
			if mb = strings.TrimSpace(mb); mb != "" {
				ok, err = gitIsAncestor(ctx, dir, mb, developRef)
				if err != nil {
					return StackInfo{}, err
				}
			}
		}
		reachable = ok
	}
	foreign, err := gitForeignCommits(ctx, dir, baseSHA, head)
	if err != nil {
		return StackInfo{}, err
	}
	return StackInfo{BaseReachableFromDevelop: reachable, ForeignCommits: foreign}, nil
}

func gitForeignCommits(ctx context.Context, dir, baseSHA, head string) ([]string, error) {
	onBranch, err := gitOut(ctx, dir, "rev-list", developRef+".."+head)
	if err != nil {
		return nil, err
	}
	own, err := gitOut(ctx, dir, "rev-list", baseSHA+".."+head)
	if err != nil {
		return nil, err
	}
	ownSet := make(map[string]bool, 8)
	for _, sha := range strings.Fields(own) {
		ownSet[sha] = true
	}
	var foreign []string
	for _, sha := range strings.Fields(onBranch) {
		if !ownSet[sha] {
			foreign = append(foreign, sha)
		}
	}
	return foreign, nil
}

// gitAddedLines maps each diffed file path to its added lines, from a
// zero-context diff. Test fixtures (*_test.go, testdata/) are skipped.
func gitAddedLines(ctx context.Context, dir, rng string) (map[string][]string, error) {
	out, err := gitOut(ctx, dir, "diff", "-U0", "--no-color", rng)
	if err != nil {
		return nil, err
	}
	added := map[string][]string{}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+++ ") {
			path = ""
			if p, ok := strings.CutPrefix(line, "+++ b/"); ok {
				path = unquotePath(p)
			}
			continue
		}
		if path == "" || skipSecretPath(path) {
			continue
		}
		if strings.HasPrefix(line, "+") {
			added[path] = append(added[path], line[1:])
		}
	}
	return added, nil
}

// skipSecretPath reports whether added lines from path are never secrets:
// tests and test fixtures.
func skipSecretPath(path string) bool {
	return strings.HasSuffix(path, "_test.go") ||
		strings.Contains(path, "/testdata/") ||
		strings.HasPrefix(path, "testdata/")
}

func unquotePath(p string) string {
	if len(p) >= 2 && strings.HasPrefix(p, `"`) && strings.HasSuffix(p, `"`) {
		return p[1 : len(p)-1]
	}
	return p
}

var secretRules = []struct {
	label string
	re    *regexp.Regexp
}{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"aws-key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"github-token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`)},
	{"slack-token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"api-key", regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)},
	{"assignment", regexp.MustCompile(`(?i)(api[_-]?key|secret|token|passw(or)?d)\s*[:=]\s*["'][^"'\s]{12,}["']`)},
}

// SecretScan counts secret-pattern hits per label over addedByPath, at most
// one hit per label per line. Only labels with a positive count are present
// and no matched value is ever returned.
func SecretScan(addedByPath map[string][]string) map[string]int {
	counts := map[string]int{}
	for _, lines := range addedByPath {
		for _, line := range lines {
			for _, rule := range secretRules {
				if rule.re.MatchString(line) {
					counts[rule.label]++
				}
			}
		}
	}
	return counts
}

// gitCmd builds a git call for dir. GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE are
// dropped so the caller's environment cannot override `-C dir`, and
// --no-optional-locks keeps the call from touching the worktree's index.
func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_WORK_TREE=") || strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	return cmd
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitCallTimeout)
	defer cancel()
	out, err := gitCmd(ctx, dir, args...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

func gitRun(ctx context.Context, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, gitCallTimeout)
	defer cancel()
	if err := gitCmd(ctx, dir, args...).Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("git %s: %w", args[0], err)
	}
	return nil
}

// gitIsAncestor reports whether commit is an ancestor of (or equal to) of;
// merge-base --is-ancestor exits 1 when it is not.
func gitIsAncestor(ctx context.Context, dir, commit, of string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, gitCallTimeout)
	defer cancel()
	err := gitCmd(ctx, dir, "merge-base", "--is-ancestor", commit, of).Run()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
}
