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

// transientRetryBackoffs is the delay before each automatic retry of a
// transient_429 (the run-failure-classes plan, D3): 30 s, then 2 min.
// len() is the retry cap.
var transientRetryBackoffs = []time.Duration{30 * time.Second, 2 * time.Minute}

const (
	transientRetryAfterMin = 5 * time.Second
	transientRetryAfterMax = 5 * time.Minute
)

// transientRetryPreface leads the prompt of an automatic transient retry.
const transientRetryPreface = "(Retrying after a short provider throttle. Your earlier attempt may have made partial progress: check git status and git log, then continue.)\n\n"

// applyRateLimitPolicy acts on a failed run by its failure class. A plan limit
// (plan_window_exhausted, residual rate_limited) does what the launching
// profile's onRateLimit says (ADR-0041 F2b), at most once per chain, only for
// user runs launched with a profile. A transient_429 is retried on the same
// chat with a bounded backoff (D3). auth_expired and model_not_in_plan do
// nothing here, by design: no wait, no fallback, no retry; a person fixes
// setup (AC12). Never for system runs (purpose != "").
func (reg *Registry) applyRateLimitPolicy(r *run, fail failure.Result) {
	if r.purpose != "" {
		return
	}
	switch {
	case fail.Kind == failure.KindTransient429:
		reg.enqueueTransientRetry(r, fail)
	case failure.IsPlanLimit(fail.Kind):
		if r.profileID == 0 || r.failoverOf != "" {
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
}

// transientRetryEligible: a plain human send is never auto-retried (it gets
// the retry_now action and an inbox item); a profile run or an
// orchestrator/agent-sourced run is.
func transientRetryEligible(r *run) bool {
	return r.profileID != 0 || (r.origin != "" && r.origin != store.ChatQueueSourceHuman)
}

// transientAttempts counts the transient_429 runs this run continues: its
// failoverOf chain, only while each ancestor itself failed transient.
func (reg *Registry) transientAttempts(r *run) int {
	n := 0
	for id := r.failoverOf; id != "" && n <= len(transientRetryBackoffs); n++ {
		row, err := reg.st.GetRun(id)
		if err != nil || row.FailureKind != failure.KindTransient429 {
			break
		}
		id = row.FailoverOf
	}
	return n
}

// enqueueTransientRetry holds one retry on the failed run's chat for the next
// backoff step (Retry-After wins, clamped). Hard cap: nothing is queued while
// the provider is in cooldown (the failure then stays actionable), and the
// retry is a real run, counted in usage and the per-tree guard like any other.
func (reg *Registry) enqueueTransientRetry(r *run, fail failure.Result) {
	if !transientRetryEligible(r) {
		return
	}
	attempt := reg.transientAttempts(r)
	if attempt >= len(transientRetryBackoffs) {
		return
	}
	if until, cooling := reg.activeProviderCooldown(string(r.provider)); cooling {
		log.Printf("runs: transient retry for run %s: provider %s is cooling down until %s; not retrying", r.id, r.provider, until.Format(time.RFC3339))
		return
	}
	delay := transientRetryBackoffs[attempt]
	if fail.RetryAfter > 0 {
		delay = min(max(fail.RetryAfter, transientRetryAfterMin), transientRetryAfterMax)
	}
	notBefore := time.Now().Add(delay)
	reg.enqueueRetry(r, transientRetryPreface, &notBefore, true)
}

// enqueueRateLimitRetry puts one held item on the failed run's own chat,
// deliverable shortly after the limit resets.
func (reg *Registry) enqueueRateLimitRetry(r *run, fail failure.Result) {
	now := time.Now()
	if fail.ResetAt == nil || !fail.ResetAt.After(now) {
		return
	}
	notBefore := fail.ResetAt.Add(reg.retryGrace)
	reg.enqueueRetry(r, retryPreface, &notBefore, false)
}

// enqueueRetry goes straight to the store: that deliberately bypasses the
// non-human auto-approve check of StartWhenBusy, because it re-sends the SAME
// run's own settings and never escalates them. Attachments are not
// re-attached (out of scope).
func (reg *Registry) enqueueRetry(r *run, preface string, notBefore *time.Time, transient bool) {
	perm := r.getPerm()
	cfg, err := json.Marshal(chatQueueRunConfig{
		Provider: string(r.provider), PermissionMode: perm.Mode, AutoAccept: perm.AutoAccept,
		ThinkingLevel: string(r.thinkingLevel), Model: r.model, ReplyChatID: r.replyChatID,
		ProfileID: r.profileID, FailoverOf: r.id, RetryTransient: transient,
	})
	if err != nil {
		return
	}
	prompt := preface + stripProvenanceHeader(r.prompt)
	_, err = reg.st.EnqueueChatPromptAt(r.chatID, prompt, string(cfg), store.ChatQueueSourceOrchestrator, 0, 0, 0, "", notBefore)
	switch {
	case errors.Is(err, store.ErrQueueFull):
		log.Printf("runs: retry for run %s: chat %d queue full; giving up", r.id, r.chatID)
		return
	case err != nil:
		log.Printf("runs: retry for run %s: %v", r.id, err)
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
