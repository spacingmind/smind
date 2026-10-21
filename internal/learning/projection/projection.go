// Package projection materialises the active learned skills of a workspace as
// a read-only, content-addressed directory the agent CLI loads through an
// add-dir (ADR-0026 §5). It is pure filesystem logic under a caller-given
// root: the caller decides which skills go in. Production callers of Project
// filter to active skills; the only path by which a proposed skill reaches an
// agent is ProjectProbe (probe.go), which needs a valid sandbox attestation
// (ADR-0037). Nothing outside root is touched.
package projection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spacingmind/smind/internal/learning/gate"
)

// keepProjections is how many hash directories of one workspace survive GC:
// the current one plus the previous, which an in-flight run may still read.
const keepProjections = 2

// staleTempAge is how old an abandoned build directory must be before GC
// removes it (a live build never lives that long).
const staleTempAge = time.Hour

// Skill is an active learned skill. The caller filters by lifecycle state.
type Skill struct{ Name, Category, Description, Body string }

// Projection locates one materialised skill set.
type Projection struct {
	Dir       string // add-dir root: <root>/<workspaceID>/<hash>
	Hash      string // first 16 hex chars of sha256 over the rendered skills
	SkillsDir string // <Dir>/.claude/skills
}

var locks sync.Map // cleaned root + workspace dir -> *sync.Mutex

func lockFor(wsDir string) *sync.Mutex {
	m, _ := locks.LoadOrStore(wsDir, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Project materialises skills under root and returns where. Empty skills
// returns the zero Projection and creates nothing; it also does not remove an
// older tree (use Remove for that).
func Project(root string, workspaceID int64, skills []Skill) (Projection, error) {
	if len(skills) == 0 {
		return Projection{}, nil
	}
	if workspaceID <= 0 {
		return Projection{}, fmt.Errorf("projection: invalid workspace id %d", workspaceID)
	}
	files, hash, err := render(skills)
	if err != nil {
		return Projection{}, err
	}
	root = filepath.Clean(root)
	wsDir := filepath.Join(root, strconv.FormatInt(workspaceID, 10))
	final := filepath.Join(wsDir, hash)
	if err := underRoot(root, final); err != nil {
		return Projection{}, err
	}

	mu := lockFor(wsDir)
	mu.Lock()
	defer mu.Unlock()

	if err := ensureDirs(root, wsDir); err != nil {
		return Projection{}, err
	}
	p := Projection{Dir: final, Hash: hash, SkillsDir: filepath.Join(final, ".claude", "skills")}

	if _, err := os.Lstat(final); err == nil {
		if verify(final, files) == nil {
			touch(final)
			gc(wsDir, hash)
			return p, nil
		}
		if err := removeTree(final); err != nil {
			return Projection{}, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Projection{}, err
	}

	tmp, err := os.MkdirTemp(wsDir, ".tmp-")
	if err != nil {
		return Projection{}, err
	}
	if err := build(tmp, files); err != nil {
		_ = removeTree(tmp)
		return Projection{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		// A second process may have won the race; accept its result if it is
		// exactly what we would have written.
		_ = removeTree(tmp)
		if (errors.Is(err, syscall.EEXIST) || errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, fs.ErrExist)) && verify(final, files) == nil {
			touch(final)
			gc(wsDir, hash)
			return p, nil
		}
		return Projection{}, fmt.Errorf("projection: publish: %w", err)
	}
	touch(final)
	gc(wsDir, hash)
	return p, nil
}

// Remove deletes the workspace's whole projection tree. A missing tree is not
// an error.
func Remove(root string, workspaceID int64) error {
	if workspaceID <= 0 {
		return fmt.Errorf("projection: invalid workspace id %d", workspaceID)
	}
	root = filepath.Clean(root)
	wsDir := filepath.Join(root, strconv.FormatInt(workspaceID, 10))
	mu := lockFor(wsDir)
	mu.Lock()
	defer mu.Unlock()

	rootInfo, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("projection: root %q is not a plain directory", root)
	}
	info, err := os.Lstat(wsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("projection: %q is not a plain directory", wsDir)
	}
	return removeTree(wsDir)
}

// render validates every skill and returns relative path -> content plus the
// content hash.
func render(skills []Skill) (map[string][]byte, string, error) {
	type item struct{ name, text string }
	items := make([]item, 0, len(skills))
	seen := map[string]bool{}
	for _, s := range skills {
		if f := gate.CheckName(s.Name); len(f) > 0 {
			return nil, "", fmt.Errorf("projection: bad skill name %q", s.Name)
		}
		if seen[s.Name] {
			return nil, "", fmt.Errorf("projection: duplicate skill %q", s.Name)
		}
		seen[s.Name] = true
		items = append(items, item{s.Name, gate.Render(gate.Skill(s))})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	h := sha256.New()
	files := make(map[string][]byte, len(items))
	for _, it := range items {
		h.Write([]byte(it.name))
		h.Write([]byte{0})
		h.Write([]byte(it.text))
		h.Write([]byte{0})
		files[filepath.Join(".claude", "skills", it.name, "SKILL.md")] = []byte(it.text)
	}
	return files, hex.EncodeToString(h.Sum(nil))[:16], nil
}

// underRoot asserts path resolves lexically under root.
func underRoot(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("projection: path %q escapes root %q", path, root)
	}
	return nil
}

// ensureDirs creates root and wsDir if missing and refuses symlinks or
// non-directories for either.
func ensureDirs(root, wsDir string) error {
	for _, d := range []string{root, wsDir} {
		info, err := os.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
			info, err = os.Lstat(d)
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("projection: %q is not a plain directory", d)
		}
	}
	return nil
}

// build writes files under dir, then locks it read-only (files 0444, dirs
// 0555, deepest first so chmod never needs a writable parent).
func build(dir string, files map[string][]byte) error {
	for rel, data := range files {
		path := filepath.Join(dir, rel)
		if err := underRoot(dir, path); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o444); err != nil {
			return err
		}
	}
	var dirs []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o555); err != nil {
			return err
		}
	}
	return nil
}

// verify checks dir holds exactly files: same entries, no symlinks, matching
// content and (off windows) read-only modes.
func verify(dir string, files map[string][]byte) error {
	want := map[string]bool{".": true}
	for rel := range files {
		for p := rel; p != "." && p != string(filepath.Separator); p = filepath.Dir(p) {
			want[p] = true
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("not a plain directory")
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if !want[rel] {
			return fmt.Errorf("unexpected entry %q", rel)
		}
		delete(want, rel)
		mode := d.Type()
		if d.IsDir() {
			if runtime.GOOS != "windows" {
				if fi, err := d.Info(); err != nil || fi.Mode().Perm() != 0o555 {
					return fmt.Errorf("directory %q not read-only", rel)
				}
			}
			return nil
		}
		exp, ok := files[rel]
		if !ok || !mode.IsRegular() {
			return fmt.Errorf("unexpected non-file %q", rel)
		}
		if runtime.GOOS != "windows" {
			if fi, err := d.Info(); err != nil || fi.Mode().Perm() != 0o444 {
				return fmt.Errorf("file %q not read-only", rel)
			}
		}
		got, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, exp) {
			return fmt.Errorf("file %q content differs", rel)
		}
		return nil
	})
}

// touch marks dir as the most recently used projection for GC ordering.
func touch(dir string) {
	now := time.Now()
	_ = os.Chtimes(dir, now, now)
}

// gc removes the workspace's other hash directories beyond the newest
// keepProjections (current counted) and long-abandoned build directories.
// Best effort: a failed removal is retried by the next Project.
func gc(wsDir, current string) {
	ents, err := os.ReadDir(wsDir)
	if err != nil {
		return
	}
	type cand struct {
		name string
		mod  time.Time
	}
	var others []cand
	for _, e := range ents {
		if e.Name() == current || e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if strings.HasPrefix(e.Name(), ".tmp-") {
			if time.Since(info.ModTime()) > staleTempAge {
				_ = removeTree(filepath.Join(wsDir, e.Name()))
			}
			continue
		}
		if !e.IsDir() {
			continue
		}
		others = append(others, cand{e.Name(), info.ModTime()})
	}
	sort.Slice(others, func(i, j int) bool {
		if !others[i].mod.Equal(others[j].mod) {
			return others[i].mod.After(others[j].mod)
		}
		return others[i].name > others[j].name
	})
	for i, c := range others {
		if i < keepProjections-1 {
			continue
		}
		_ = removeTree(filepath.Join(wsDir, c.name))
	}
}

// removeTree deletes path after making everything inside writable (the
// projection is read-only by design). Symlinks are never followed.
func removeTree(path string) error {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	return os.RemoveAll(path)
}
