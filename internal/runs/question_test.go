package runs

import (
	"context"
	"fmt"
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
		case <-time.After(5 * time.Millisecond): // poll tick
		}
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

// A credential typed into a question the secret check did not catch reaches the
// agent as typed (the human's intent) but is scrubbed in the stored history.
func TestQuestion_StoredAnswerTextIsScrubbed(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	d := runPermissionDecider{reg: reg, r: r}
	ch, id := askAsync(t, d, colourQ("Which colour?"))
	secret := "Authorization: Bearer abcdef0123456789abcdef0123456789"
	ans := taskrunner.QuestionAnswer{Action: taskrunner.QuestionActionAnswer,
		Answers: []taskrunner.QuestionAnswerItem{{QuestionID: "q0", Text: secret}}}
	if err := reg.RespondQuestion(runID, id, ans); err != nil {
		t.Fatal(err)
	}
	res := <-ch
	if got := res.out.Answer.Answers[0].Text; got != secret {
		t.Fatalf("the agent must get the answer as typed, got %q", got)
	}
	_, resolved := questionEvents(r)
	stored := resolved[0].Question.Answer.Answers[0].Text
	if stored != answerWithheld {
		t.Fatalf("stored history keeps a secret-looking answer: %q", stored)
	}
	for text, keep := range map[string]bool{"my p\u200bassword is hunter2": false, "Blue": true} {
		if got := scrubStoredText(text); (got == text) != keep {
			t.Fatalf("scrubStoredText(%q) = %q", text, got)
		}
	}
}

// A profile whose setting cannot be read fails closed: the question is
// declined, not parked for a human.
func TestQuestion_UnreadableProfileFailsClosed(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	r.profileID = 987654 // no such profile: the store read errors
	out, err := runPermissionDecider{reg: reg, r: r}.DecideQuestion(context.Background(), colourQ("Which colour?"))
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedProfile, "not attended")
	if len(reg.PendingQuestions()) != 0 {
		t.Fatal("an unreadable profile parked a question")
	}
}

// A request the provider mapping refused (an ACP form it cannot model) is
// declined whole: recorded refused_unsupported, never parked, never shown.
func TestQuestion_RefusedRequestDeclinedAndRecordedNotParked(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	req := colourQ("Which colour?")
	req.Refuse = taskrunner.QuestionResolvedUnsupported
	out, err := runPermissionDecider{reg: reg, r: r}.DecideQuestion(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedUnsupported, "could not be shown")
	reqs, resolved := questionEvents(r)
	if len(reqs) != 0 {
		t.Fatalf("a refused request was shown: %+v", reqs)
	}
	if len(resolved) != 1 || resolved[0].Question.Resolution != taskrunner.QuestionResolvedUnsupported ||
		resolved[0].Question.Request != nil || resolved[0].Question.Answer != nil {
		t.Fatalf("resolved: %+v, want one bare refused_unsupported", resolved)
	}
	if len(reg.PendingQuestions()) != 0 {
		t.Fatal("a refused request parked")
	}
}

// The refusal does not bypass the earlier gates: a seat or system run still
// resolves NoHuman, a declining profile still resolves profile_decline.
func TestQuestion_RefuseDoesNotBypassSeatOrProfile(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	reg.SetSeatSource(func(int64) (SeatSpec, bool) { return SeatSpec{}, true })
	req := colourQ("Which colour?")
	req.Refuse = taskrunner.QuestionResolvedUnsupported
	out, err := runPermissionDecider{reg: reg, r: r}.DecideQuestion(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedNoHuman, "No human")

	reg2, r2, _ := startedRun(t, taskrunner.PermissionSettings{})
	p, err := reg2.st.CreateAgentProfile(store.AgentProfile{Name: "refuse-decline", Provider: "claude-native", Questions: QuestionsDecline})
	if err != nil {
		t.Fatal(err)
	}
	r2.profileID = p.ID
	out, err = runPermissionDecider{reg: reg2, r: r2}.DecideQuestion(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	wantDeclined(t, out, taskrunner.QuestionResolvedProfile, "not attended")
}

// TestElicitationEnabled_Matrix pins who may advertise the ACP elicitation
// capability (ADR-0055 D4): only an attended run whose profile EXPLICITLY opted
// in (agent_questions) and asks. The default `ask` alone is not an opt-in.
func TestElicitationEnabled_Matrix(t *testing.T) {
	t.Parallel()
	reg, r, _ := startedRun(t, taskrunner.PermissionSettings{})
	n := 0
	profile := func(questions string, optIn bool) int64 {
		n++
		p, err := reg.st.CreateAgentProfile(store.AgentProfile{Name: fmt.Sprintf("elicit-%d", n), Provider: "glm", Questions: questions, AgentQuestions: optIn})
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}

	r.profileID = profile(QuestionsAsk, true)
	if !reg.elicitationEnabled(r) {
		t.Fatal("a run whose profile opted in and asks must advertise elicitation")
	}
	// An existing / default profile (questions ask by default, no opt-in) must
	// NOT advertise: upgrading changes no running setup.
	r.profileID = profile("", false)
	if reg.elicitationEnabled(r) {
		t.Fatal("a default profile (ask, not opted in) must not advertise")
	}
	r.profileID = profile(QuestionsAsk, false)
	if reg.elicitationEnabled(r) {
		t.Fatal("ask without the explicit opt-in must not advertise")
	}
	for _, setting := range []string{QuestionsDecline, QuestionsDisabled} {
		r.profileID = profile(setting, true)
		if reg.elicitationEnabled(r) {
			t.Fatalf("profile %q must not advertise even when opted in", setting)
		}
	}

	r.profileID = 0
	if reg.elicitationEnabled(r) {
		t.Fatal("a run without a profile must not advertise")
	}

	r.profileID = profile(QuestionsAsk, true)
	r.purpose = "learning"
	if reg.elicitationEnabled(r) {
		t.Fatal("a system run must not advertise")
	}
	r.purpose = ""

	reg.SetSeatSource(func(int64) (SeatSpec, bool) { return SeatSpec{}, true })
	if reg.elicitationEnabled(r) {
		t.Fatal("a daemon-owned seat must not advertise")
	}

	reg.SetSeatSource(func(int64) (SeatSpec, bool) { return SeatSpec{}, false })
	r.profileID = 987654 // no such profile: the store read errors
	if reg.elicitationEnabled(r) {
		t.Fatal("an unreadable profile must fail closed")
	}
}

// A question parked in memory dies with the process. On startup the run is
// marked interrupted and its still-open question_request gets a
// question_resolved(run_ended), so history never holds an orphan that a client
// would draw as a live card; an already answered question is left alone.
func TestQuestion_RestartClosesOrphanRequests(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	if _, err := st.CreateRun(store.Run{ID: "orphan-run", TaskID: task.ID, Provider: "claude-native", Prompt: "p", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	req := colourQ("Which colour?")
	events := []taskrunner.Event{
		{Type: taskrunner.EventTypeQuestionRequest, Question: &taskrunner.QuestionEvent{RequestID: "answered", Request: &req}},
		{Type: taskrunner.EventTypeQuestionResolved, Question: &taskrunner.QuestionEvent{RequestID: "answered", Resolution: taskrunner.QuestionResolvedByHuman}},
		{Type: taskrunner.EventTypeQuestionRequest, Question: &taskrunner.QuestionEvent{RequestID: "open", Request: &req}},
	}
	for i, e := range events {
		data, err := encodeEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AppendRunEvent("orphan-run", int64(i+1), data); err != nil {
			t.Fatal(err)
		}
	}
	reg := newTestRegistry(t, st)
	if _, status, err := reg.History("orphan-run"); err != nil || status.Status != StatusInterrupted {
		t.Fatalf("status = %+v, %v; want interrupted", status, err)
	}
	rows, err := st.ListRunEvents("orphan-run")
	if err != nil {
		t.Fatal(err)
	}
	var resolved []string
	for _, row := range rows {
		e, _ := decodeEvent(row.EventData)
		if e.Type == taskrunner.EventTypeQuestionResolved && e.Question.Resolution == taskrunner.QuestionResolvedDaemonRestart {
			resolved = append(resolved, e.Question.RequestID)
		}
	}
	if len(resolved) != 1 || resolved[0] != "open" {
		t.Fatalf("daemon_restart resolutions = %v, want only the open request", resolved)
	}
}
