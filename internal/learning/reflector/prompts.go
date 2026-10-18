package reflector

import (
	_ "embed"
	"fmt"
	"regexp"
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
	rejectedNoteCap  = 120

	maxCuratorSkills    = 60
	maxCuratorBodyBytes = 4 * 1024
	maxCuratorBytes     = 120 * 1024
	markerReserve       = 128 // room for the omitted-skills marker and closing delimiter
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

var delimRun = regexp.MustCompile("<{3,}|>{3,}")

// neutralise breaks the wrapper delimiters so data cannot close or reopen a
// block: every run of three or more < or > gets a space between each of its
// characters, so no three adjacent ones survive.
func neutralise(s string) string {
	return delimRun.ReplaceAllStringFunc(s, func(run string) string {
		parts := make([]string, 0, len(run))
		for _, r := range run {
			parts = append(parts, string(r))
		}
		return strings.Join(parts, " ")
	})
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
// appended inside their own REJECTED_PROPOSALS block (at most 10, each field
// clipped; omitted when empty) so the model does not repeat them. The block is
// untrusted data. A free-text note is dropped unless it is short and, after
// whitespace collapsing, a single line.
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
		sb.WriteString("\n<<<REJECTED_PROPOSALS\n")
		for _, r := range rejected {
			line := "- " + oneLine(r.Name, rejectedFieldCap) + " :: " + oneLine(r.Description, rejectedFieldCap)
			if note := rejectedNote(r.Note); note != "" {
				line += " :: " + note
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("REJECTED_PROPOSALS>>>\n")
	}
	sb.WriteString("\nReply with the JSON object only.\n")
	return sb.String()
}

// rejectedNote returns the note only when it is at most rejectedNoteCap bytes
// and a single line.
func rejectedNote(note string) string {
	if len(note) > rejectedNoteCap || strings.ContainsAny(strings.TrimSpace(note), "\r\n") {
		return ""
	}
	return oneLine(note, rejectedNoteCap)
}

// ComposeCuratorPrompt builds the curator user prompt: skills in name order
// inside one LEARNED_SKILLS block with delimiters neutralised. At most 60
// skills are shown, each body is clipped to 4 KB and the whole input stays
// under 120 KB; skills dropped to fit (always the last by name) are announced
// by a marker line.
func ComposeCuratorPrompt(skills []SkillBody) string {
	sorted := append([]SkillBody(nil), skills...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var sb strings.Builder
	sb.WriteString("<<<LEARNED_SKILLS\n")
	shown := 0
	for _, s := range sorted {
		var one strings.Builder
		one.WriteString("### " + oneLine(s.Name, 128) + "\n")
		one.WriteString("category: " + oneLine(s.Category, 64) + "\n")
		one.WriteString("description: " + oneLine(s.Description, 400) + "\n\n")
		one.WriteString(strings.TrimRight(clipBytes(neutralise(s.Body), maxCuratorBodyBytes), "\n") + "\n\n")
		if shown >= maxCuratorSkills || sb.Len()+one.Len() > maxCuratorBytes-markerReserve {
			break
		}
		sb.WriteString(one.String())
		shown++
	}
	if omitted := len(sorted) - shown; omitted > 0 {
		sb.WriteString(fmt.Sprintf("[... %d more skills omitted ...]\n\n", omitted))
	}
	sb.WriteString("LEARNED_SKILLS>>>\n\nReply with the JSON object only.\n")
	return sb.String()
}
