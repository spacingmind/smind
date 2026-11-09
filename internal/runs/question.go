package runs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// ADR-0055: a question an agent asks the human is parked like a permission
// request (one pending entry per request id, the same timeout, the same
// cancellation cases) but carries a structured answer. Nothing here ever
// invents an answer: a timeout, a policy, a refusal and a repeat are all
// declines with a note the agent reads.

// questionRepeatWindow and questionRepeatCap are the repeat guard (ADR-0055
// §5.4): an identical question inside the window, or more than the cap of any
// questions inside it, is declined at once.
const (
	questionRepeatWindow = 10 * time.Minute
	questionRepeatCap    = 3
)

// Question policies of a profile (store.AgentProfile.Questions).
const (
	QuestionsAsk      = "ask"
	QuestionsDecline  = "decline"
	QuestionsDisabled = "disabled"
)

type pendingQuestion struct {
	req taskrunner.QuestionRequest
	ch  chan taskrunner.QuestionAnswer
}

type questionAsk struct {
	hash string
	at   time.Time
}

// QuestionNotifier is the optional extension of Notifier that announces
// question requests. A Notifier that does not implement it simply sends no
// question topics (the run still parks, and the history events still flow).
type QuestionNotifier interface {
	NotifyQuestionPending(runID string, taskID, chatID int64, requestID string, req taskrunner.QuestionRequest)
	NotifyQuestionResolved(runID string, taskID, chatID int64, requestID, reason string)
}

func (d runPermissionDecider) notifyQuestionPending(requestID string, req taskrunner.QuestionRequest) {
	if qn, ok := d.reg.getNotifier().(QuestionNotifier); ok {
		qn.NotifyQuestionPending(d.r.id, d.r.taskID, d.r.chatID, requestID, req)
	}
}

func (d runPermissionDecider) notifyQuestionResolved(requestID, reason string) {
	if qn, ok := d.reg.getNotifier().(QuestionNotifier); ok {
		qn.NotifyQuestionResolved(d.r.id, d.r.taskID, d.r.chatID, requestID, reason)
	}
}

// questionHash identifies a question by its texts only, so a re-ask with the
// options reordered or reworded still counts as the same question.
func questionHash(req taskrunner.QuestionRequest) string {
	h := sha256.New()
	for _, q := range req.Questions {
		h.Write([]byte(strings.ToLower(strings.TrimSpace(q.Text))))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// repeatedLocked records this ask and reports whether the guard declines it.
func (r *run) repeatedLocked(hash string, now time.Time) bool {
	kept := r.questionAsks[:0]
	same := false
	for _, a := range r.questionAsks {
		if now.Sub(a.at) < questionRepeatWindow {
			kept = append(kept, a)
			same = same || a.hash == hash
		}
	}
	r.questionAsks = kept
	if same || len(kept) >= questionRepeatCap {
		return true
	}
	r.questionAsks = append(r.questionAsks, questionAsk{hash: hash, at: now})
	return false
}

// questionPolicy is the run's profile setting. A run with no profile asks (the
// default). A profile that cannot be read fails CLOSED: the setting is unknown,
// and a profile that declines questions must not reach the human on a store
// hiccup, so the question is declined (and the error logged).
func (d runPermissionDecider) questionPolicy() string {
	if d.r.profileID == 0 || d.reg.st == nil {
		return QuestionsAsk
	}
	p, err := d.reg.st.GetAgentProfile(d.r.profileID)
	if err != nil {
		slog.Warn("runs: question policy unreadable; declining", "run", d.r.id, "profile", d.r.profileID, "err", err)
		return QuestionsDecline
	}
	if p.Questions == "" {
		return QuestionsAsk
	}
	return p.Questions
}

// declineOutcome is a decline carrying the note the agent reads.
func declineOutcome(res taskrunner.QuestionResolution, note string) taskrunner.QuestionOutcome {
	return taskrunner.QuestionOutcome{
		Answer:     taskrunner.QuestionAnswer{Action: taskrunner.QuestionActionDecline, Note: note},
		Resolution: res,
	}
}

// recordQuestionResolved appends the resolved event. An answer is stored only
// for a human-answered request (never for a refusal: ADR-0055 §6).
func (d runPermissionDecider) recordQuestionResolved(requestID string, res taskrunner.QuestionResolution, ans *taskrunner.QuestionAnswer) {
	d.reg.record(d.r, taskrunner.Event{
		Type:     taskrunner.EventTypeQuestionResolved,
		Question: &taskrunner.QuestionEvent{RequestID: requestID, Resolution: res, Answer: ans},
	})
}

// DecideQuestion implements taskrunner.QuestionDecider.
func (d runPermissionDecider) DecideQuestion(ctx context.Context, req taskrunner.QuestionRequest) (taskrunner.QuestionOutcome, error) {
	requestID, err := newRunID()
	if err != nil {
		return taskrunner.QuestionOutcome{}, fmt.Errorf("runs: question request id: %w", err)
	}

	// A system run or a daemon-owned seat never parks a question: no human
	// is attached to it (ADR-0055 §3).
	if _, seat := d.reg.seatSpec(d.r.taskID); d.r.purpose != "" || seat {
		d.recordQuestionResolved(requestID, taskrunner.QuestionResolvedNoHuman, nil)
		return declineOutcome(taskrunner.QuestionResolvedNoHuman, taskrunner.QuestionNoteNoHuman), nil
	}

	// The profile's setting: a run that does not take questions declines.
	if p := d.questionPolicy(); p == QuestionsDecline || p == QuestionsDisabled {
		d.recordQuestionResolved(requestID, taskrunner.QuestionResolvedProfile, nil)
		return declineOutcome(taskrunner.QuestionResolvedProfile, taskrunner.QuestionNoteProfile), nil
	}

	// A request the provider mapping could not model (an ACP form with an
	// unsupported field) is declined whole, with nothing shown.
	if req.Refuse != "" {
		d.recordQuestionResolved(requestID, req.Refuse, nil)
		return declineOutcome(req.Refuse, taskrunner.QuestionNoteBadForm), nil
	}

	// Secrets are refused, with nothing rendered and nothing stored.
	if taskrunner.AsksForSecret(req) {
		d.recordQuestionResolved(requestID, taskrunner.QuestionResolvedSecret, nil)
		return declineOutcome(taskrunner.QuestionResolvedSecret, taskrunner.QuestionNoteSecret), nil
	}

	pq := &pendingQuestion{req: req, ch: make(chan taskrunner.QuestionAnswer, 1)}
	d.r.mu.Lock()
	if d.r.repeatedLocked(questionHash(req), time.Now()) {
		d.r.mu.Unlock()
		d.recordQuestionResolved(requestID, taskrunner.QuestionResolvedRepeat, nil)
		return declineOutcome(taskrunner.QuestionResolvedRepeat, taskrunner.QuestionNoteRepeat), nil
	}
	if d.r.pendingQuestions == nil {
		d.r.pendingQuestions = make(map[string]*pendingQuestion)
	}
	d.r.pendingQuestions[requestID] = pq
	d.r.mu.Unlock()

	d.notifyQuestionPending(requestID, req)
	reqCopy := req
	d.reg.record(d.r, taskrunner.Event{
		Type:     taskrunner.EventTypeQuestionRequest,
		Question: &taskrunner.QuestionEvent{RequestID: requestID, Request: &reqCopy},
	})

	timeout := d.reg.getPermissionTimeout()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case ans := <-pq.ch:
		return d.answered(requestID, ans), nil

	case <-timer.C:
		if ans, ok := d.settleQuestion(requestID, pq); ok {
			return d.answered(requestID, ans), nil
		}
		d.recordQuestionResolved(requestID, taskrunner.QuestionResolvedTimeout, nil)
		d.notifyQuestionResolved(requestID, string(taskrunner.QuestionResolvedTimeout))
		return declineOutcome(taskrunner.QuestionResolvedTimeout, taskrunner.QuestionNoteTimeout(timeout)), nil

	case <-ctx.Done():
		if ans, ok := d.settleQuestion(requestID, pq); ok {
			return d.answered(requestID, ans), nil
		}
		d.recordQuestionResolved(requestID, taskrunner.QuestionResolvedProviderStop, nil)
		d.notifyQuestionResolved(requestID, string(taskrunner.QuestionResolvedProviderStop))
		return taskrunner.QuestionOutcome{}, ctx.Err()

	case <-d.r.ctx.Done():
		if ans, ok := d.settleQuestion(requestID, pq); ok {
			return d.answered(requestID, ans), nil
		}
		d.recordQuestionResolved(requestID, taskrunner.QuestionResolvedRunEnded, nil)
		d.notifyQuestionResolved(requestID, string(taskrunner.QuestionResolvedRunEnded))
		return taskrunner.QuestionOutcome{}, d.r.ctx.Err()
	}
}

// answered records and announces a human's answer or decline.
func (d runPermissionDecider) answered(requestID string, ans taskrunner.QuestionAnswer) taskrunner.QuestionOutcome {
	res := taskrunner.QuestionResolvedByHuman
	var stored *taskrunner.QuestionAnswer
	if ans.Action == taskrunner.QuestionActionDecline {
		res = taskrunner.QuestionResolvedDeclined
	} else {
		scrubbed := scrubAnswerForHistory(ans)
		stored = &scrubbed
	}
	d.recordQuestionResolved(requestID, res, stored)
	d.notifyQuestionResolved(requestID, string(res))
	return taskrunner.QuestionOutcome{Answer: ans, Resolution: res}
}

// settleQuestion is the give-up path (timeout, provider cancel, run end): it
// removes the pending entry under the lock. If RespondQuestion already took
// the entry and delivered an answer in the instant the select fired, that
// answer wins and is returned, so a human who was told "answered" is never
// overruled by a timeout decline.
func (d runPermissionDecider) settleQuestion(requestID string, pq *pendingQuestion) (taskrunner.QuestionAnswer, bool) {
	d.r.mu.Lock()
	defer d.r.mu.Unlock()
	if _, still := d.r.pendingQuestions[requestID]; still {
		delete(d.r.pendingQuestions, requestID)
		return taskrunner.QuestionAnswer{}, false
	}
	select {
	case ans := <-pq.ch:
		return ans, true
	default:
		return taskrunner.QuestionAnswer{}, false
	}
}

// RespondQuestion answers runID's pending question requestID. The answer is
// validated against the request first (a bad answer leaves it parked); the
// first valid answer removes the entry under the same lock, so a second call
// or an answer after the decider gave up is a clear error, never a lost send.
func (reg *Registry) RespondQuestion(runID, requestID string, ans taskrunner.QuestionAnswer) error {
	r, err := reg.get(runID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	pq, ok := r.pendingQuestions[requestID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("runs: question request %q: %w", requestID, ErrNotFound)
	}
	if err := taskrunner.ValidateQuestionAnswer(pq.req, ans); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("runs: question request %q: %w", requestID, err)
	}
	select {
	case pq.ch <- ans:
		delete(r.pendingQuestions, requestID)
		r.mu.Unlock()
		return nil
	default:
		r.mu.Unlock()
		return fmt.Errorf("runs: question request %q already resolved", requestID)
	}
}

// PendingQuestion is one question request a run is waiting on.
type PendingQuestion struct {
	RunID     string
	TaskID    int64
	ChatID    int64
	RequestID string
	Request   taskrunner.QuestionRequest
}

// PendingQuestions lists every question a live run is waiting on, ordered by
// run then request id. It only reads: answering stays RespondQuestion.
func (reg *Registry) PendingQuestions() []PendingQuestion {
	reg.mu.Lock()
	rs := make([]*run, 0, len(reg.runs))
	for _, r := range reg.runs {
		rs = append(rs, r)
	}
	reg.mu.Unlock()

	var out []PendingQuestion
	for _, r := range rs {
		r.mu.Lock()
		for id, pq := range r.pendingQuestions {
			out = append(out, PendingQuestion{RunID: r.id, TaskID: r.taskID, ChatID: r.chatID, RequestID: id, Request: pq.req})
		}
		r.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RunID != out[j].RunID {
			return out[i].RunID < out[j].RunID
		}
		return out[i].RequestID < out[j].RequestID
	})
	return out
}

// questionRedactor scrubs what a human typed before it is kept in run history.
var questionRedactor = redact.New(nil, nil)

// answerWithheld replaces stored text that looks like a secret.
const answerWithheld = "[answer withheld: it looks like a secret]"

// scrubStoredText returns text for run history. Text the secret redactor would
// change, or that itself matches the secret vocabulary, is NEVER persisted: the
// whole value is replaced, not partially masked (ADR-0055 §6). Other text is
// kept.
func scrubStoredText(text string) string {
	if text != "" && (questionRedactor.Changed(text) || taskrunner.MentionsSecret(text)) {
		return answerWithheld
	}
	return text
}

// scrubAnswerForHistory returns ans with its free text and note made safe to
// store. The agent receives the answer verbatim (it is the human's intent);
// only the stored copy is withheld, so a credential typed into a question the
// secret check did not catch is not kept in history.
func scrubAnswerForHistory(ans taskrunner.QuestionAnswer) taskrunner.QuestionAnswer {
	out := ans
	out.Note = scrubStoredText(ans.Note)
	out.Answers = make([]taskrunner.QuestionAnswerItem, len(ans.Answers))
	for i, it := range ans.Answers {
		it.Text = scrubStoredText(it.Text)
		out.Answers[i] = it
	}
	return out
}

// closeOrphanQuestions appends question_resolved(daemon_restart) for every question
// request of a run the previous process left running that never got a
// resolution. A parked question lives only in memory, so after a restart it
// cannot be answered; without this its history row would stay open forever.
func closeOrphanQuestions(st *store.Store) error {
	ids, err := st.ListRunningRunIDs()
	if err != nil {
		return err
	}
	for _, id := range ids {
		rows, err := st.ListRunEvents(id)
		if err != nil {
			return err
		}
		open := map[string]bool{}
		var order []string
		var lastSeq int64
		for _, row := range rows {
			if row.Seq > lastSeq {
				lastSeq = row.Seq
			}
			e, err := decodeEvent(row.EventData)
			if err != nil || e.Question == nil {
				continue
			}
			switch e.Type {
			case taskrunner.EventTypeQuestionRequest:
				if !open[e.Question.RequestID] {
					order = append(order, e.Question.RequestID)
				}
				open[e.Question.RequestID] = true
			case taskrunner.EventTypeQuestionResolved:
				open[e.Question.RequestID] = false
			}
		}
		for _, reqID := range order {
			if !open[reqID] {
				continue
			}
			data, err := encodeEvent(taskrunner.Event{
				Type:     taskrunner.EventTypeQuestionResolved,
				Question: &taskrunner.QuestionEvent{RequestID: reqID, Resolution: taskrunner.QuestionResolvedDaemonRestart},
			})
			if err != nil {
				return err
			}
			lastSeq++
			if _, err := st.AppendRunEvent(id, lastSeq, data); err != nil {
				return err
			}
		}
	}
	return nil
}

// elicitationEnabled reports whether an ACP run may advertise the elicitation
// capability (ADR-0055 D4): a run with a profile whose setting is `ask`. A run
// with no profile, a system run and a daemon-owned seat never advertise (they
// would only decline), so an agent is never offered a channel nobody answers.
func (reg *Registry) elicitationEnabled(r *run) bool {
	if r.profileID == 0 || r.purpose != "" || reg.st == nil {
		return false
	}
	if _, seat := reg.seatSpec(r.taskID); seat {
		return false
	}
	p, err := reg.st.GetAgentProfile(r.profileID)
	return err == nil && p.Questions == QuestionsAsk
}
