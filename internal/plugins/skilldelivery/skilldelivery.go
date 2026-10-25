// Package skilldelivery hands the skills of active plugins to agent runs
// (ADR-0042 section 6): a read-only content-addressed projection that the
// claude CLI loads through an add-dir, an inline preface for the ACP
// providers, nothing for Codex. It never writes under ~/.claude/skills, a
// repository's .claude/skills or .agents/skills (ADR-0026 section 4).
package skilldelivery

import (
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// PrefaceBudgetBytes bounds the inline preface an ACP provider receives.
const PrefaceBudgetBytes = 16 << 10

// Delivery implements runs.PluginSkillSource over the plugin manager.
type Delivery struct {
	mgr  *plugins.Manager
	root string // <home>/plugin-projection
}

// New returns a Delivery that builds projections under root.
func New(mgr *plugins.Manager, root string) *Delivery { return &Delivery{mgr: mgr, root: root} }

// PluginSkillBody returns the SKILL.md of an active plugin's skill, or false
// when the plugin is not active (disabled, removed or the switch is off).
func (d *Delivery) PluginSkillBody(qualified string) (string, bool) {
	s, ok := d.mgr.PluginSkill(qualified)
	if !ok {
		return "", false
	}
	return string(s.Files["SKILL.md"]), true
}

// PluginSkillDelivery returns what a run on provider is offered: every active
// plugin skill, as a projection (claude), a preface (ACP) or nothing.
func (d *Delivery) PluginSkillDelivery(provider taskrunner.Provider) taskrunner.Delivery {
	skills := d.mgr.Skills()
	if len(skills) == 0 {
		return taskrunner.Delivery{}
	}
	switch providers.Delivery(string(provider)) {
	case providers.DeliveryProjection:
		files := map[string][]byte{}
		for _, s := range skills {
			base := path.Join(".claude", "skills", s.Dir)
			for rel, b := range s.Files {
				if rel == "SKILL.md" {
					b = []byte(RenderSkillMD(s))
				}
				files[path.Join(base, rel)] = b
			}
		}
		p, err := projection.ProjectFileSet(d.root, 0, files)
		if err != nil || p.Dir == "" {
			slog.Warn("plugin skills: projection failed; run proceeds without them", "err", err)
			return taskrunner.Delivery{}
		}
		return taskrunner.Delivery{AddDirs: []string{p.Dir}}
	case "":
		return taskrunner.Delivery{}
	}
	return taskrunner.Delivery{Preface: Preface(skills, PrefaceBudgetBytes)}
}

var frontmatterRe = regexp.MustCompile(`(?s)\A---\n.*?\n---[ \t]*\n?`)

// SkillBody is a SKILL.md without its frontmatter.
func SkillBody(md string) string {
	md = strings.ReplaceAll(strings.TrimPrefix(md, "\xef\xbb\xbf"), "\r\n", "\n")
	return strings.TrimSpace(frontmatterRe.ReplaceAllString(md, ""))
}

// RenderSkillMD is the SKILL.md a projection holds: the plugin's text under a
// frontmatter the host writes itself (name = the namespaced directory,
// description), so a plugin cannot smuggle other frontmatter keys such as
// tool allowances into the agent.
func RenderSkillMD(s plugins.Skill) string {
	desc := strings.Join(strings.Fields(s.Description), " ")
	if desc == "" {
		desc = "Skill " + s.Name + " from plugin " + s.Plugin
	}
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", s.Dir, yamlLine(desc), SkillBody(string(s.Files["SKILL.md"])))
}

// yamlLine quotes a one-line scalar.
func yamlLine(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

const (
	prefaceOpen  = "<<<PLUGIN_SKILLS\n"
	prefaceHead  = "Skills from plugins the user enabled (apply when relevant):\n"
	prefaceIndex = "\nOther plugin skills (names only):\n"
	prefaceClose = "PLUGIN_SKILLS>>>\n\n"
)

var delimRun = regexp.MustCompile("<{3,}|>{3,}")

// neutralise spaces apart runs of three or more < or > so skill text cannot
// forge or close the PLUGIN_SKILLS markers.
func neutralise(s string) string {
	return delimRun.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Join(strings.Split(m, ""), " ")
	})
}

// Preface renders skills as a marker-wrapped block under budget bytes: skills
// whose full text fits keep it, the rest become name-and-description lines.
func Preface(skills []plugins.Skill, budget int) string {
	sorted := append([]plugins.Skill(nil), skills...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Qualified < sorted[j].Qualified })
	remaining := budget - len(prefaceOpen) - len(prefaceHead) - len(prefaceClose)
	var full, index strings.Builder
	for _, s := range sorted {
		desc := neutralise(strings.Join(strings.Fields(s.Description), " "))
		entry := fmt.Sprintf("\n### %s\n%s\n%s\n", neutralise(s.Qualified), desc, neutralise(SkillBody(string(s.Files["SKILL.md"]))))
		line := fmt.Sprintf("- %s: %s\n", neutralise(s.Qualified), desc)
		switch {
		case len(entry) <= remaining:
			full.WriteString(entry)
			remaining -= len(entry)
		case len(line)+len(prefaceIndex) <= remaining:
			if index.Len() == 0 {
				remaining -= len(prefaceIndex)
			}
			index.WriteString(line)
			remaining -= len(line)
		}
	}
	if full.Len() == 0 && index.Len() == 0 {
		return ""
	}
	out := prefaceOpen + prefaceHead + full.String()
	if index.Len() > 0 {
		out += prefaceIndex + index.String()
	}
	return out + prefaceClose
}
