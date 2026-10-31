// Package skills embeds smind's orchestration skills (plain SKILL.md files)
// and installs them, on explicit request only, into the per-agent skill
// directories (~/.claude/skills, ~/.agents/skills, ~/.codex/skills).
//
// Every installed skill directory carries a manifest
// (.smind-managed-files.json, path -> sha256) so install/uninstall touch only
// files smind itself wrote and never clobber user edits.
package skills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/fsutil"
	"github.com/spacingmind/smind/internal/realpath"
)

//go:embed bundle
var bundleFS embed.FS

// ManifestName is the per-skill-directory record of files smind manages.
const ManifestName = ".smind-managed-files.json"

// Skill statuses reported by Install, Uninstall and Status.
const (
	StatusInstalled = "installed" // on disk, matches the manifest and the bundle
	StatusOutdated  = "outdated"  // managed and unedited, but older than the bundle
	StatusModified  = "modified"  // user-edited (or pre-existing unmanaged) content differs
	StatusMissing   = "missing"   // not installed
	StatusRemoved   = "removed"   // uninstall removed it
)

// TargetNames lists the supported install targets in display order.
var TargetNames = []string{"claude", "agents", "codex"}

var targetDirs = map[string]string{
	"claude": ".claude/skills",
	"agents": ".agents/skills",
	"codex":  ".codex/skills",
}

// RepoTargetDirs are the skills directories, relative to Options.Root, that a
// repo install may use (Options.RepoSubdir).
var RepoTargetDirs = []string{".agents/skills", ".claude/skills"}

// RepoTarget is the target name used in Results when Options.Root is set.
const RepoTarget = "repo"

// ErrSymlinkedTarget is returned (wrapped) when a root-based install would
// have to traverse a symlink inside the root; nothing is ever written through
// one.
var ErrSymlinkedTarget = errors.New("refusing to write through a symlink")

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Skill is one bundled skill: its name (directory) and file contents.
type Skill struct {
	Name  string
	Files map[string][]byte // slash-separated relative path -> content
}

// Options configure Install, Uninstall and Status.
type Options struct {
	Home    string   // default os.UserHomeDir()
	Targets []string // subset of TargetNames; default all
	Names   []string // bundled skill names; default all
	Force   bool

	// Skills, when non-nil, replaces the embedded bundle as the set of skills
	// to operate on. Names and file keys are validated.
	Skills []Skill
	// Root, when set, makes the install target a repo: paths resolve against
	// Root (an absolute, existing directory) instead of Home, under
	// RepoSubdir (one of RepoTargetDirs, default ".agents/skills"). Targets is
	// ignored. Symlinks below Root are refused with ErrSymlinkedTarget.
	Root       string
	RepoSubdir string
}

// Result describes the outcome for one skill in one target.
type Result struct {
	Skill   string
	Target  string
	Dir     string
	Status  string
	Written []string // files written (install)
	Removed []string // files removed (uninstall)
	Skipped []string // modified files left untouched
}

// Bundle returns the embedded skills sorted by name.
func Bundle() ([]Skill, error) {
	entries, err := fs.ReadDir(bundleFS, "bundle")
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s := Skill{Name: e.Name(), Files: map[string][]byte{}}
		root := path.Join("bundle", e.Name())
		err := fs.WalkDir(bundleFS, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := bundleFS.ReadFile(p)
			if err != nil {
				return err
			}
			s.Files[strings.TrimPrefix(p, root+"/")] = b
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type manifest struct {
	Files map[string]string `json:"files"`
}

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type env struct {
	home    string // symlink-resolved
	roots   map[string]string
	targets []string
	skills  []Skill
	force   bool
	strict  bool // root mode: refuse any symlink below home
}

func validateSkills(in []Skill) ([]Skill, error) {
	for _, s := range in {
		if !skillNameRe.MatchString(s.Name) {
			return nil, fmt.Errorf("invalid skill name %q", s.Name)
		}
		for rel := range s.Files {
			if !validRel(rel) {
				return nil, fmt.Errorf("skill %s: invalid file path %q", s.Name, rel)
			}
		}
	}
	return in, nil
}

func prepareRoot(opts Options) (*env, error) {
	if !filepath.IsAbs(opts.Root) {
		return nil, fmt.Errorf("root %q must be an absolute path", opts.Root)
	}
	resolved, err := realpath.Resolve(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve root %s: %w", opts.Root, err)
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("root %s is not an existing directory", opts.Root)
	}
	sub := opts.RepoSubdir
	if sub == "" {
		sub = RepoTargetDirs[0]
	}
	ok := false
	for _, d := range RepoTargetDirs {
		ok = ok || d == sub
	}
	if !ok {
		return nil, fmt.Errorf("unsupported repo subdir %q (want one of %v)", sub, RepoTargetDirs)
	}
	return &env{
		home:    resolved,
		roots:   map[string]string{RepoTarget: filepath.Join(resolved, filepath.FromSlash(sub))},
		targets: []string{RepoTarget},
		force:   opts.Force,
		strict:  true,
	}, nil
}

func prepare(opts Options) (*env, error) {
	if opts.Root != "" {
		e, err := prepareRoot(opts)
		if err != nil {
			return nil, err
		}
		return e, e.selectSkills(opts)
	}
	home := opts.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		home = h
	}
	resolved, err := realpath.Resolve(home)
	if err != nil {
		return nil, fmt.Errorf("resolve home %s: %w", home, err)
	}
	targets := opts.Targets
	if len(targets) == 0 {
		targets = TargetNames
	}
	e := &env{home: resolved, roots: map[string]string{}, force: opts.Force}
	for _, t := range targets {
		rel, ok := targetDirs[t]
		if !ok {
			return nil, fmt.Errorf("unknown target %q (want claude, agents, codex or all)", t)
		}
		if _, dup := e.roots[t]; dup {
			continue
		}
		e.roots[t] = filepath.Join(home, filepath.FromSlash(rel))
		e.targets = append(e.targets, t)
	}
	return e, e.selectSkills(opts)
}

func (e *env) selectSkills(opts Options) error {
	var all []Skill
	var err error
	if opts.Skills != nil {
		all, err = validateSkills(opts.Skills)
	} else {
		all, err = Bundle()
	}
	if err != nil {
		return err
	}
	if len(opts.Names) == 0 {
		e.skills = all
		return nil
	}
	byName := map[string]Skill{}
	for _, s := range all {
		byName[s.Name] = s
	}
	seen := map[string]bool{}
	for _, n := range opts.Names {
		s, ok := byName[n]
		if !ok {
			return fmt.Errorf("unknown skill %q", n)
		}
		if !seen[n] {
			seen[n] = true
			e.skills = append(e.skills, s)
		}
	}
	return nil
}

// within reports whether p is the home directory or lies under it.
func within(home, p string) bool {
	rel, err := filepath.Rel(home, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkContained refuses p when it (or its nearest existing ancestor)
// resolves, through symlinks, to somewhere outside the home directory.
func (e *env) checkContained(p string) error {
	if e.strict {
		return e.checkNoSymlinks(p)
	}
	cur := p
	for {
		if _, err := os.Lstat(cur); err == nil {
			r, err := realpath.Resolve(cur)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", cur, err)
			}
			if !within(e.home, r) {
				return fmt.Errorf("refusing to touch %s: it resolves to %s, outside the home directory %s", p, r, e.home)
			}
			return nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil
		}
		cur = parent
	}
}

// checkFile is checkContained for a file path: in root mode the file itself
// must not be a symlink either; otherwise only its directory is checked.
func (e *env) checkFile(p string) error {
	if e.strict {
		return e.checkContained(p)
	}
	return e.checkContained(filepath.Dir(p))
}

// checkNoSymlinks refuses p unless it lies lexically inside the (already
// resolved) root and no existing component from the root down is a symlink.
func (e *env) checkNoSymlinks(p string) error {
	rel, err := filepath.Rel(e.home, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to touch %s: outside the root %s", p, e.home)
	}
	cur := e.home
	if rel == "." {
		return nil
	}
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fsutil.IsLink(fi.Mode()) {
			return fmt.Errorf("%w: %s is a symlink or junction; use a real directory", ErrSymlinkedTarget, cur)
		}
	}
	return nil
}

func readManifest(dir string) (manifest, error) {
	m := manifest{Files: map[string]string{}}
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	var parsed manifest
	if json.Unmarshal(b, &parsed) != nil {
		return m, nil // corrupt manifest: treat as unmanaged, never trust it
	}
	for rel, h := range parsed.Files {
		if validRel(rel) {
			m.Files[rel] = h
		}
	}
	return m, nil
}

func validRel(rel string) bool {
	c := path.Clean(rel)
	return c == rel && rel != "." && !path.IsAbs(rel) && !strings.HasPrefix(rel, "../") && rel != ".." && rel != ManifestName
}

func marshalManifest(m manifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func writeManifest(dir string, m manifest) error {
	b, err := marshalManifest(m)
	if err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(dir, ManifestName), b)
}

func writeIfChanged(p string, b []byte) error {
	if old, err := os.ReadFile(p); err == nil && string(old) == string(b) {
		return nil
	}
	return atomicWrite(p, b)
}

func atomicWrite(p string, b []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".smind-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, p); err != nil {
		cleanup()
		return err
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Install writes the selected bundled skills into the selected targets.
func Install(opts Options) ([]Result, error) {
	e, err := prepare(opts)
	if err != nil {
		return nil, err
	}
	var results []Result
	for _, t := range e.targets {
		root := e.roots[t]
		if err := e.checkContained(root); err != nil {
			return results, err
		}
		for _, s := range e.skills {
			dir := filepath.Join(root, s.Name)
			if err := e.checkContained(dir); err != nil {
				return results, err
			}
			r, err := e.installOne(t, dir, s)
			if err != nil {
				return results, fmt.Errorf("install %s into %s: %w", s.Name, dir, err)
			}
			results = append(results, r)
		}
	}
	return results, nil
}

func (e *env) installOne(target, dir string, s Skill) (Result, error) {
	res := Result{Skill: s.Name, Target: target, Dir: dir, Status: StatusInstalled}
	if err := e.checkFile(filepath.Join(dir, ManifestName)); err != nil {
		return res, err
	}
	m, err := readManifest(dir)
	if err != nil {
		return res, err
	}
	next := manifest{Files: map[string]string{}}
	for _, rel := range sortedKeys(s.Files) {
		want := s.Files[rel]
		wantHash := hashBytes(want)
		p := filepath.Join(dir, filepath.FromSlash(rel))
		// A subdirectory inside the skill dir could itself be a symlink out
		// of the home directory; check every file path, not just the root.
		if err := e.checkFile(p); err != nil {
			return res, err
		}
		disk, err := os.ReadFile(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// fresh write below
		case err != nil:
			return res, err
		default:
			diskHash := hashBytes(disk)
			if diskHash == wantHash {
				next.Files[rel] = wantHash // identical: adopt, nothing to write
				continue
			}
			managed, ok := m.Files[rel]
			if !e.force && (!ok || managed != diskHash) {
				res.Skipped = append(res.Skipped, rel)
				res.Status = StatusModified
				if ok {
					next.Files[rel] = managed
				}
				continue
			}
		}
		if err := atomicWrite(p, want); err != nil {
			return res, err
		}
		res.Written = append(res.Written, rel)
		next.Files[rel] = wantHash
	}
	// Entries for files no longer in the bundle stay put so uninstall can
	// still remove them.
	for rel, h := range m.Files {
		if _, inBundle := s.Files[rel]; !inBundle {
			next.Files[rel] = h
		}
	}
	if len(next.Files) > 0 {
		if err := writeManifest(dir, next); err != nil {
			return res, err
		}
	}
	return res, nil
}

// Uninstall removes only the files recorded in each skill's manifest (and
// whose content still matches it, unless Force), then the skill directory if
// it ends up empty.
func Uninstall(opts Options) ([]Result, error) {
	e, err := prepare(opts)
	if err != nil {
		return nil, err
	}
	var results []Result
	for _, t := range e.targets {
		root := e.roots[t]
		if err := e.checkContained(root); err != nil {
			return results, err
		}
		for _, s := range e.skills {
			dir := filepath.Join(root, s.Name)
			if err := e.checkContained(dir); err != nil {
				return results, err
			}
			r, err := e.uninstallOne(t, dir, s)
			if err != nil {
				return results, fmt.Errorf("uninstall %s from %s: %w", s.Name, dir, err)
			}
			results = append(results, r)
		}
	}
	return results, nil
}

func (e *env) uninstallOne(target, dir string, s Skill) (Result, error) {
	res := Result{Skill: s.Name, Target: target, Dir: dir, Status: StatusMissing}
	if err := e.checkFile(filepath.Join(dir, ManifestName)); err != nil {
		return res, err
	}
	m, err := readManifest(dir)
	if err != nil {
		return res, err
	}
	if len(m.Files) == 0 {
		return res, nil
	}
	res.Status = StatusRemoved
	keep := manifest{Files: map[string]string{}}
	for _, rel := range sortedKeys(m.Files) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := e.checkFile(p); err != nil {
			return res, err
		}
		disk, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return res, err
		}
		if !e.force && hashBytes(disk) != m.Files[rel] {
			res.Skipped = append(res.Skipped, rel)
			res.Status = StatusModified
			keep.Files[rel] = m.Files[rel]
			continue
		}
		if err := os.Remove(p); err != nil {
			return res, err
		}
		res.Removed = append(res.Removed, rel)
		pruneEmptyDirs(dir, filepath.Dir(p))
	}
	if len(keep.Files) > 0 {
		return res, writeManifest(dir, keep)
	}
	if err := os.Remove(filepath.Join(dir, ManifestName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	_ = os.Remove(dir) // only succeeds when empty
	return res, nil
}

// pruneEmptyDirs removes empty directories from d up to (excluding) stop.
func pruneEmptyDirs(stop, d string) {
	for d != stop && within(stop, d) {
		if os.Remove(d) != nil {
			return
		}
		d = filepath.Dir(d)
	}
}

// Status reports, per target and skill, whether it is installed, modified or
// missing. It never writes.
func Status(opts Options) ([]Result, error) {
	e, err := prepare(opts)
	if err != nil {
		return nil, err
	}
	var results []Result
	for _, t := range e.targets {
		root := e.roots[t]
		if err := e.checkContained(root); err != nil {
			return results, err
		}
		for _, s := range e.skills {
			dir := filepath.Join(root, s.Name)
			if err := e.checkContained(dir); err != nil {
				return results, err
			}
			m, err := readManifest(dir)
			if err != nil {
				return results, err
			}
			res := Result{Skill: s.Name, Target: t, Dir: dir, Status: StatusInstalled}
			for _, rel := range sortedKeys(s.Files) {
				disk, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
				if err != nil {
					res.Status = StatusMissing
					break
				}
				dh := hashBytes(disk)
				if managed, ok := m.Files[rel]; (!ok || managed != dh) && dh != hashBytes(s.Files[rel]) {
					res.Status = StatusModified
					break
				} else if dh != hashBytes(s.Files[rel]) {
					res.Status = StatusOutdated
				}
			}
			results = append(results, res)
		}
	}
	return results, nil
}
