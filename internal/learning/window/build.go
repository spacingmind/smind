package window

import (
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const (
	truncationMarker = "[... earlier records truncated ..." + "]"
	maxIndexEntries  = 100
	maxPriorTurns    = 10
	indexDescBytes   = 200
	priorTurnBytes   = 200
	runErrorBytes    = 200
	contradictBytes  = 1024
)

func (c Caps) withDefaults() Caps {
	d := CapsFromConfig(config.DefaultLearning())
	if c.RecordBytes <= 0 {
		c.RecordBytes = d.RecordBytes
	}
	if c.InputBytes <= 0 {
		c.InputBytes = d.InputBytes
	}
	if c.ResultBytes <= 0 {
		c.ResultBytes = d.ResultBytes
	}
	if c.FailureResultBytes <= 0 {
		c.FailureResultBytes = d.FailureResultBytes
	}
	if c.WindowBytes <= 0 {
		c.WindowBytes = d.WindowBytes
	}
	return c
}

// failingOutcome reports whether a run ended badly; unknown strings are not
// failures.
func failingOutcome(run Run) bool {
	switch strings.ToLower(run.Status) {
	case "failed", "cancelled", "canceled", "error":
		return true
	}
	switch strings.ToLower(run.StopReason) {
	case "error", "cancelled", "canceled", "refusal", "max_tokens", "max_turn_requests":
		return true
	}
	return false
}

type runBlock struct {
	header string
	recs   []record
}

// Build renders the window. Without a Redact function it fails closed and
// returns an empty Output rather than render unredacted text.
func Build(in Input) Output {
	if in.Redact == nil {
		return Output{}
	}
	r := renderer{redact: in.Redact, caps: in.Caps.withDefaults()}
	out := Output{Window: gate.Window{}}

	var blocks []runBlock
	loadedAll := map[string]struct{}{}
	prevFailed := false
	for _, run := range in.Runs {
		out.ToolCalls += countRunToolCalls(run)
		for _, ev := range run.Events {
			if ev.ID > out.LastEventID {
				out.LastEventID = ev.ID
			}
		}
		loaded := r.loadedNames(in.LoadedByRun[run.ID])
		for _, n := range loaded {
			loadedAll[n] = struct{}{}
		}
		correction := run.Origin == OriginHuman && (run.Interrupt || prevFailed)
		prevFailed = failingOutcome(run)

		var recs []record
		if rc, ok := r.promptRecord(run, correction); ok {
			recs = append(recs, rc)
			if correction && len(loaded) > 0 {
				names := strings.Join(loaded, ", ")
				const prefix = "[LOADED-SKILL-CONTRADICTED]"
				body := r.fitBody(prefix, names+" :: "+clipRunes(rc.body, contradictBytes))
				recs = append(recs, record{prefix: prefix, body: body, key: gate.EventKey{RunID: run.ID, Seq: seqLoadedSkill}, gated: true, kind: gate.EvLoadedSkillWrong})
			}
		}
		recs = append(recs, r.runRecords(run)...)
		if run.ErrMsg != "" {
			if body := r.line(run.ErrMsg, runErrorBytes); body != "" {
				recs = append(recs, record{prefix: "[RUN-ERROR]", body: body})
			}
		}
		if len(recs) == 0 {
			continue
		}
		blocks = append(blocks, runBlock{
			header: "## run " + r.line(run.ID, 128) + " (" + r.line(run.Provider, 64) + ", " + r.line(run.Status, 64) + ", stop=" + r.line(run.StopReason, 64) + ")",
			recs:   recs,
		})
	}

	loadedSection := ""
	if len(loadedAll) > 0 {
		names := make([]string, 0, len(loadedAll))
		for n := range loadedAll {
			names = append(names, n)
		}
		sort.Strings(names)
		loadedSection = "## learned skills loaded in this window\n- " + strings.Join(names, "\n- ") + "\n\n"
	}

	// Degrade the fixed parts only if they alone would blow the cap.
	var fixed []string
	for level := 0; level < 3; level++ {
		fixed = r.fixedParts(in, loadedSection, level)
		if len(fixed[0])+len(fixed[1])+len(fixed[2]) <= r.caps.WindowBytes {
			break
		}
	}
	budget := r.caps.WindowBytes - len(fixed[0]) - len(fixed[1]) - len(fixed[2])
	if budget < 0 {
		budget = 0
	}
	body, kept, truncated := selectTail(blocks, budget)
	out.Truncated = truncated
	out.Text = fixed[0] + fixed[1] + body + fixed[2]
	for bi, b := range blocks {
		for k := kept[bi]; k < len(b.recs); k++ {
			if rc := b.recs[k]; rc.gated {
				out.Window[rc.key] = gate.WindowEvent{Kind: rc.kind, Text: rc.body}
			}
		}
	}
	return out
}

// selectTail keeps the newest whole records that fit budget. It returns the
// rendered runs section, per-block first kept record index (len(recs) when a
// block is dropped), and whether anything was dropped.
func selectTail(blocks []runBlock, budget int) (string, []int, bool) {
	kept := make([]int, len(blocks))
	total := 0
	for i, b := range blocks {
		kept[i] = 0
		total += len(b.header) + 1
		for _, rc := range b.recs {
			total += len(rc.line()) + 1
		}
	}
	truncated := total > budget
	if truncated {
		budget -= len(truncationMarker) + 1
		used := 0
		for i := range kept {
			kept[i] = len(blocks[i].recs)
		}
	outer:
		for bi := len(blocks) - 1; bi >= 0; bi-- {
			b := blocks[bi]
			for k := len(b.recs) - 1; k >= 0; k-- {
				cost := len(b.recs[k].line()) + 1
				if kept[bi] == len(b.recs) {
					cost += len(b.header) + 1
				}
				if used+cost > budget {
					break outer
				}
				used += cost
				kept[bi] = k
			}
		}
	}
	var sb strings.Builder
	if truncated {
		sb.WriteString(truncationMarker + "\n")
	}
	for bi, b := range blocks {
		if kept[bi] >= len(b.recs) {
			continue
		}
		sb.WriteString(b.header + "\n")
		for _, rc := range b.recs[kept[bi]:] {
			sb.WriteString(rc.line() + "\n")
		}
	}
	return sb.String(), kept, truncated
}

// fixedParts returns the skill index block, the transcript opening (loaded
// skills, background turns) and the transcript closing. level 1 drops the
// background turns, level 2 also empties the skill index.
func (r renderer) fixedParts(in Input, loadedSection string, level int) []string {
	var idx strings.Builder
	idx.WriteString("<<<EXISTING_SKILLS\n## existing learned skills\n")
	if level < 2 {
		r.writeIndex(&idx, in.Learned)
	}
	idx.WriteString("\n## existing human skills\n")
	if level < 2 {
		r.writeIndex(&idx, in.Human)
	}
	idx.WriteString("EXISTING_SKILLS>>>\n\n")

	var head strings.Builder
	head.WriteString("<<<UNTRUSTED_TRANSCRIPT\n")
	head.WriteString(loadedSection)
	if level < 1 {
		turns := in.PriorUserTurns
		if len(turns) > maxPriorTurns {
			turns = turns[len(turns)-maxPriorTurns:]
		}
		var lines []string
		for _, t := range turns {
			if l := r.line(t, priorTurnBytes); l != "" {
				lines = append(lines, "- "+l)
			}
		}
		if len(lines) > 0 {
			head.WriteString("## earlier user turns (background, NOT evidence)\n" + strings.Join(lines, "\n") + "\n\n")
		}
	}
	return []string{idx.String(), head.String(), "UNTRUSTED_TRANSCRIPT>>>\n"}
}

func (r renderer) writeIndex(sb *strings.Builder, skills []SkillRef) {
	type entry struct{ name, line string }
	var es []entry
	for _, s := range skills {
		n := r.line(s.Name, 128)
		if n == "" {
			continue
		}
		es = append(es, entry{n, "- " + n + ": " + r.line(s.Description, indexDescBytes)})
	}
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].name != es[j].name {
			return es[i].name < es[j].name
		}
		return es[i].line < es[j].line
	})
	if len(es) > maxIndexEntries {
		es = es[:maxIndexEntries]
	}
	for _, e := range es {
		sb.WriteString(e.line + "\n")
	}
}

// loadedNames returns the run's loaded skill names, rendered, sorted, deduped.
func (r renderer) loadedNames(names []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, n := range names {
		n = r.line(n, 128)
		if _, dup := seen[n]; n == "" || dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func countRunToolCalls(run Run) int {
	evs := make([]taskrunner.Event, 0, len(run.Events))
	for _, e := range run.Events {
		evs = append(evs, e.E)
	}
	return CountToolCalls(evs)
}
