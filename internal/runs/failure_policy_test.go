package runs

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/store"
)

const (
	authErrText      = `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`
	modelErrText     = `There's an issue with the selected model (claude-opus-9). It may not exist or you may not have access to it.`
	transientErrText = `API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
)

// humanFixNeverActs: a human-fix failure sets no cooldown, queues no wait
// retry and starts no fallback successor, for either profile policy (AC12).
func humanFixNeverActs(t *testing.T, kind, errText string) {
	t.Helper()
	e := newFallbackEnv(t, "")
	fbMain, _ := fallbackProfiles(t, e.st)
	r := e.failedRun(t, "hf-fallback", failedOpts{profileID: fbMain})
	e.reg.finish(r, errors.New(errText))
	if st := r.statusLocked(); st.FailureKind != kind || st.FailureAction == nil {
		t.Fatalf("status = kind %q action %+v, want %q with an action", st.FailureKind, st.FailureAction, kind)
	}
	if n := len(extraChats(t, e, r.taskID)); n != 0 {
		t.Fatalf("fallback chats = %d, want 0", n)
	}
	if _, ok, _ := e.st.GetProviderCooldown("glm", time.Now()); ok {
		t.Fatal("provider cooldown set")
	}

	// A wait profile, even when a (made-up) future reset rode along.
	wait := profileWith(t, e.st, "wait")
	r2 := e.failedRun(t, "hf-wait", failedOpts{profileID: wait})
	at := time.Now().Add(time.Hour).UTC()
	e.reg.applyRateLimitPolicy(r2, failure.Result{Kind: kind, ResetAt: &at})
	if n := len(retryItems(t, e.st, r2.chatID)); n != 0 {
		t.Fatalf("wait retry items = %d, want 0", n)
	}
}

func TestFinish_AuthExpiredSetsNoCooldownNoFallbackNoWait(t *testing.T) {
	t.Parallel()
	humanFixNeverActs(t, failure.KindAuthExpired, authErrText)
}

func TestFinish_ModelNotInPlanNeverRetriesSameAccount(t *testing.T) {
	t.Parallel()
	humanFixNeverActs(t, failure.KindModelNotInPlan, modelErrText)
}

func TestFinish_Transient429RetriesTwiceThenSurfaces(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "none")

	r1 := e.failedRun(t, "t1", failedOpts{profileID: pid})
	before := time.Now()
	e.reg.finish(r1, errors.New(transientErrText))
	items := retryItems(t, e.st, r1.chatID)
	if len(items) != 1 || items[0].NotBefore == nil {
		t.Fatalf("after the first failure: %d retry items, want 1 held", len(items))
	}
	if d := items[0].NotBefore.Sub(before); d < 29*time.Second || d > 40*time.Second {
		t.Fatalf("first backoff = %v, want ~30s", d)
	}
	var cfg chatQueueRunConfig
	if err := json.Unmarshal([]byte(items[0].RunConfig), &cfg); err != nil || cfg.FailoverOf != "t1" || !cfg.RetryTransient {
		t.Fatalf("run config = %+v (%v)", cfg, err)
	}
	if _, ok, _ := e.st.GetProviderCooldown("glm", time.Now()); ok {
		t.Fatal("a transient 429 set a provider cooldown")
	}
	if a := r1.statusLocked().FailureAction; a == nil || a.Kind != failure.ActionAutoRetry || a.Attempt != 1 || a.Max != 2 {
		t.Fatalf("action = %+v, want auto_retry 1 of 2", a)
	}

	r2 := e.failedRun(t, "t2", failedOpts{profileID: pid, failoverOf: "t1"})
	before = time.Now()
	e.reg.finish(r2, errors.New(transientErrText))
	items = retryItems(t, e.st, r1.chatID)
	if len(items) != 2 {
		t.Fatalf("after the second failure: %d retry items, want 2", len(items))
	}
	if d := items[1].NotBefore.Sub(before); d < 119*time.Second || d > 130*time.Second {
		t.Fatalf("second backoff = %v, want ~2m", d)
	}

	r3 := e.failedRun(t, "t3", failedOpts{profileID: pid, failoverOf: "t2"})
	e.reg.finish(r3, errors.New(transientErrText))
	if n := len(retryItems(t, e.st, r1.chatID)); n != 2 {
		t.Fatalf("after the third failure: %d retry items, want still 2 (cap)", n)
	}
	if a := r3.statusLocked().FailureAction; a == nil || a.Kind != failure.ActionRetryNow {
		t.Fatalf("spent action = %+v, want retry_now", a)
	}
}

func TestFinish_Transient429PlainHumanSendIsNotRetried(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	r := e.failedRun(t, "human", failedOpts{})
	e.reg.finish(r, errors.New(transientErrText))
	if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
		t.Fatalf("retry items = %d, want 0 for a plain human send", n)
	}
}

func TestFinish_Transient429RetryHeldWhileProviderCooldown(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "none")
	e.cool(t, "glm", time.Hour)
	r := e.failedRun(t, "cooling", failedOpts{profileID: pid})
	e.reg.finish(r, errors.New(transientErrText))
	if n := len(retryItems(t, e.st, r.chatID)); n != 0 {
		t.Fatalf("retry items = %d, want 0 while the provider cools down", n)
	}

	// A retry queued before the cooldown began is dropped at delivery, not run.
	e2 := newQueuedTestEnv(t, "")
	pid2 := profileWith(t, e2.st, "none")
	r2 := e2.failedRun(t, "queued-first", failedOpts{profileID: pid2})
	e2.reg.finish(r2, errors.New(transientErrText))
	items := retryItems(t, e2.st, r2.chatID)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	e2.cool(t, "glm", time.Hour)
	if err := (func() error { _, err := e2.st.ReleaseHeldRetries("glm", time.Now()); return err })(); err != nil {
		t.Fatal(err)
	}
	e2.reg.DeliverQueued()
	got := pollItem(t, e2.st, items[0].ID, store.ChatQueueStatusCancelled, 5*time.Second)
	if got.RunID != nil {
		t.Fatalf("item started run %v during a cooldown", *got.RunID)
	}
}

func TestFinish_Transient429RetryRunsAreCounted(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.retryGrace = 0
	pid := profileWith(t, e.st, "none")
	r := e.failedRun(t, "counted", failedOpts{profileID: pid})
	e.reg.finish(r, errors.New(`API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"slow"}} try again in 1 seconds`))
	items := retryItems(t, e.st, r.chatID)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	// Retry-After wins but is clamped to >= 5s.
	if d := time.Until(*items[0].NotBefore); d < 3*time.Second || d > 8*time.Second {
		t.Fatalf("backoff = %v, want the clamped 5s hint", d)
	}
	if err := (func() error { _, err := e.st.ReleaseHeldRetries("glm", time.Now()); return err })(); err != nil {
		t.Fatal(err)
	}
	e.reg.DeliverQueued()
	got := pollItem(t, e.st, items[0].ID, store.ChatQueueStatusDelivered, 10*time.Second)
	waitForStatus(t, e.reg, *got.RunID, StatusDone, 10*time.Second)
	row, err := e.st.GetRun(*got.RunID)
	if err != nil || row.FailoverOf != "counted" || row.ProfileID != pid {
		t.Fatalf("retry run row = %+v (%v): a counted run of the same chain", row, err)
	}
}

func TestFinish_PlanWindowStillSetsCooldownAndWait(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "wait")
	r := e.failedRun(t, "plan", failedOpts{profileID: pid})
	e.finishRateLimited(r, time.Now().Add(time.Hour))
	if st := r.statusLocked(); st.FailureKind != failure.KindPlanWindowExhausted {
		t.Fatalf("kind = %q", st.FailureKind)
	}
	if _, ok, _ := e.st.GetProviderCooldown("glm", time.Now()); !ok {
		t.Fatal("no cooldown for a plan window with a future reset")
	}
	if n := len(retryItems(t, e.st, r.chatID)); n != 1 {
		t.Fatalf("wait items = %d, want 1", n)
	}
}

func TestResumableKind_NewClasses(t *testing.T) {
	t.Parallel()
	for _, k := range []string{failure.KindAuthExpired, failure.KindModelNotInPlan, failure.KindTransient429, failure.KindPlanWindowExhausted, failure.KindRateLimited, failure.KindCLIExited} {
		if !resumableKind(k) {
			t.Errorf("resumableKind(%q) = false", k)
		}
	}
	if resumableKind("") || resumableKind("martian") {
		t.Error("empty or unknown kind is resumable")
	}
}

// statusRecorder is a Notifier that records the RunStatus of every NotifyRunStatus.
type statusRecorder struct {
	failoverRecorder
	smu      sync.Mutex
	statuses []RunStatus
}

func (n *statusRecorder) NotifyRunStatus(s RunStatus) {
	n.smu.Lock()
	n.statuses = append(n.statuses, s)
	n.smu.Unlock()
}

func (n *statusRecorder) last(runID string) (RunStatus, bool) {
	n.smu.Lock()
	defer n.smu.Unlock()
	for i := len(n.statuses) - 1; i >= 0; i-- {
		if n.statuses[i].ID == runID {
			return n.statuses[i], true
		}
	}
	return RunStatus{}, false
}

// TestFinish_Transient429StatusEventCarriesRetryAction: the terminal run.status
// a client sees last must already say "retrying automatically": the event is
// republished once the retry is queued, so no client keeps a stale Retry button
// or inbox item for a run the daemon is already retrying.
func TestFinish_Transient429StatusEventCarriesRetryAction(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	rec := &statusRecorder{}
	e.reg.SetNotifier(rec)
	pid := profileWith(t, e.st, "none")
	r := e.failedRun(t, "evt", failedOpts{profileID: pid})
	e.reg.finish(r, errors.New(transientErrText))
	st, ok := rec.last("evt")
	if !ok || st.Status != StatusError || st.FailureAction == nil || st.FailureAction.Kind != failure.ActionAutoRetry || st.FailureAction.Attempt != 1 {
		t.Fatalf("last status event = %+v (seen %v), want error with an auto_retry action", st, ok)
	}
}

// TestEnqueueRetry_DoesNotStackThePreface: a retry of a retry carries one
// preface, not the earlier one inside the new one.
func TestEnqueueRetry_DoesNotStackThePreface(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	pid := profileWith(t, e.st, "none")
	r := e.failedRun(t, "stack", failedOpts{profileID: pid, failoverOf: "first", prompt: transientRetryPreface + "build it"})
	e.reg.finish(r, errors.New(transientErrText))
	items := retryItems(t, e.st, r.chatID)
	if len(items) != 1 {
		t.Fatalf("retry items = %d, want 1", len(items))
	}
	if got := strings.Count(items[0].Prompt, "Retrying after"); got != 1 || !strings.HasSuffix(items[0].Prompt, "build it") {
		t.Fatalf("prompt = %q, want exactly one preface and the original prompt", items[0].Prompt)
	}
}
