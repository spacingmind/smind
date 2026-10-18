package reflector

import (
	_ "embed"
	"sort"
	"strings"
	"unicode/utf8"
)

//go:embed prompts/reflector.md
var reflectorPrompt string

//go:embed prompts/curator.md
var curatorPrompt string

// ReflectorPrompt is the reflector system prompt.
func ReflectorPrompt() string { return reflectorPrompt }

// CuratorPrompt is the curator system prompt.
func CuratorPrompt() string { return curatorPrompt }

const (
	maxRejected      = 10
	rejectedFieldCap = 200
)

// RejectedExample is a proposal the gate or a human rejected earlier, shown
// back to the reflector so it does not repeat it.
type RejectedExample struct {
	Name, Description, Note string
}

// SkillBody is one learned skill as the curator sees it.
type SkillBody struct {
	Name, Category, Description, Body string
}

// neutralise breaks the wrapper delimiters so data cannot close or reopen a
// block (same rewrite as the window builder).
func neutralise(s string) string {
	s = strings.ReplaceAll(s, "<<<", "< < <")
	return strings.ReplaceAll(s, ">>>", "> > >")
}

// clipBytes cuts s to at most n bytes on a rune boundary.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// oneLine collapses whitespace, neutralises delimiters and clips to n bytes.
func oneLine(s string, n int) string {
	return clipBytes(neutralise(strings.Join(strings.Fields(s), " ")), n)
}

// ComposeUserPrompt builds the reflector user prompt. windowText already
// carries the window builder's delimiters; previously rejected proposals are
// appended (at most 10, each field clipped) so the model does not repeat them.
func ComposeUserPrompt(windowText string, rejected []RejectedExample) string {
	var sb strings.Builder
	sb.WriteString(windowText)
	if !strings.HasSuffix(windowText, "\n") {
		sb.WriteByte('\n')
	}
	if len(rejected) > maxRejected {
		rejected = rejected[:maxRejected]
	}
	if len(rejected) > 0 {
		sb.WriteString("\n## previously rejected proposals (do not repeat)\n")
		for _, r := range rejected {
			sb.WriteString("- " + oneLine(r.Name, rejectedFieldCap) + " :: " +
				oneLine(r.Description, rejectedFieldCap) + " :: " + oneLine(r.Note, rejectedFieldCap) + "\n")
		}
	}
	sb.WriteString("\nReply with the JSON object only.\n")
	return sb.String()
}

// ComposeCuratorPrompt builds the curator user prompt: every skill, in name
// order, inside one LEARNED_SKILLS block with delimiters neutralised.
func ComposeCuratorPrompt(skills []SkillBody) string {
	sorted := append([]SkillBody(nil), skills...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var sb strings.Builder
	sb.WriteString("<<<LEARNED_SKILLS\n")
	for _, s := range sorted {
		sb.WriteString("### " + oneLine(s.Name, 128) + "\n")
		sb.WriteString("category: " + oneLine(s.Category, 64) + "\n")
		sb.WriteString("description: " + oneLine(s.Description, 400) + "\n\n")
		sb.WriteString(strings.TrimRight(neutralise(s.Body), "\n") + "\n\n")
	}
	sb.WriteString("LEARNED_SKILLS>>>\n\nReply with the JSON object only.\n")
	return sb.String()
}
