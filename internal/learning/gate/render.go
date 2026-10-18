package gate

import (
	"fmt"
	"strings"
)

// Render emits the full SKILL.md text: frontmatter from the gate's own
// fields (never the model's), then the body. The description and category are
// always double-quoted with `"` and `\` escaped so YAML metacharacters can't leak.
func Render(s Skill) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: %q\n", s.Name, s.Description)
	if s.Category != "" {
		fmt.Fprintf(&b, "category: %q\n", s.Category)
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(s.Body))
	b.WriteString("\n")
	return b.String()
}
