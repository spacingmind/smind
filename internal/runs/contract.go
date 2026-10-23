package runs

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/spacingmind/smind/internal/skills"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

var (
	bundleOnce   sync.Once
	bundleCached []skills.Skill
	bundleErr    error
)

// cachedBundle reads the embedded skill bundle once per process.
func cachedBundle() ([]skills.Skill, error) {
	bundleOnce.Do(func() { bundleCached, bundleErr = skills.Bundle() })
	return bundleCached, bundleErr
}

// buildInstructions renders a profile's role text (ADR-0041 S2a): its
// Instructions, then each bundled skill named in Skills (in order) under a
// "## Skill: <name>" header with its SKILL.md frontmatter removed. An
// unknown skill name is skipped with one log line (profile validation
// already rejects unknown names; only a binary downgrade can lose one). An
// empty result means no contract. The text itself is never logged.
func buildInstructions(p store.AgentProfile) (string, error) {
	bundle, err := cachedBundle()
	if err != nil {
		return "", err
	}
	return buildInstructionsFrom(p, bundle), nil
}

func buildInstructionsFrom(p store.AgentProfile, bundle []skills.Skill) string {
	var b strings.Builder
	b.WriteString(p.Instructions)
	for _, name := range p.Skills {
		var body []byte
		found := false
		for _, s := range bundle {
			if s.Name == name {
				body, found = s.Files["SKILL.md"]
				break
			}
		}
		if !found {
			slog.Warn("runs: profile names a skill missing from this binary; skipped", "profile", p.ID, "skill", name)
			continue
		}
		b.WriteString("\n\n## Skill: " + name + "\n\n")
		b.WriteString(strings.TrimSpace(stripFrontmatter(string(body))))
	}
	return strings.TrimSpace(b.String())
}

// stripFrontmatter removes a leading "---" ... "---" YAML block.
func stripFrontmatter(s string) string {
	s = strings.ReplaceAll(strings.TrimPrefix(s, "\xef\xbb\xbf"), "\r\n", "\n")
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return s
	}
	for {
		line, after, more := strings.Cut(rest, "\n")
		if strings.TrimRight(line, " \t") == "---" {
			return after
		}
		if !more {
			return s // unterminated block: not frontmatter
		}
		rest = after
	}
}

// withRunContract attaches the run's seat contract to ctx: the role text of
// the agent profile the run was launched with. System runs (purpose set),
// runs without a profile, a missing profile and an empty text add nothing.
// Nothing about the text is logged.
func (reg *Registry) withRunContract(ctx context.Context, r *run) context.Context {
	if r.profileID == 0 || r.purpose != "" {
		return ctx
	}
	p, err := reg.st.GetAgentProfile(r.profileID)
	if err != nil {
		slog.Warn("runs: run contract skipped", "run", r.id, "profile", r.profileID, "err", err)
		return ctx
	}
	text, err := buildInstructions(p)
	if err != nil {
		slog.Warn("runs: run contract skipped", "run", r.id, "profile", r.profileID, "err", err)
		return ctx
	}
	if text == "" {
		return ctx
	}
	return taskrunner.WithRunContract(ctx, taskrunner.RunContract{Instructions: text})
}
