package projection

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// PrefaceSkill is one approved skill offered inline to an agent that cannot
// load an add-dir (ACP). LoadRate is loaded_runs/offered_runs, 0 if none.
type PrefaceSkill struct {
	Name, Description, Body string
	LoadRate                float64
}

const (
	prefaceOpen   = "<<<LEARNED_SKILLS\n"
	prefaceHeader = "Learned workspace skills (approved by a human; apply when relevant):\n"
	prefaceIndex  = "\nOther learned skills (names only):\n"
	prefaceClose  = "LEARNED_SKILLS>>>\n\n"
)

var delimRun = regexp.MustCompile("<{3,}|>{3,}")

// neutralise spaces apart every run of three or more `<` or `>` so skill text
// cannot forge or close the LEARNED_SKILLS markers.
func neutralise(s string) string {
	if !strings.Contains(s, "<<<") && !strings.Contains(s, ">>>") {
		return s
	}
	return delimRun.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Join(strings.Split(m, ""), " ")
	})
}

// Preface renders skills as a marker-wrapped block to prepend to a prompt,
// never longer than budgetBytes. It returns "" when there is nothing to say
// (no skills, or not even one line fits), so the caller prepends only when
// the result is non-empty and the prompt stays byte-identical otherwise.
//
// Skills are filled greedily by LoadRate descending (ties by name): each one
// whose full entry fits keeps its body; the rest become index-only lines,
// truncated with "[... N more ...]" if even those overflow.
func Preface(skills []PrefaceSkill, budgetBytes int) string {
	if len(skills) == 0 {
		return ""
	}
	type entry struct{ name, desc, full, line string }
	es := make([]entry, len(skills))
	for i, s := range skills {
		name := neutralise(strings.Join(strings.Fields(s.Name), " "))
		desc := neutralise(strings.Join(strings.Fields(s.Description), " "))
		body := neutralise(strings.TrimSpace(s.Body))
		es[i] = entry{
			name: name,
			desc: desc,
			full: fmt.Sprintf("\n### %s\n%s\n%s\n", name, desc, body),
			line: fmt.Sprintf("- %s: %s\n", name, desc),
		}
	}
	// Sort by rate desc, then name, then description for full determinism.
	idx := make([]int, len(es))
	for i := range idx {
		idx[i] = i
	}
	rate := func(i int) float64 {
		r := skills[i].LoadRate
		if math.IsNaN(r) {
			return 0
		}
		return r
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		if ra, rb := rate(ia), rate(ib); ra != rb {
			return ra > rb
		}
		if es[ia].name != es[ib].name {
			return es[ia].name < es[ib].name
		}
		return es[ia].full < es[ib].full
	})

	remaining := budgetBytes - len(prefaceOpen) - len(prefaceHeader) - len(prefaceClose)
	if remaining <= 0 {
		return ""
	}
	var body strings.Builder
	var rest []int
	for _, i := range idx {
		if len(es[i].full) <= remaining {
			body.WriteString(es[i].full)
			remaining -= len(es[i].full)
		} else {
			rest = append(rest, i)
		}
	}
	if len(rest) > 0 && len(prefaceIndex) < remaining {
		var lines strings.Builder
		room := remaining - len(prefaceIndex)
		shown := 0
		for k, i := range rest {
			more := len(rest) - k - 1
			// Keep room for a truncation marker unless this is the last line.
			need := len(es[i].line)
			if more > 0 {
				need += len(moreLine(more))
			}
			if need > room {
				break
			}
			lines.WriteString(es[i].line)
			room -= len(es[i].line)
			shown++
		}
		if left := len(rest) - shown; left > 0 {
			if m := moreLine(left); len(m) <= room {
				lines.WriteString(m)
			}
		}
		if lines.Len() > 0 {
			body.WriteString(prefaceIndex)
			body.WriteString(lines.String())
		}
	}
	if body.Len() == 0 {
		return ""
	}
	return prefaceOpen + prefaceHeader + body.String() + prefaceClose
}

func moreLine(n int) string { return fmt.Sprintf("[... %d more ...]\n", n) }
