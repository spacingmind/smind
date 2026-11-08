package runs

import (
	"context"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// ForkAnchor is what the daemon needs to fork a chat natively at a finished turn
// (ADR-0057): the provider session that turn ran in (not necessarily the chat's
// current one: a failed resume starts a new session) and the transcript entry to
// resume it at.
type ForkAnchor struct {
	SessionID string
	EntryUUID string
	Reason    native.Reason // set when the fork is refused
	Detail    string
}

// OK reports whether the anchor can be forked from.
func (a ForkAnchor) OK() bool { return a.Reason == native.ReasonNone }

// ForkAnchorOf resolves the native fork anchor of runID through the session
// reader, which is human-only, bounded and self-checking. Only the Claude Code
// harness forks natively; Codex waits on a live probe of its thread API, and
// every other harness has no fork. A refusal is a Reason, never an error: the
// caller offers the seeded fallback.
func (reg *Registry) ForkAnchorOf(ctx context.Context, runID string) ForkAnchor {
	rb, ref, refused, ok := reg.rebuildTarget(runID, RequesterHuman)
	if !ok {
		return ForkAnchor{Reason: refused.Reason, Detail: refused.Detail}
	}
	switch ref.Harness {
	case native.HarnessClaudeCode:
	case native.HarnessCodex:
		return ForkAnchor{Reason: native.ReasonForkUnsupported, Detail: "waiting on the Codex probe"}
	default:
		return ForkAnchor{Reason: native.ReasonForkUnsupported, Detail: "this agent has no session fork"}
	}
	res := rb.Rebuild(ctx, native.Request{Ref: ref, Requester: RequesterHuman, ExpectedToolCalls: reg.storedToolCalls(runID)})
	if !res.OK() {
		return ForkAnchor{Reason: res.Reason, Detail: res.Detail}
	}
	if res.Anchor == "" || !taskrunner.ValidForkEntry(res.Anchor) {
		return ForkAnchor{Reason: native.ReasonFormatDrift, Detail: "the transcript records carry no usable entry uuid"}
	}
	return ForkAnchor{SessionID: ref.SessionID, EntryUUID: res.Anchor}
}

// Caps of the seeded fork's context (ADR-0057 §2): the earlier turns' prompts and
// final texts, newest kept, bounded.
const (
	forkSeedTurnTextMax = 1500
	forkSeedMax         = 12 << 10
)

// ForkSeed builds the context a seeded fork prepends to its first prompt, from
// the stored prompts and final texts of the chat's runs up to and including
// anchor (oldest first). It never reads a provider session. The newest turns are
// kept when the cap bites. "" when there is nothing to say.
func ForkSeed(runs []store.Run, finalText func(runID string) string) string {
	type turn struct{ prompt, answer string }
	var turns []turn
	for _, r := range runs {
		p := strings.TrimSpace(r.Prompt)
		a := strings.TrimSpace(finalText(r.ID))
		if p == "" && a == "" {
			continue
		}
		turns = append(turns, turn{neutralise(clip(p, forkSeedTurnTextMax)), neutralise(clip(a, forkSeedTurnTextMax))})
	}
	if len(turns) == 0 {
		return ""
	}
	var parts []string
	size := 0
	dropped := 0
	for i := len(turns) - 1; i >= 0; i-- {
		part := fmt.Sprintf("User: %s\nAssistant: %s", turns[i].prompt, turns[i].answer)
		if size+len(part) > forkSeedMax {
			dropped = i + 1
			break
		}
		parts = append([]string{part}, parts...)
		size += len(part)
	}
	var b strings.Builder
	b.WriteString("This conversation continues one that was forked from an earlier chat. The earlier turns follow, condensed: tool calls, file contents and reasoning are not available to you, only what was said.\n\n")
	if dropped > 0 {
		fmt.Fprintf(&b, "(%d earlier turn(s) left out.)\n\n", dropped)
	}
	b.WriteString(strings.Join(parts, "\n\n"))
	b.WriteString("\n\n" + forkSeedEnd + "\n\n")
	return b.String()
}

// forkSeedEnd is the line that closes the seed. A turn's own text cannot carry it:
// neutralise rewrites the dash run that opens it, so a stored prompt or answer
// cannot fake the boundary and make the rest of the seed read as the new message.
const forkSeedEnd = "--- end of the earlier turns; the user's new message follows ---"

func neutralise(s string) string {
	return strings.ReplaceAll(s, "---", "- - -")
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + " ..."
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
