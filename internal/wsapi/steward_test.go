package wsapi

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// bindSteward installs the handlers of the env's host (fake collector,
// enabled rails) over the API's real steward wiring, so a test drives the
// real handler funcs without the daemon's config.
func (e *stewardHostEnv) bindSteward(t *testing.T) {
	t.Helper()
	for m, h := range stewardHandlers(e.host) {
		e.api.hs[m] = h
	}
}

// setInputs swaps the fake collector's Inputs (stewardTestInputs leaves the
// not_stacked and pr_hygiene gates failing).
func (e *stewardHostEnv) setInputs(in steward.Inputs) {
	e.coll.mu.Lock()
	e.coll.inputs = in
	e.coll.mu.Unlock()
}

// greenInputs is a digest every gate of which passes, so an ACCEPTED seat
// verdict is stored as ACCEPTED.
func greenInputs(ci string) steward.Inputs {
	in := stewardTestInputs(ci)
	in.Stacking = steward.StackInfo{BaseReachableFromDevelop: true}
	in.PR = &steward.PRInfo{Number: 7, Base: "develop", Title: "feat: one", Body: "validated: ok\nnot done: nothing"}
	return in
}

// seatToken reads the one-time token out of the host's in-memory seat entry,
// the only place it exists.
func (e *stewardHostEnv) seatToken(t *testing.T, stewardTaskID int64) string {
	t.Helper()
	spec, ok := e.host.seatSource(stewardTaskID)
	if !ok {
		t.Fatal("no seat spec for the seat task")
	}
	return spec.Env[steward.EnvToken]
}

// seatRC is the request context of the seat's own run.
func seatRC(row store.StewardReview) *requestContext {
	return &requestContext{runScoped: true, ident: runIdentity{runID: row.RunID, taskID: row.StewardTaskID}}
}

// acceptedVerdict builds a complete ACCEPTED verdict for review id against
// its stored digest: every gate acknowledged, an observation per frame, no
// findings.
func acceptedVerdict(t *testing.T, e *stewardHostEnv, reviewID int64) json.RawMessage {
	t.Helper()
	row := e.row(t, reviewID)
	var d steward.Digest
	if err := json.Unmarshal([]byte(row.DigestJSON), &d); err != nil {
		t.Fatalf("digest of %d: %v", reviewID, err)
	}
	gates := make([]steward.GateAck, 0, len(d.Gates))
	for _, g := range d.Gates {
		gates = append(gates, steward.GateAck{ID: g.ID, Ack: true})
	}
	frames := make([]steward.FrameObservation, 0, len(d.Frames.Items))
	for _, f := range d.Frames.Items {
		frames = append(frames, steward.FrameObservation{Path: f.Path, Observation: "the frame matches the diff and looks right"})
	}
	raw, err := json.Marshal(steward.SeatVerdict{V: 1, ReviewID: reviewID, Verdict: steward.VerdictAccepted,
		Summary: "every gate holds and the diff is sound", Gates: gates, Frames: frames})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// reviewViaRPC starts a review through the bound handler as a human caller.
func (e *stewardHostEnv) reviewViaRPC(t *testing.T, rc *requestContext) reviewResult {
	t.Helper()
	res, err := budgetCall(t, e.api, rc, "steward.review", map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatalf("steward.review: %v", err)
	}
	var r reviewResult
	if err := json.Unmarshal(res, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReview_OnlyHumanOrDaemon(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.bindSteward(t)
	for i, rc := range []*requestContext{{local: true}, {local: false}} {
		r := e.reviewViaRPC(t, rc)
		if r.ReviewID == 0 || r.TaskID != e.seatTask.ID || r.Status != "running" || r.RunID == "" || r.StewardTaskID == 0 {
			t.Fatalf("caller %d (%+v): result = %+v", i, rc, r)
		}
		e.endReview(t, r.ReviewID)
	}
}

func TestReview_RefusedForRunScopedAndAutomationOrigin(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.bindSteward(t)
	// A run-scoped caller is an agent: a seat, a Peer, or a task an
	// automation started (automation runs identify the same way), so this
	// one refusal covers the automation origin too.
	agent := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	for _, m := range []string{"steward.review", "steward.cancel", "steward.get", "steward.list"} {
		_, err := budgetCall(t, e.api, agent, m, map[string]any{"taskId": e.seatTask.ID, "reviewId": 1})
		wantErrCode(t, err, codeForbidden)
	}
	// An identified connection is refused one gate earlier: none of these is
	// in RunScopedMethods.
	ws := e.identified(t)
	for _, m := range []string{"steward.review", "steward.cancel", "steward.get", "steward.list"} {
		wantForbidden(t, callWS(t, ws, m, map[string]any{}), "not available to agents")
	}
	if rows, _ := e.db.ListStewardReviews(e.seatTask.ID, 10); len(rows) != 0 {
		t.Fatalf("a refused call opened a review: %+v", rows)
	}
}

func TestVerdictSubmit_LeadCannotWrite(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)
	token := e.seatToken(t, row.StewardTaskID)
	verdict := acceptedVerdict(t, e, r.ReviewID)
	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}

	// The Lead's run holds the CORRECT token and still cannot write: its
	// task is not the seat's, and the refusal happens before the token is
	// even compared.
	_, err := budgetCall(t, e.api, lead, "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": token, "verdict": verdict})
	wantErrCode(t, err, codeForbidden)
	// The seat's own run with a wrong token is refused too.
	_, err = budgetCall(t, e.api, seatRC(row), "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": "not-the-token", "verdict": verdict})
	wantErrCode(t, err, codeForbidden)

	after := e.row(t, r.ReviewID)
	if after.Status != "running" || after.Verdict != "" || after.SeatVerdict != "" || after.TokenUsed {
		t.Fatalf("a refused call stored something: %+v", after)
	}
	e.endReview(t, r.ReviewID)
}

func TestVerdictSubmit_SeatRunSucceedsOnce(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)
	token := e.seatToken(t, row.StewardTaskID)

	res, err := budgetCall(t, e.api, seatRC(row), "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": token, "verdict": acceptedVerdict(t, e, r.ReviewID)})
	if err != nil {
		t.Fatal(err)
	}
	var v steward.View
	if err := json.Unmarshal(res, &v); err != nil {
		t.Fatal(err)
	}
	if v.Status != "done" || v.Verdict != steward.VerdictAccepted || v.SeatVerdict != steward.VerdictAccepted {
		t.Fatalf("view = %+v", v)
	}
	stored := e.row(t, r.ReviewID)
	if stored.Status != "done" || stored.Verdict != steward.VerdictAccepted || !stored.TokenUsed {
		t.Fatalf("row = %+v", stored)
	}

	// The token is one-time: the same call again is invalid, not forbidden.
	_, err = budgetCall(t, e.api, seatRC(row), "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": token, "verdict": acceptedVerdict(t, e, r.ReviewID)})
	wantErrCode(t, err, codeInvalid)

	// A gate that fails clamps the seat's ACCEPTED down: a new review key
	// (CI failure) opens a second review whose verdict is stored capped.
	e.setInputs(greenInputs("failure"))
	r2 := e.reviewViaRPC(t, &requestContext{local: true})
	row2 := e.waitSeatStarted(t, r2.ReviewID)
	res2, err := budgetCall(t, e.api, seatRC(row2), "steward.verdict.submit", map[string]any{"reviewId": r2.ReviewID, "token": e.seatToken(t, row2.StewardTaskID), "verdict": acceptedVerdict(t, e, r2.ReviewID)})
	if err != nil {
		t.Fatal(err)
	}
	var v2 steward.View
	if err := json.Unmarshal(res2, &v2); err != nil {
		t.Fatal(err)
	}
	if v2.SeatVerdict != steward.VerdictAccepted || v2.Verdict != steward.VerdictChanges {
		t.Fatalf("clamped view = %+v", v2)
	}
	if stored2 := e.row(t, r2.ReviewID); !strings.Contains(stored2.Summary, "clamped") {
		t.Fatalf("summary = %q, want the clamp reason", stored2.Summary)
	}
	_ = e.api.Runs.Stop(row.RunID)
	_ = e.api.Runs.Stop(row2.RunID)
}

func TestVerdictSubmit_RequiresToken(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)
	verdict := acceptedVerdict(t, e, r.ReviewID)
	for _, tok := range []string{"", "   "} {
		_, err := budgetCall(t, e.api, seatRC(row), "steward.verdict.submit", map[string]any{"reviewId": r.ReviewID, "token": tok, "verdict": verdict})
		wantErrCode(t, err, codeForbidden)
	}
	if after := e.row(t, r.ReviewID); after.Verdict != "" || after.TokenUsed {
		t.Fatalf("a tokenless call stored something: %+v", after)
	}
	e.endReview(t, r.ReviewID)
}

func TestDigestGet_OnlySeatOwnRun(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	e.setInputs(greenInputs("success"))
	e.bindSteward(t)
	r := e.reviewViaRPC(t, &requestContext{local: true})
	row := e.waitSeatStarted(t, r.ReviewID)
	token := e.seatToken(t, row.StewardTaskID)

	raw, err := budgetCall(t, e.api, seatRC(row), "steward.digest.get", map[string]any{"reviewId": r.ReviewID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"gates"`) {
		t.Fatalf("digest = %s", raw)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("the digest carries the one-time token")
	}

	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	_, err = budgetCall(t, e.api, lead, "steward.digest.get", map[string]any{"reviewId": r.ReviewID})
	wantErrCode(t, err, codeForbidden)
	other := &requestContext{runScoped: true, ident: runIdentity{runID: "run-unrelated", taskID: e.seatTask.ID + 1000}}
	_, err = budgetCall(t, e.api, other, "steward.digest.get", map[string]any{"reviewId": r.ReviewID})
	wantErrCode(t, err, codeForbidden)
	_, err = budgetCall(t, e.api, &requestContext{local: true}, "steward.digest.get", map[string]any{"reviewId": r.ReviewID})
	wantErrCode(t, err, codeForbidden)

	// A finished review hands its seat nothing either.
	e.endReview(t, r.ReviewID)
	_, err = budgetCall(t, e.api, seatRC(row), "steward.digest.get", map[string]any{"reviewId": r.ReviewID})
	wantErrCode(t, err, codeForbidden)
}

// TestNoStewardMergeRPC is the steward twin of validation's TestNoMergeRPC
// (that name is taken in validation_test.go).
func TestNoStewardMergeRPC(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	var got []string
	for name := range e.api.hs {
		seg := name
		if i := strings.LastIndex(name, "."); i >= 0 {
			seg = name[i+1:]
		}
		// These predate steward and are not review authority: pairing and
		// approving fleet hosts / relay devices, and approving learned-skill
		// proposals. None is callable by an agent.
		preexisting := map[string]bool{
			"relay.device.approve": true, "fleet.host.approve": true, "learning.proposals.approve": true,
		}
		switch strings.ToLower(seg) {
		case "merge", "approve", "accept":
			if !preexisting[name] {
				t.Errorf("no merge/approve/accept RPC may exist, found %q", name)
			}
		}
		if strings.HasPrefix(name, "steward.") {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	want := append([]string(nil), stewardMethods...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("steward handlers = %v, want exactly %v", got, stewardMethods)
	}
	var scoped []string
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "steward.") {
			scoped = append(scoped, m)
		}
	}
	if strings.Join(scoped, ",") != "steward.digest.get,steward.verdict.submit" {
		t.Fatalf("run-scoped steward methods = %v, want only the seat's two", scoped)
	}
}

func TestStewardTopicInEventAllowlist(t *testing.T) {
	t.Parallel()
	if !knownTopics[TopicStewardUpdated] {
		t.Fatalf("%q is not in the events.subscribe allowlist", TopicStewardUpdated)
	}
	raw, err := json.Marshal(stewardUpdatedPayload{TaskID: 1, ReviewID: 2, Status: "pending"})
	if err != nil || string(raw) != `{"taskId":1,"reviewId":2,"status":"pending"}` {
		t.Fatalf("payload = %s (%v)", raw, err)
	}
}

func TestStewardObserver_MapsRunFinishedToTrigger(t *testing.T) {
	t.Parallel()
	e := newStewardHostEnv(t, "success")
	lead, err := e.db.CreateAgentProfile(store.AgentProfile{Name: "lead", Provider: "glm"})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.db.CreateAgentProfile(store.AgentProfile{Name: "peer-sonnet", Provider: "glm"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.StewardConfig{Enabled: true, AutoReview: []string{"W"}} // newTestTask's workspace title
	var mu sync.Mutex
	var fired []int64
	o := &stewardObserver{host: e.host, db: e.db,
		trigger: steward.NewAutoTrigger(func() (config.StewardConfig, error) { return cfg, nil },
			func(taskID int64) { mu.Lock(); fired = append(fired, taskID); mu.Unlock() })}

	mk := func(id string, taskID, profileID int64, status string) runs.RunFinished {
		t.Helper()
		if _, err := e.db.CreateRun(store.Run{ID: id, TaskID: taskID, Provider: "glm", Prompt: "p", Status: status, StartedAt: time.Now().UTC(), ProfileID: profileID}); err != nil {
			t.Fatal(err)
		}
		return runs.RunFinished{RunID: id, TaskID: taskID, Status: status}
	}

	// A done root run of the lead profile, with a branch and a head, in an
	// opted-in workspace: exactly one fire, with the task id.
	o.OnRunFinished(mk("run-obs-1", e.seatTask.ID, lead.ID, "done"))
	// The same run id twice: still once (the trigger remembers it).
	o.OnRunFinished(runs.RunFinished{RunID: "run-obs-1", TaskID: e.seatTask.ID, Status: "done"})
	// An errored run, a child task's run, a non-lead profile and steward off:
	// none of them fire.
	o.OnRunFinished(mk("run-obs-err", e.seatTask.ID, lead.ID, "error"))
	child, err := e.wm.CreateTask(e.seatTask.WorkspaceID, e.seatTask.SpaceID, "child", workspace.WithParentTask(e.seatTask.ID))
	if err != nil {
		t.Fatal(err)
	}
	o.OnRunFinished(mk("run-obs-child", child.ID, lead.ID, "done"))
	o.OnRunFinished(mk("run-obs-peer", e.seatTask.ID, peer.ID, "done"))
	cfg.Enabled = false
	o.OnRunFinished(mk("run-obs-off", e.seatTask.ID, lead.ID, "done"))

	mu.Lock()
	got := append([]int64(nil), fired...)
	mu.Unlock()
	if len(got) != 1 || got[0] != e.seatTask.ID {
		t.Fatalf("fired = %v, want exactly [%d]", got, e.seatTask.ID)
	}
	if newStewardObserver(e.host, e.db).trigger == nil {
		t.Fatal("the daemon's observer carries no trigger")
	}
}

func TestAdmission_LeadCannotLaunchStewardProfile(t *testing.T) {
	t.Parallel()
	no := false
	seat := store.AgentProfile{ChildPolicy: "leaf"}
	if mayLaunch(seat, store.AgentProfile{Name: steward.SeatProfileName, OrchestrationTools: &no}) {
		t.Fatal("a Lead must never launch the steward seat")
	}
	if !mayLaunch(seat, store.AgentProfile{Name: "peer-sonnet", OrchestrationTools: &no}) {
		t.Fatal("a leaf Lead may launch a peer profile")
	}
}
