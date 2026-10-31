package runs

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/store"
)

// fallbackTailMax bounds the previous attempt's output quoted into a
// fallback successor's prompt.
const fallbackTailMax = 4096

// defaultRetryGrace is added to a rate limit's reset instant before the
// "wait" retry becomes deliverable, so the retry does not race the provider's
// own clock.
const defaultRetryGrace = 30 * time.Second

// retryPreface leads the prompt of an automatic "wait" retry: the failed
// attempt may already have changed the worktree.
const retryPreface = "(Retrying after a provider rate limit. Your earlier attempt may have made partial progress: check git status and git log, then continue.)\n\n"

// FallbackProfileFor returns the fallback profile id the given profile's
// author named (ADR-0041 F2), or 0 when the profile is unknown or has none.
func (reg *Registry) FallbackProfileFor(profileID int64) int64 {
	if profileID == 0 {
		return 0
	}
	p, err := reg.st.GetAgentProfile(profileID)
	if err != nil || p.FallbackProfileID == nil {
		return 0
	}
	return *p.FallbackProfileID
}

// applyRateLimitPolicy is ADR-0041 F2b: when a run launched with a profile
// fails on a provider rate limit, do what the profile's onRateLimit says.
// It acts at most once per chain (a retry is itself a run with failoverOf
// set, and never triggers another action), only for user runs, and only
// while the profile still exists.
func (reg *Registry) applyRateLimitPolicy(r *run, fail failure.Result) {
	if fail.Kind != "rate_limited" || r.profileID == 0 || r.failoverOf != "" || r.purpose != "" {
		return
	}
	prof, err := reg.st.GetAgentProfile(r.profileID)
	if err != nil {
		return
	}
	switch prof.OnRateLimit {
	case "wait":
		reg.enqueueRateLimitRetry(r, fail)
	case "fallback":
		reg.startFallbackSuccessor(r, prof)
	}
}

// enqueueRateLimitRetry puts one held item on the failed run's own chat,
// deliverable shortly after the limit resets. It goes straight to the store:
// that deliberately bypasses the non-human auto-approve check of
// StartWhenBusy, because it re-sends the SAME run's own settings and never
// escalates them. Attachments are not re-attached (out of scope).
func (reg *Registry) enqueueRateLimitRetry(r *run, fail failure.Result) {
	now := time.Now()
	if fail.ResetAt == nil || !fail.ResetAt.After(now) {
		return
	}
	notBefore := fail.ResetAt.Add(reg.retryGrace)
	perm := r.getPerm()
	cfg, err := json.Marshal(chatQueueRunConfig{
		Provider: string(r.provider), PermissionMode: perm.Mode, AutoAccept: perm.AutoAccept,
		ThinkingLevel: string(r.thinkingLevel), Model: r.model, ReplyChatID: r.replyChatID,
		ProfileID: r.profileID, FailoverOf: r.id,
	})
	if err != nil {
		return
	}
	prompt := retryPreface + stripProvenanceHeader(r.prompt)
	_, err = reg.st.EnqueueChatPromptAt(r.chatID, prompt, string(cfg), store.ChatQueueSourceOrchestrator, 0, 0, 0, "", &notBefore)
	switch {
	case errors.Is(err, store.ErrQueueFull):
		log.Printf("runs: rate-limit retry for run %s: chat %d queue full; giving up", r.id, r.chatID)
		return
	case err != nil:
		log.Printf("runs: rate-limit retry for run %s: %v", r.id, err)
		return
	}
	reg.notifyQueueUpdated(r.chatID)
	reg.armQueueWake()
}

// stripProvenanceHeader removes a leading "[message from task #N, chat #M]"
// line, which an immediate agent-sourced start stamps onto the run's prompt.
func stripProvenanceHeader(prompt string) string {
	if strings.HasPrefix(prompt, "[message from task #") {
		if line, rest, ok := strings.Cut(prompt, "\n"); ok && strings.HasSuffix(line, "]") {
			return rest
		}
	}
	return prompt
}

// failoverPreface leads a fallback successor's prompt.
func failoverPreface(oldProvider string) string {
	return "(The previous attempt on " + oldProvider + " stopped at a rate limit. The task's worktree holds its changes: inspect `git status` and `git log`, then continue the work below. Your earlier conversation is not available to you.)\n\n"
}

// tailBytes returns the last max bytes of s, cut on a rune boundary.
func tailBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	start := len(s) - max
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// startFallbackSuccessor is ADR-0041 F3: continue the failed run's work on
// the profile's fallback profile, in a NEW chat of the same task (a chat is
// bound to its provider, ADR-0016). It gives up with one log line on any
// problem. The fallback profile is human-authored, so its auto-approving
// mode is allowed: the item goes straight to the store, deliberately
// bypassing the non-human enqueue check, and only here. Holds no lock.
func (reg *Registry) startFallbackSuccessor(r *run, prof store.AgentProfile) {
	if prof.FallbackProfileID == nil || *prof.FallbackProfileID == 0 {
		return
	}
	fb, err := reg.st.GetAgentProfile(*prof.FallbackProfileID)
	if err != nil {
		log.Printf("runs: fallback for run %s: load profile %d: %v; giving up", r.id, *prof.FallbackProfileID, err)
		return
	}
	if fb.Provider == string(r.provider) {
		log.Printf("runs: fallback for run %s: profile %d uses the same provider %s; giving up", r.id, fb.ID, fb.Provider)
		return
	}
	if until, cooling := reg.activeProviderCooldown(fb.Provider); cooling {
		log.Printf("runs: fallback for run %s: provider %s is rate-limited until %s; giving up", r.id, fb.Provider, until.Format(time.RFC3339))
		return
	}
	wm, _, ok := reg.starterDeps()
	if !ok {
		log.Printf("runs: fallback for run %s: no starter wired; giving up", r.id)
		return
	}
	chat, err := wm.CreateChat(r.taskID, "Continued on "+fb.Name)
	if err != nil {
		log.Printf("runs: fallback for run %s: create chat: %v; giving up", r.id, err)
		return
	}

	r.mu.Lock()
	tail := trailingAgentText(r.history)
	r.mu.Unlock()
	prompt := failoverPreface(string(r.provider)) + stripProvenanceHeader(r.prompt)
	if tail != "" {
		prompt += "\n\n--- tail of the previous attempt's output (read-only context, may be incomplete) ---\n" + tailBytes(tail, fallbackTailMax)
	}
	cfg, err := json.Marshal(chatQueueRunConfig{
		Provider: fb.Provider, PermissionMode: fb.PermissionMode, AutoAccept: fb.AutoAccept,
		ThinkingLevel: fb.ThinkingLevel, Model: fb.Model, ReplyChatID: r.replyChatID,
		ProfileID: fb.ID, FailoverOf: r.id,
	})
	if err != nil {
		return
	}
	if _, err := reg.st.EnqueueChatPromptAt(chat.ID, prompt, string(cfg), store.ChatQueueSourceOrchestrator, 0, 0, 0, "", nil); err != nil {
		log.Printf("runs: fallback for run %s: enqueue on chat %d: %v; giving up", r.id, chat.ID, err)
		return
	}
	reg.notifyQueueUpdated(chat.ID)
	reg.deliverOne(chat.ID)
}
