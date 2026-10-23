package runs

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/runs/ratelimit"
	"github.com/spacingmind/smind/internal/store"
)

// defaultRetryGrace is added to a rate limit's reset instant before the
// "wait" retry becomes deliverable, so the retry does not race the provider's
// own clock.
const defaultRetryGrace = 30 * time.Second

// retryPreface leads the prompt of an automatic "wait" retry: the failed
// attempt may already have changed the worktree.
const retryPreface = "(Retrying after a provider rate limit. Your earlier attempt may have made partial progress: check git status and git log, then continue.)\n\n"

// applyRateLimitPolicy is ADR-0041 F2b: when a run launched with a profile
// fails on a provider rate limit, do what the profile's onRateLimit says.
// It acts at most once per chain (a retry is itself a run with failoverOf
// set, and never triggers another action), only for user runs, and only
// while the profile still exists.
func (reg *Registry) applyRateLimitPolicy(r *run, fail ratelimit.Result) {
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
		// TODO(ADR-0041 F3): start the work on prof.FallbackProfileID's
		// provider in a new chat. F2b only surfaces the suggestion.
	}
}

// enqueueRateLimitRetry puts one held item on the failed run's own chat,
// deliverable shortly after the limit resets. It goes straight to the store:
// that deliberately bypasses the non-human auto-approve check of
// StartWhenBusy, because it re-sends the SAME run's own settings and never
// escalates them. Attachments are not re-attached (out of scope).
func (reg *Registry) enqueueRateLimitRetry(r *run, fail ratelimit.Result) {
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
