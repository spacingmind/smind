package runs

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func colourQ(text string) taskrunner.QuestionRequest {
	return taskrunner.QuestionRequest{Source: taskrunner.QuestionSourceClaude, Questions: []taskrunner.Question{{
		ID: "q0", Text: text, Kind: taskrunner.QuestionSingle, AllowOther: true,
		Options: []taskrunner.QuestionOption{{ID: "o0", Label: "Red"}, {ID: "o1", Label: "Blue"}},
	}}}
}

func pickBlue() taskrunner.QuestionAnswer {
	return taskrunner.QuestionAnswer{Action: taskrunner.QuestionActionAnswer,
		Answers: []taskrunner.QuestionAnswerItem{{QuestionID: "q0", OptionIDs: []string{"o1"}}}}
}

type qResult struct {
	out taskrunner.QuestionOutcome
	err error
}

// askAsync parks a question on its own goroutine and returns the request id
// once it is pending.
func askAsync(t *testing.T, d runPermissionDecider, req taskrunner.QuestionRequest) (<-chan qResult, string) {
	t.Helper()
	ch := make(chan qResult, 1)
	go func() {
		out, err := d.DecideQuestion(context.Background(), req)
		ch <- qResult{out, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d.r.mu.Lock()
		for id := range d.r.pendingQuestions {
			d.r.mu.Unlock()
			return ch, id
		}
		d.r.mu.Unlock()
		select {
		case res := <-ch:
			t.Fatalf("question did not park: %+v %v", res.out, res.err)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pending question")
	return nil, ""
}

func questionEvents(r *run) (reqs, resolved []taskrunner.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.history {
		switch e.Type {
		case taskrunner.EventTypeQuestionRequest:
			reqs = append(reqs, e)
		case taskrunner.EventTypeQuestionResolved:
			resolved = append(resolved, e)
		}
	}
	return
}

func wantDeclined(t *testing.T, out taskrunner.QuestionOutcome, res taskrunner.QuestionResolution, noteHas string) {
	t.Helper()
	if out.Answer.Action != taskrunner.QuestionActionDecline || len(out.Answer.Answers) != 0 || out.Resolution != res ||
		!strings.Contains(out.Answer.Note, noteHas) {
		t.Fatalf("outcome = %+v; want decline %q containing %q", out, res, noteHas)
	}
}

func TestQuestion_HumanAnswerResolvesAndIsRecorded(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	d := runPermissionDecider{reg: reg, r: r}
	ch, id := askAsync(t, d, colourQ("Which colour?"))
	if pq := reg.PendingQuestions(); len(pq) != 1 || pq[0].RequestID != id || pq[0].RunID != runID {
		t.Fatalf("PendingQuestions = %+v", pq)
	}
	if err := reg.RespondQuestion(runID, id, pickBlue()); err != nil {
		t.Fatal(err)
	}
	res := <-ch
	if res.err != nil || res.out.Resolution != taskrunner.QuestionResolvedByHuman || res.out.Answer.Action != taskrunner.QuestionActionAnswer {
		t.Fatalf("outcome = %+v %v", res.out, res.err)
	}
	reqs, resolved := questionEvents(r)
	if len(reqs) != 1 || len(resolved) != 1 || resolved[0].Question.Answer == nil || resolved[0].Question.RequestID != id {
		t.Fatalf("events: %+v / %+v", reqs, resolved)
	}
	if len(reg.PendingQuestions()) != 0 {
		t.Fatal("still pending after answer")
	}
	if err := reg.RespondQuestion(runID, id, pickBlue()); err == nil {
		t.Fatal("a second answer was accepted")
	}
}

func TestQuestion_InvalidAnswerLeavesItParked(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	d := runPermissionDecider{reg: reg, r: r}
	ch, id := askAsync(t, d, colourQ("Which colour?"))
	bad := taskrunner.QuestionAnswer{Action: taskrunner.QuestionActionAnswer,
		Answers: []taskrunner.QuestionAnswerItem{{QuestionID: "q0", OptionIDs: []string{"nope"}}}}
	if err := reg.RespondQuestion(runID, id, bad); err == nil {
		t.Fatal("invalid answer accepted")
	}
	if len(reg.PendingQuestions()) != 1 {
		t.Fatal("an invalid answer must leave the question parked")
	}
	if err := reg.RespondQuestion(runID, id, taskrunner.QuestionAnswer{Action: taskrunner.QuestionActionDecline, Note: "later"}); err != nil {
		t.Fatal(err)
	}
	res := <-ch
	wantDeclined(t, res.out, taskrunner.QuestionResolvedDeclined, "later")
	_, resolved := questionEvents(r)
	if resolved[0].Question.Answer != nil {
		t.Fatal("a decline must not store an answer")
	}
}

// TestQuestion_TimeoutDeclinesWithNote: nobody answers, the timeout fires, and the
// agent gets a decline with a note, never an answer and never an allow.
func TestQuestion_TimeoutDeclinesWithNote(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	reg.SetPermissionTimeout(40 * time.Millisecond)
	out, err := runPermissionDecider{reg: reg, r: r}.DecideQuestion(context.Background(), colourQ("Which colour?"))
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedTimeout, "No human answered")
	_, resolved := questionEvents(r)
	if len(resolved) != 1 || resolved[0].Question.Resolution != taskrunner.QuestionResolvedTimeout || resolved[0].Question.Answer != nil {
		t.Fatalf("resolved: %+v", resolved)
	}
	if len(reg.PendingQuestions()) != 0 {
		t.Fatal("a timed-out question stays pending")
	}
}

func TestQuestion_SeatAndSystemRunDeclineAtOnce(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	reg.SetSeatSource(func(int64) (SeatSpec, bool) { return SeatSpec{}, true })
	out, err := runPermissionDecider{reg: reg, r: r}.DecideQuestion(context.Background(), colourQ("Which colour?"))
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedNoHuman, "No human")
	if reqs, _ := questionEvents(r); len(reqs) != 0 || len(reg.PendingQuestions()) != 0 {
		t.Fatal("a seat must never park a question")
	}

	reg2, r2, _ := startedRun(t, taskrunner.PermissionSettings{})
	r2.purpose = "learning"
	out, err = runPermissionDecider{reg: reg2, r: r2}.DecideQuestion(context.Background(), colourQ("Which colour?"))
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedNoHuman, "No human")
}

func TestQuestion_ProfileDeclineAndDisabled(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{QuestionsDecline, QuestionsDisabled} {
		reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
		p, err := reg.st.CreateAgentProfile(store.AgentProfile{Name: "p-" + policy, Provider: "claude-native", PermissionMode: "default", Questions: policy})
		if err != nil {
			t.Fatal(err)
		}
		r.profileID = p.ID
		out, err := runPermissionDecider{reg: reg, r: r}.DecideQuestion(context.Background(), colourQ("Which colour?"))
		if err != nil {
			t.Fatal(err)
		}
		wantDeclined(t, out, taskrunner.QuestionResolvedProfile, "not attended")
		if len(reg.PendingQuestions()) != 0 {
			t.Fatalf("%s: parked", policy)
		}
	}
}

func TestQuestion_SecretRefusedNothingRenderedOrStored(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	out, err := runPermissionDecider{reg: reg, r: r}.DecideQuestion(context.Background(), colourQ("Please paste the API key for the service"))
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedSecret, "Secrets cannot be provided")
	reqs, resolved := questionEvents(r)
	if len(reqs) != 0 || len(resolved) != 1 || resolved[0].Question.Request != nil || resolved[0].Question.Answer != nil {
		t.Fatalf("a refused secret must record no question text: %+v / %+v", reqs, resolved)
	}
	if len(reg.PendingQuestions()) != 0 {
		t.Fatal("a secret question was parked")
	}
}

func TestQuestion_RepeatGuard(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	d := runPermissionDecider{reg: reg, r: r}
	ch, id := askAsync(t, d, colourQ("Which colour?"))
	if err := reg.RespondQuestion(runID, id, pickBlue()); err != nil {
		t.Fatal(err)
	}
	<-ch
	// The same question again (reworded case/space) is declined at once.
	out, err := d.DecideQuestion(context.Background(), colourQ("  which COLOUR? "))
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedRepeat, "already asked")

	// Distinct questions are capped inside the window.
	reg2, r2, runID2 := startedRun(t, taskrunner.PermissionSettings{})
	d2 := runPermissionDecider{reg: reg2, r: r2}
	for i := 0; i < questionRepeatCap; i++ {
		ch, id := askAsync(t, d2, colourQ("question "+string(rune('a'+i))))
		if err := reg2.RespondQuestion(runID2, id, pickBlue()); err != nil {
			t.Fatal(err)
		}
		<-ch
	}
	out, err = d2.DecideQuestion(context.Background(), colourQ("one more"))
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedRepeat, "already asked")
}

func TestQuestion_RunStopAbandonsAndSecondAnswerFails(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	d := runPermissionDecider{reg: reg, r: r}
	ch, id := askAsync(t, d, colourQ("Which colour?"))
	r.mu.Lock()
	r.status = StatusRunning
	r.mu.Unlock()
	if err := reg.Stop(runID); err != nil {
		t.Fatal(err)
	}
	res := <-ch
	// Put the run back to its real (finished) status so CloseAll does not wait on it.
	r.mu.Lock()
	r.status = StatusDone
	r.mu.Unlock()
	if res.err == nil {
		t.Fatalf("a stopped run must unblock the decider with an error, got %+v", res.out)
	}
	if err := reg.RespondQuestion(runID, id, pickBlue()); err == nil {
		t.Fatal("answered a question of a stopped run")
	}
	_, resolved := questionEvents(r)
	if len(resolved) != 1 || resolved[0].Question.Resolution != taskrunner.QuestionResolvedRunEnded {
		t.Fatalf("resolved: %+v", resolved)
	}
}

func TestQuestion_NotifierGetsPendingAndResolved(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	n := &questionRecorder{pendingCounter: &pendingCounter{resolvedRecorder: newResolvedRecorder()}}
	reg.SetNotifier(n)
	d := runPermissionDecider{reg: reg, r: r}
	ch, id := askAsync(t, d, colourQ("Which colour?"))
	if err := reg.RespondQuestion(runID, id, pickBlue()); err != nil {
		t.Fatal(err)
	}
	<-ch
	if n.qPending.Load() != 1 || n.qResolved.Load() != 1 {
		t.Fatalf("pending=%d resolved=%d", n.qPending.Load(), n.qResolved.Load())
	}
}

type questionRecorder struct {
	*pendingCounter
	qPending, qResolved atomic.Int32
}

func (q *questionRecorder) NotifyQuestionPending(string, int64, int64, string, taskrunner.QuestionRequest) {
	q.qPending.Add(1)
}

func (q *questionRecorder) NotifyQuestionResolved(string, int64, int64, string, string) {
	q.qResolved.Add(1)
}

// A profile with questions: disabled takes the agent's ask tool away (claude);
// ask and decline leave it available.
func TestQuestion_DisabledProfileDisallowsAskTool(t *testing.T) {
	t.Parallel()
	c, _ := policyFor(t, store.AgentProfile{Questions: QuestionsDisabled})
	if len(c.DisallowedTools) != 1 || c.DisallowedTools[0] != "AskUserQuestion" {
		t.Fatalf("disabled: DisallowedTools = %v", c.DisallowedTools)
	}
	for _, q := range []string{QuestionsAsk, QuestionsDecline} {
		c, _ := policyFor(t, store.AgentProfile{Questions: q, Instructions: "ROLE"})
		if len(c.DisallowedTools) != 0 {
			t.Fatalf("%s: DisallowedTools = %v", q, c.DisallowedTools)
		}
	}
}

// TestQuestion_NeverAutoAnswers: whatever ends a question without a human
// answer (timeout, repeat, profile, secret, seat), the outcome carries no
// chosen option and no free text.
func TestQuestion_NeverAutoAnswers(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	reg.SetPermissionTimeout(30 * time.Millisecond)
	d := runPermissionDecider{reg: reg, r: r}
	for _, q := range []taskrunner.QuestionRequest{colourQ("Which colour?"), colourQ("Paste the password"), colourQ("which colour?")} {
		out, err := d.DecideQuestion(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if out.Answer.Action != taskrunner.QuestionActionDecline || len(out.Answer.Answers) != 0 {
			t.Fatalf("%q ended with an answer: %+v", q.Questions[0].Text, out)
		}
	}
}

// TestQuestion_AnswerWinsRaceWithTimeout: an answer that RespondQuestion has
// already accepted is never overruled by the timeout arm.
func TestQuestion_AnswerWinsRaceWithTimeout(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	d := runPermissionDecider{reg: reg, r: r}
	pq := &pendingQuestion{req: colourQ("Which colour?"), ch: make(chan taskrunner.QuestionAnswer, 1)}
	r.mu.Lock()
	r.pendingQuestions = map[string]*pendingQuestion{"req1": pq}
	r.mu.Unlock()
	if err := reg.RespondQuestion(runID, "req1", pickBlue()); err != nil {
		t.Fatal(err)
	}
	// RespondQuestion took the entry and delivered; the timeout arm now fires.
	ans, ok := d.settleQuestion("req1", pq)
	if !ok || ans.Action != taskrunner.QuestionActionAnswer {
		t.Fatalf("settleQuestion = %+v, %v; want the delivered answer", ans, ok)
	}
	// Without a delivered answer the entry is removed and the decline stands.
	r.mu.Lock()
	r.pendingQuestions = map[string]*pendingQuestion{"req2": pq}
	r.mu.Unlock()
	if _, ok := d.settleQuestion("req2", pq); ok {
		t.Fatal("settleQuestion invented an answer")
	}
	if len(reg.PendingQuestions()) != 0 {
		t.Fatal("entry left pending")
	}
}

// Question events survive the persisted-row round trip.
func TestEvent_QuestionRoundTripsThroughPersistence(t *testing.T) {
	t.Parallel()
	req := colourQ("Which colour?")
	for _, e := range []taskrunner.Event{
		{Type: taskrunner.EventTypeQuestionRequest, Question: &taskrunner.QuestionEvent{RequestID: "r", Request: &req}},
		{Type: taskrunner.EventTypeQuestionResolved, Question: &taskrunner.QuestionEvent{RequestID: "r", Resolution: taskrunner.QuestionResolvedByHuman, Answer: &taskrunner.QuestionAnswer{Action: "answer"}}},
	} {
		raw, err := encodeEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeEvent(raw)
		if err != nil || got.Type != e.Type || got.Question == nil || got.Question.RequestID != "r" {
			t.Fatalf("round trip: %+v %v", got, err)
		}
	}
}
