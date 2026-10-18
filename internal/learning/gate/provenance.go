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
func checkProvenance(in Intent, w Window, mode Mode) []Finding {
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
	for _, ev := range items {
		if !validEvidenceKinds[ev.Kind] {
			out = append(out, Finding{Reason: ReasonBadEvidence, Detail: fmt.Sprintf("evidence kind %q cannot justify a skill", ev.Kind)})
			continue
		}
		q := strings.TrimSpace(ev.Quote)
		if n := utf8.RuneCountInString(q); n < 8 || n > 500 {
			out = append(out, Finding{Reason: ReasonBadEvidence, Detail: fmt.Sprintf("quote is %d runes, need 8..500", n)})
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
		if !strings.Contains(event.Text, ev.Quote) {
			out = append(out, Finding{Reason: ReasonEvidenceNotVerbatim, Detail: "quote not found verbatim in the event"})
		}
	}
	return out
}
