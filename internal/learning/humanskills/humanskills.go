// Package humanskills loads the names and descriptions of human/third-party
// skills for read-only dedupe. It never reads skill bodies (capped at the
// frontmatter head) and never opens anything for write.
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func readHead(fsys fs.FS, dir, dirName string) (Skill, bool) {
	f, err := fsys.Open(dir + "/SKILL.md")
	if err != nil {
		return Skill{}, false
	}
	defer f.Close()
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
	buf := make([]byte, 512)
	scanner.Buffer(buf, 512)
	var fields []fmField
	inFM := false
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t")
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
				fields[len(fields)-1].value += " " + trimmed
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
