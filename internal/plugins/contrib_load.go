package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Caps on what a plugin's data contributions may weigh; they bound what a
// plugin can put into an agent's prompt and what the projection copies.
const (
	maxProfileFileBytes = 64 << 10
	maxSkillFileBytes   = 128 << 10
	maxSkillTotalBytes  = 256 << 10
	maxSkillFiles       = 32
)

var (
	skillNameRe   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	profileSkillR = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*(:[a-z0-9]+(-[a-z0-9]+)*)?$`)
)

// SkillSpec is one skill a plugin contributes: the SKILL.md plus any files
// beside it, read from the plugin directory.
type SkillSpec struct {
	Name        string
	Description string
	Files       map[string][]byte // slash-separated relative path -> content
}

// ProfileSpec is the JSON a profile contribution file holds. It names the
// same fields as a user's profile (ADR-0014/0041) except where a stored id
// would be: childProfiles lists the plugin's own profiles by name.
type ProfileSpec struct {
	Name               string   `json:"name"`
	Provider           string   `json:"provider"`
	PermissionMode     string   `json:"permissionMode,omitempty"`
	AutoAccept         bool     `json:"autoAccept,omitempty"`
	ThinkingLevel      string   `json:"thinkingLevel,omitempty"`
	Model              string   `json:"model,omitempty"`
	Notes              string   `json:"notes,omitempty"`
	OnRateLimit        string   `json:"onRateLimit,omitempty"`
	OrchestrationTools *bool    `json:"orchestrationTools,omitempty"`
	Instructions       string   `json:"instructions,omitempty"`
	Skills             []string `json:"skills,omitempty"`
	ChildPolicy        string   `json:"childPolicy,omitempty"`
	ChildProfiles      []string `json:"childProfiles,omitempty"`
}

// loadContributions reads the skill directories and profile files the
// manifest declares from root (the plugin's directory, or a linked source) and
// validates them. Every problem names its manifest path and the fix. It
// never reads outside root: a symlink anywhere fails.
func (m *Manifest) loadContributions(root string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	v := &manifestVal{}
	m.skills, m.profiles = nil, nil

	seenSkill := map[string]bool{}
	for i, p := range m.Contributes.Skills {
		f := fmt.Sprintf("contributes.skills[%d]", i)
		name := p[strings.LastIndex(p, "/")+1:]
		if !skillNameRe.MatchString(name) || len(name) > 64 {
			v.bad(f, fmt.Sprintf("the skill directory name %q is not a valid skill name", name), "name the directory with lowercase letters, digits and single hyphens, like \"code-review\"")
			continue
		}
		if seenSkill[name] {
			v.bad(f, fmt.Sprintf("skill %q is declared twice", name), "keep one directory per skill name")
			continue
		}
		seenSkill[name] = true
		sk, err := readSkillDir(realRoot, p, name)
		if err != nil {
			v.bad(f, err.Error(), "give the directory a SKILL.md and only regular files")
			continue
		}
		m.skills = append(m.skills, sk)
	}

	seenProfile := map[string]bool{}
	for i, p := range m.Contributes.Profiles {
		f := fmt.Sprintf("contributes.profiles[%d]", i)
		raw, err := readConfinedFile(realRoot, p, maxProfileFileBytes)
		if err != nil {
			v.bad(f, err.Error(), "point it at a regular JSON file inside the plugin")
			continue
		}
		var ps ProfileSpec
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&ps); err != nil {
			v.bad(f, "is not a valid profile: "+shortErr(err), "write a JSON object with name, provider and the profile fields documented in the plugin docs")
			continue
		}
		if err := validateProfileSpec(m.ID, ps); err != nil {
			v.bad(f, err.Error(), "fix the profile file")
			continue
		}
		if seenProfile[ps.Name] {
			v.bad(f, fmt.Sprintf("profile %q is declared twice", ps.Name), "keep one file per profile name")
			continue
		}
		seenProfile[ps.Name] = true
		m.profiles = append(m.profiles, ps)
	}
	for i, ps := range m.profiles {
		for _, c := range ps.ChildProfiles {
			if !seenProfile[c] || c == ps.Name {
				v.bad(fmt.Sprintf("contributes.profiles[%d]", i), fmt.Sprintf("childProfiles names %q, which is not another profile of this plugin", c), "list the names of this plugin's other profiles")
			}
		}
	}
	if len(v.errs) > 0 {
		return &ManifestError{Errs: v.errs}
	}
	sort.Slice(m.skills, func(i, j int) bool { return m.skills[i].Name < m.skills[j].Name })
	sort.Slice(m.profiles, func(i, j int) bool { return m.profiles[i].Name < m.profiles[j].Name })
	return nil
}

func validateProfileSpec(pluginID string, p ProfileSpec) error {
	if !nameRe.MatchString(p.Name) {
		return fmt.Errorf("name %q is not a valid profile name (1-64 letters, digits, . _ -)", p.Name)
	}
	if strings.TrimSpace(p.Provider) == "" {
		return fmt.Errorf("profile %q needs a provider", p.Name)
	}
	if len(p.Instructions) > 16<<10 {
		return fmt.Errorf("profile %q instructions are over 16 KiB", p.Name)
	}
	if len(p.Notes) > 2<<10 {
		return fmt.Errorf("profile %q notes are over 2 KiB", p.Name)
	}
	switch p.OnRateLimit {
	case "", "none", "wait":
	default:
		return fmt.Errorf("profile %q onRateLimit %q is not none or wait (fallback needs a stored profile id)", p.Name, p.OnRateLimit)
	}
	switch p.ChildPolicy {
	case "", "none", "leaf", "profiles":
	default:
		return fmt.Errorf("profile %q childPolicy %q is not none, leaf or profiles", p.Name, p.ChildPolicy)
	}
	if p.ChildPolicy == "profiles" && len(p.ChildProfiles) == 0 {
		return fmt.Errorf("profile %q: childPolicy profiles needs childProfiles", p.Name)
	}
	if p.ChildPolicy != "profiles" && len(p.ChildProfiles) > 0 {
		return fmt.Errorf("profile %q: childProfiles needs childPolicy profiles", p.Name)
	}
	seen := map[string]bool{}
	for _, s := range p.Skills {
		if !profileSkillR.MatchString(s) || seen[s] {
			return fmt.Errorf("profile %q skill %q is not a skill name (use \"name\" or \"plugin:name\", once each)", p.Name, s)
		}
		seen[s] = true
	}
	return nil
}

// readConfinedFile reads the regular file at rel under realRoot, refusing a
// symlink at any step and a file over limit.
func readConfinedFile(realRoot, rel string, limit int64) ([]byte, error) {
	full := filepath.Join(realRoot, filepath.FromSlash(rel))
	if err := noSymlinkUnder(realRoot, full); err != nil {
		return nil, err
	}
	st, err := os.Lstat(full)
	if err != nil {
		return nil, fmt.Errorf("%s does not exist in the plugin", rel)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", rel)
	}
	if st.Size() > limit {
		return nil, fmt.Errorf("%s is %d bytes, over the %d byte cap", rel, st.Size(), limit)
	}
	return os.ReadFile(full)
}

// noSymlinkUnder fails when any component of full below realRoot is a symlink.
func noSymlinkUnder(realRoot, full string) error {
	rel, err := filepath.Rel(realRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes the plugin")
	}
	cur := realRoot
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, seg)
		st, err := os.Lstat(cur)
		if err != nil {
			return nil // missing is reported by the caller
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link", filepath.ToSlash(rel))
		}
	}
	return nil
}

func readSkillDir(realRoot, rel, name string) (SkillSpec, error) {
	dir := filepath.Join(realRoot, filepath.FromSlash(rel))
	if err := noSymlinkUnder(realRoot, dir); err != nil {
		return SkillSpec{}, err
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return SkillSpec{}, fmt.Errorf("%s is not a directory", rel)
	}
	sk := SkillSpec{Name: name, Files: map[string][]byte{}}
	var total int64
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxSkillFileBytes {
			return fmt.Errorf("a file in %s is over the %d byte cap", rel, maxSkillFileBytes)
		}
		total += info.Size()
		if len(sk.Files) >= maxSkillFiles || total > maxSkillTotalBytes {
			return fmt.Errorf("%s has more than %d files or %d bytes", rel, maxSkillFiles, maxSkillTotalBytes)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(dir, p)
		sk.Files[filepath.ToSlash(r)] = b
		return nil
	})
	if err != nil {
		return SkillSpec{}, err
	}
	body, ok := sk.Files["SKILL.md"]
	if !ok || len(bytes.TrimSpace(body)) == 0 {
		return SkillSpec{}, fmt.Errorf("%s has no SKILL.md", rel)
	}
	sk.Description = skillDescription(string(body))
	return sk, nil
}

// skillDescription returns the one-line description from a SKILL.md
// frontmatter block, or "".
func skillDescription(md string) string {
	md = strings.ReplaceAll(strings.TrimPrefix(md, "\xef\xbb\xbf"), "\r\n", "\n")
	rest, ok := strings.CutPrefix(md, "---\n")
	if !ok {
		return ""
	}
	for {
		line, after, more := strings.Cut(rest, "\n")
		if strings.TrimRight(line, " \t") == "---" {
			return ""
		}
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			return v
		}
		if !more {
			return ""
		}
		rest = after
	}
}
