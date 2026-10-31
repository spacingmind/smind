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
	"unicode"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/fsutil"
	"github.com/spacingmind/smind/internal/plugins/uiview"
)

// Caps on what a plugin's data contributions may weigh; they bound what a
// plugin can put into an agent's prompt and what the projection copies.
const (
	maxProfileFileBytes = 64 << 10
	maxSkillFileBytes   = 128 << 10
	maxSkillTotalBytes  = 256 << 10
	maxSkillFiles       = 32
	maxPromptFileBytes  = 16 << 10
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
	m.skills, m.profiles, m.prompts, m.themes = nil, nil, nil, nil
	m.panelViews = nil

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
	m.loadPrompts(realRoot, v)
	m.loadThemes(realRoot, v)
	m.loadPanels(realRoot, v)
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
		if fsutil.IsLink(st.Mode()) {
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

// loadPrompts reads the prompt text of every prompt command. A prompt command
// inserts text into the composer and nothing else: the file must be a regular,
// non-empty UTF-8 text file with no NUL, under maxPromptFileBytes.
func (m *Manifest) loadPrompts(realRoot string, v *manifestVal) {
	seen := map[string]bool{}
	for i, c := range m.Contributes.Commands {
		f := fmt.Sprintf("contributes.commands[%d]", i)
		if seen[c.Name] {
			v.bad(f+".name", fmt.Sprintf("command %q is declared twice", c.Name), "keep one entry per command name")
			continue
		}
		seen[c.Name] = true
		if c.Prompt == "" {
			continue
		}
		raw, err := readConfinedFile(realRoot, c.Prompt, maxPromptFileBytes)
		if err != nil {
			v.bad(f+".prompt", err.Error(), "point it at a regular text file inside the plugin")
			continue
		}
		if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
			v.bad(f+".prompt", c.Prompt+" is not UTF-8 text", "save the prompt as plain UTF-8 text")
			continue
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			v.bad(f+".prompt", c.Prompt+" is empty", "write the text the command should insert")
			continue
		}
		if m.prompts == nil {
			m.prompts = map[string]string{}
		}
		m.prompts[c.Name] = string(raw)
	}
}

// loadThemes reads and validates every theme file: complete token sets per
// declared scheme, values in the safe grammar (see theme.go).
func (m *Manifest) loadThemes(realRoot string, v *manifestVal) {
	seen := map[string]bool{}
	for i, t := range m.Contributes.Themes {
		f := fmt.Sprintf("contributes.themes[%d]", i)
		if seen[t.ID] {
			v.bad(f+".id", fmt.Sprintf("theme %q is declared twice", t.ID), "keep one entry per theme id")
			continue
		}
		seen[t.ID] = true
		label := t.Label
		if label == "" {
			label = t.ID
		}
		if n := utf8.RuneCountInString(label); n > maxThemeLabelRunes {
			v.bad(f+".label", fmt.Sprintf("is %d characters, over the %d limit", n, maxThemeLabelRunes), "shorten the label")
			continue
		}
		if strings.IndexFunc(label, unicode.IsControl) >= 0 {
			v.bad(f+".label", "has a control character", "use plain text")
			continue
		}
		raw, err := readConfinedFile(realRoot, t.File, maxThemeFileBytes)
		if err != nil {
			v.bad(f+".file", err.Error(), "point it at a regular JSON file inside the plugin")
			continue
		}
		schemes, probs := parseThemeFile(raw)
		if len(probs) > 0 {
			for _, p := range probs {
				v.bad(f+".file", t.File+" "+p, "give every scheme the full token list (see the plugin docs)")
			}
			continue
		}
		m.themes = append(m.themes, ThemeSpec{ID: t.ID, Label: label, Schemes: schemes})
	}
}

// viewEnv is what the view sanitiser checks actions against: the rpc names and
// panel ids the manifest declares.
func viewEnv(m *Manifest) uiview.Env {
	env := uiview.Env{RPCs: map[string]bool{}, Panels: map[string]bool{}}
	for _, r := range m.Contributes.RPC {
		env.RPCs[r.Name] = true
	}
	for _, p := range m.Contributes.Panels {
		env.Panels[p.ID] = true
	}
	return env
}

// loadPanels reads and validates every file-backed panel view, so an invalid
// document fails the install instead of the first time a client opens it.
func (m *Manifest) loadPanels(realRoot string, v *manifestVal) {
	env := viewEnv(m)
	for i, p := range m.Contributes.Panels {
		if p.View.File == "" {
			continue
		}
		f := fmt.Sprintf("contributes.panels[%d].view.file", i)
		raw, err := readConfinedFile(realRoot, p.View.File, uiview.MaxDocBytes)
		if err != nil {
			v.bad(f, err.Error(), "point it at a regular JSON file inside the plugin")
			continue
		}
		out, err := uiview.Validate(raw, env)
		if err != nil {
			v.bad(f, err.Error(), "fix the view document (the error names the JSON path)")
			continue
		}
		if m.panelViews == nil {
			m.panelViews = map[string]json.RawMessage{}
		}
		m.panelViews[p.ID] = out
	}
}
