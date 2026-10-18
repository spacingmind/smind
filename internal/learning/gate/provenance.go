package gate

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

var validEvidenceKinds = map[string]bool{
	EvUserMessage:      true,
	EvFailureRecovery:  true,
	EvPermissionDenied: true,
	EvLoadedSkillWrong: true,
}

// checkProvenance enforces the provenance rule (ADR-0026 item 9.3 / AC13):
// in reflect mode every intent must cite 1..3 evidence items of qualifying
// kinds whose quotes occur verbatim in the referenced window events. Curator
// intents skip provenance (they reconcile already-approved skills).
//
// proposed holds the text pieces the intent would persist or change (create:
// description and body; patch: description and new_string). Every code span
// in them must occur in the full text of a cited, verified evidence event
// (ReasonUngroundedContent), and each quote must be at least 12 runes and 3
// words long and appear word-aligned in its event.
func checkProvenance(in Intent, w Window, mode Mode, proposed []string) []Finding {
	if mode == ModeCurate {
		return nil
	}

	seen := make(map[EventKey]map[string]struct{})
	var items []Evidence
	for _, ev := range in.Evidence {
		k := EventKey{RunID: ev.RunID, Seq: ev.Seq}
		if seen[k] == nil {
			seen[k] = make(map[string]struct{})
		}
		if _, dup := seen[k][ev.Quote]; dup {
			continue
		}
		seen[k][ev.Quote] = struct{}{}
		items = append(items, ev)
	}

	if len(items) < 1 || len(items) > 3 {
		return []Finding{{Reason: ReasonNoProvenance, Detail: fmt.Sprintf("intent cites %d deduped evidence items, need 1..3", len(items))}}
	}

	var out []Finding
	var verified []string
	for _, ev := range items {
		if !validEvidenceKinds[ev.Kind] {
			out = append(out, Finding{Reason: ReasonBadEvidence, Detail: fmt.Sprintf("evidence kind %q cannot justify a skill", ev.Kind)})
			continue
		}
		q := strings.TrimSpace(ev.Quote)
		if n := utf8.RuneCountInString(q); n < minQuoteRunes || n > 500 || len(strings.Fields(q)) < minQuoteWords {
			out = append(out, Finding{Reason: ReasonBadEvidence, Detail: fmt.Sprintf("quote is %d runes, need %d..500 and at least %d words", n, minQuoteRunes, minQuoteWords)})
			continue
		}
		event, ok := w[EventKey{RunID: ev.RunID, Seq: ev.Seq}]
		if !ok {
			out = append(out, Finding{Reason: ReasonEvidenceNotVerbatim, Detail: "event not in window"})
			continue
		}
		if event.Kind != ev.Kind {
			out = append(out, Finding{Reason: ReasonBadEvidence, Detail: fmt.Sprintf("event kind %q does not match claimed kind %q", event.Kind, ev.Kind)})
			continue
		}
		if !containsAligned(event.Text, ev.Quote) {
			out = append(out, Finding{Reason: ReasonEvidenceNotVerbatim, Detail: "quote not found verbatim and word-aligned in the event"})
			continue
		}
		verified = append(verified, collapseSpace(event.Text))
	}
	if len(out) > 0 {
		return out
	}
	for _, span := range codeSpans(proposed) {
		if !spanInAny(span, verified) {
			out = append(out, finding(ReasonUngroundedContent, "code span not in cited evidence: %q", span))
		}
	}
	return out
}

const (
	minQuoteRunes = 12
	minQuoteWords = 3
	minSpanRunes  = 3
)

func isASCIIAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// containsAligned reports whether quote occurs verbatim in text at a word
// boundary: when the quote starts (ends) with an ASCII letter or digit, the
// byte before (after) the occurrence must not be one. Overlapping
// occurrences are considered.
func containsAligned(text, quote string) bool {
	if quote == "" {
		return false
	}
	for off := 0; off <= len(text)-len(quote); {
		i := strings.Index(text[off:], quote)
		if i < 0 {
			return false
		}
		i += off
		end := i + len(quote)
		startOK := i == 0 || !isASCIIAlnum(quote[0]) || !isASCIIAlnum(text[i-1])
		endOK := end == len(text) || !isASCIIAlnum(quote[len(quote)-1]) || !isASCIIAlnum(text[end])
		if startOK && endOK {
			return true
		}
		off = i + 1
	}
	return false
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func spanInAny(span string, collapsedEvents []string) bool {
	for _, e := range collapsedEvents {
		if strings.Contains(e, span) {
			return true
		}
	}
	return false
}

// codeSpans returns the whitespace-collapsed markdown inline code spans and
// fenced-code lines of the texts that are at least minSpanRunes runes long.
// An unclosed fence runs to the end of the text.
func codeSpans(texts []string) []string {
	var out []string
	add := func(s string) {
		s = collapseSpace(s)
		if utf8.RuneCountInString(s) >= minSpanRunes {
			out = append(out, s)
		}
	}
	for _, t := range texts {
		inFence := false
		for _, ln := range strings.Split(normalizeNewlines(t), "\n") {
			trimmed := strings.TrimSpace(ln)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = !inFence
				continue
			}
			if inFence {
				add(ln)
				continue
			}
			for _, sp := range inlineSpans(ln) {
				add(sp)
			}
		}
	}
	return out
}

// inlineSpans extracts `code` spans of any backtick-run length from a line.
// An unmatched opening run is treated as code up to the end of the line.
func inlineSpans(ln string) []string {
	var out []string
	for i := 0; i < len(ln); {
		if ln[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(ln) && ln[j] == '`' {
			j++
		}
		n := j - i
		rest := ln[j:]
		end := -1
		for k := 0; k < len(rest); {
			if rest[k] != '`' {
				k++
				continue
			}
			m := k
			for m < len(rest) && rest[m] == '`' {
				m++
			}
			if m-k == n {
				end = k
				break
			}
			k = m
		}
		if end < 0 {
			out = append(out, rest)
			break
		}
		out = append(out, rest[:end])
		i = j + end + n
	}
	return out
}
