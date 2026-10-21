package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Finding kinds. "arm_escape" is raised by the caller (the arm's own
// bookkeeping), not by Tripwire.Check.
const (
	KindRepoChanged   = "repo_changed"
	KindSkillsChanged = "skills_changed"
	KindInitTools     = "init_tools"
	KindArmEscape     = "arm_escape"
)

// gitTimeout bounds each read-only git invocation.
const gitTimeout = 30 * time.Second

// TripwireConfig names what the arm must never change: the human's real
// checkout and the skill dirs the human's own agents load (like
// ~/.claude/skills, <repo>/.claude/skills, <repo>/.agents/skills).
type TripwireConfig struct {
	RepoDir        string
	HumanSkillDirs []string
}

// Finding is one tripwire hit. Detail names what changed, never contents.
type Finding struct{ Kind, Detail string }

// Tripwire is a snapshot of the human's files taken before the arm runs
// (R7). It never writes to the checkout and takes no index.lock
// (`git --no-optional-locks`).
//
// Findings are only STATE CHANGES or a wrong tool surface. A DENIED attempt is
// not a finding: it is the sandbox working, and is counted by Policy.Denials
// and the result's permission_denials instead.
//
// Known limit: the git status hash does not see a further edit to a file that
// was already modified (or already untracked) before the arm started.
type Tripwire struct {
	cfg  TripwireConfig
	snap tripSnapshot
}

type tripSnapshot struct {
	head, status, refs string
	skills             map[string]string // dir -> content hash or "absent"
}

// Arm snapshots the checkout and skill dirs. Any error is returned: an arm
// must not start without a baseline.
func Arm(cfg TripwireConfig) (*Tripwire, error) {
	if !filepath.IsAbs(cfg.RepoDir) {
		return nil, errors.New("sandbox: tripwire repo dir must be absolute")
	}
	for _, d := range cfg.HumanSkillDirs {
		if !filepath.IsAbs(d) {
			return nil, fmt.Errorf("sandbox: tripwire skill dir %q must be absolute", d)
		}
	}
	snap, err := take(cfg)
	if err != nil {
		return nil, err
	}
	return &Tripwire{cfg: cfg, snap: snap}, nil
}

// Check re-snapshots and diffs. Any difference is a finding, and so is an
// error while re-reading (fail closed).
func (t *Tripwire) Check() []Finding {
	var out []Finding
	cur, err := takeRepo(t.cfg.RepoDir)
	if err != nil {
		out = append(out, Finding{KindRepoChanged, "cannot re-read the checkout: " + err.Error()})
	} else {
		for _, p := range []struct{ name, was, now string }{
			{"HEAD", t.snap.head, cur.head}, {"working tree/index status", t.snap.status, cur.status}, {"refs", t.snap.refs, cur.refs},
		} {
			if p.was != p.now {
				out = append(out, Finding{KindRepoChanged, p.name + " changed"})
			}
		}
	}
	for _, d := range t.cfg.HumanSkillDirs {
		h, err := hashTree(d)
		switch {
		case err != nil:
			out = append(out, Finding{KindSkillsChanged, "cannot re-read " + d + ": " + err.Error()})
		case h != t.snap.skills[d]:
			out = append(out, Finding{KindSkillsChanged, d + " changed"})
		}
	}
	return out
}

// CheckInit flags a wrong tool surface in the CLI's init report.
func CheckInit(o Observation) []Finding {
	var out []Finding
	if !sameSet(o.InitTools, AllowedTools()) {
		out = append(out, Finding{KindInitTools, "the CLI's tool list is not the allowlist"})
	}
	if o.MCPServers != 0 {
		out = append(out, Finding{KindInitTools, fmt.Sprintf("%d MCP server(s) loaded", o.MCPServers)})
	}
	if o.Plugins != 0 {
		out = append(out, Finding{KindInitTools, fmt.Sprintf("%d non-builtin plugin(s) loaded", o.Plugins)})
	}
	return out
}

type repoSnapshot struct{ head, status, refs string }

func take(cfg TripwireConfig) (tripSnapshot, error) {
	r, err := takeRepo(cfg.RepoDir)
	if err != nil {
		return tripSnapshot{}, err
	}
	s := tripSnapshot{head: r.head, status: r.status, refs: r.refs, skills: map[string]string{}}
	for _, d := range cfg.HumanSkillDirs {
		if s.skills[d], err = hashTree(d); err != nil {
			return tripSnapshot{}, fmt.Errorf("sandbox: tripwire skill dir %s: %w", d, err)
		}
	}
	return s, nil
}

func takeRepo(dir string) (repoSnapshot, error) {
	head, err := git(dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return repoSnapshot{}, err
	}
	status, err := git(dir, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return repoSnapshot{}, err
	}
	refs, err := git(dir, "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return repoSnapshot{}, err
	}
	return repoSnapshot{head: strings.TrimSpace(string(head)), status: sum(status), refs: sum(refs)}, nil
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// git runs one read-only git command in dir with a scrubbed environment.
func git(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(clip(stderr.String(), 200)))
	}
	return out, nil
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// hashTree returns a content hash of everything under dir: relative path,
// full mode, symlink targets (not followed) and file bytes. A missing dir is
// "absent"; any other read error is an error.
func hashTree(dir string) (string, error) {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return "absent", nil
	} else if err != nil {
		return "", err
	}
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fmt.Fprintf(h, "%q %o\n", rel, uint32(info.Mode()))
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "-> %q\n", target)
		case info.Mode().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(h, f)
			_ = f.Close()
			if err != nil {
				return err
			}
			fmt.Fprintln(h)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
