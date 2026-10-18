// Package humanskills loads the names and descriptions of human/third-party
// skills for read-only dedupe. It never PARSES or retains body bytes; the
// scanner's read-ahead is bounded by MaxHeadBytes. It never opens anything
// for write, and skips SKILL.md entries that are not regular files (FIFOs,
// devices) so a hostile skills directory cannot hang the loader.
//
// A human skill named "learned-*" is returned like any other: it is a name
// the gate's collision check will then refuse for learned skills.
package humanskills

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

// Skill is a human skill's identity. There is no body field, by construction.
type Skill struct {
	Name        string
	Description string
}

// MaxHeadBytes caps how much of any SKILL.md is read.
const MaxHeadBytes = 8192

const (
	// MaxEntriesPerRoot caps how many directory entries of one root are
	// considered (in name order).
	MaxEntriesPerRoot = 500
	// MaxSkills caps the total number of skills returned.
	MaxSkills = 1000
)

// Load reads skills from each root directory inside fsys. Missing roots and
// unparseable skills are skipped, never an error.
func Load(fsys fs.FS, roots ...string) ([]Skill, error) {
	seen := make(map[string]bool)
	var out []Skill
	for _, root := range roots {
		entries, err := fs.ReadDir(fsys, root)
		if err != nil {
			continue
		}
		if len(entries) > MaxEntriesPerRoot {
			entries = entries[:MaxEntriesPerRoot]
		}
		for _, e := range entries {
			path := path.Join(root, e.Name())
			if st, err := fs.Stat(fsys, path); err != nil || !st.IsDir() {
				continue
			}
			s, ok := readHead(fsys, path, e.Name())
			if !ok || seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			out = append(out, s)
		}
	}
	return finish(out), nil
}

// finish sorts by name and applies the total cap.
func finish(out []Skill) []Skill {
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > MaxSkills {
		out = out[:MaxSkills]
	}
	return out
}

// LoadDirs loads skills from absolute directories on disk. A missing dir is
// skipped (no error). Results are deduped by name (first dir wins) and
// sorted by name.
func LoadDirs(dirs ...string) ([]Skill, error) {
	seen := make(map[string]bool)
	var out []Skill
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		skills, err := Load(os.DirFS(dir), ".")
		if err != nil {
			continue
		}
		for _, s := range skills {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			out = append(out, s)
		}
	}
	return finish(out), nil
}

func readHead(fsys fs.FS, dir, dirName string) (Skill, bool) {
	// Stat before Open: opening a FIFO for read blocks until a writer shows
	// up. Symlinks are followed on purpose (skill dirs are legitimately
	// symlinked); only the final target must be a regular file.
	if st, err := fs.Stat(fsys, dir+"/SKILL.md"); err != nil || !st.Mode().IsRegular() {
		return Skill{}, false
	}
	f, err := fsys.Open(dir + "/SKILL.md")
	if err != nil {
		return Skill{}, false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return Skill{}, false
	}
	s := parseHead(f)
	if s.Name == "" {
		s.Name = dirName
	}
	return s, true
}

type fmField struct {
	key   string
	value string
}

// parseHead reads at most MaxHeadBytes and stops at the closing frontmatter
// fence; the body beyond it is never read.
func parseHead(r io.Reader) Skill {
	scanner := bufio.NewScanner(io.LimitReader(r, MaxHeadBytes))
	// Start small so a short frontmatter stops the read early; allow one
	// line up to the head cap so a long description is not silently dropped.
	scanner.Buffer(make([]byte, 512), MaxHeadBytes)
	var fields []fmField
	inFM := false
	first := true
	for scanner.Scan() {
		text := scanner.Text()
		if first {
			text = strings.TrimPrefix(text, "\ufeff")
			first = false
		}
		line := strings.TrimRight(text, " \t")
		trimmed := strings.TrimSpace(line)
		if !inFM {
			if trimmed == "---" {
				inFM = true
			}
			// text before the fence: no frontmatter
			if trimmed != "" && trimmed != "---" {
				return Skill{}
			}
			continue
		}
		if trimmed == "---" {
			break // closing fence: stop before the body
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			// folded/literal continuation of the previous field
			if len(fields) > 0 {
				last := &fields[len(fields)-1]
				switch last.value {
				case ">", "|", ">-", "|-", ">+", "|+": // block scalar indicator, not text
					last.value = trimmed
				default:
					last.value += " " + trimmed
				}
			}
			continue
		}
		if k, v, found := strings.Cut(line, ":"); found {
			fields = append(fields, fmField{key: strings.TrimSpace(k), value: strings.TrimSpace(v)})
		}
	}
	var s Skill
	for _, f := range fields {
		switch f.key {
		case "name":
			s.Name = unquote(f.value)
		case "description":
			s.Description = unquote(f.value)
			// YAML block scalar indicators carry no text themselves.
			switch s.Description {
			case ">", "|", ">-", "|-", ">+", "|+":
				s.Description = ""
			}
		}
	}
	return s
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
