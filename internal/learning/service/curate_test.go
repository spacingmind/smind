package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

type skillSpec struct{ name, cat, desc, body string }

// curateSkills are eight unrelated skills (low pairwise similarity).
var curateSkills = []skillSpec{
	{"learned-go-tempdir", "go", "Use when a Go test fails on a shared TempDir.",
		"Run the failing test alone with -race.\nCheck for a shared directory across parallel tests.\nGive each test its own directory."},
	{"learned-go-flaky-race", "go", "Use when a Go test is flaky under the race detector.",
		"Re-run the suite with -race and -count=5.\nLook for goroutines that outlive the test.\nJoin them before the test returns."},
	{"learned-docker-prune", "ops", "Use when disk fills up with unused container layers.",
		"List dangling images before deleting anything.\nPrune images older than a week.\nKeep the volumes of running services."},
	{"learned-sql-index", "db", "Use when a query scans a whole table on a large column.",
		"Run EXPLAIN on the slow query.\nAdd an index on the filtered column.\nRe-run EXPLAIN to confirm the index is used."},
	{"learned-git-bisect", "git", "Use when a regression appeared between two known commits.",
		"Mark the last good and the first bad commit.\nLet bisect check out the middle commit.\nRun the failing test at each step."},
	{"learned-css-grid", "web", "Use when cards overflow their container on narrow screens.",
		"Switch the card row to a wrapping grid.\nSet a minimum card width with minmax.\nCheck the layout at 320 pixels."},
	{"learned-http-retry", "net", "Use when an upstream call fails with a transient 503.",
		"Retry idempotent requests with exponential backoff.\nCap the attempts at three.\nNever retry a POST without an idempotency key."},
	{"learned-yaml-lint", "ci", "Use when a pipeline file is rejected for bad indentation.",
		"Run the linter on the single failing file.\nReplace tabs with two spaces.\nValidate the file against the schema before pushing."},
}

const humanMarker = "HUMAN-SECRET-RUNBOOK"

func (f *fx) landSkills(n int) []store.LearnedSkill {
	f.t.Helper()
	var out []store.LearnedSkill
	for _, sp := range curateSkills[:n] {
		k, err := f.st.LandSkill(store.LandSkillInput{
			Skill: store.LearnedSkill{WorkspaceID: f.ws.ID, Name: sp.name, Category: sp.cat, Description: sp.desc, Body: sp.body, Source: "reflector"},
			Actor: "reflector", Action: "create", // not a landing action: tests add landings explicitly
		})
		if err != nil {
			f.t.Fatal(err)
		}
		out = append(out, k)
	}
	return out
}

// landings appends n landing-action ledger rows stamped at the fake clock.
func (f *fx) landings(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		if err := f.st.AppendLedger(store.LearnedLedgerEntry{
			WorkspaceID: f.ws.ID, SkillName: "x", Action: "approve", Actor: "human", CreatedAt: f.clk.Now(),
		}); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fx) say(text string) {
	f.exec.mu.Lock()
	f.exec.fn = func(executor.Request) (executor.Result, error) {
		return executor.Result{RunID: "cur-1", FinalText: text}, nil
	}
	f.exec.mu.Unlock()
}

func (f *fx) curate() store.LearningJob {
	f.t.Helper()
	if err := f.svc.Curate(context.Background(), f.ws.ID); err != nil {
		f.t.Fatalf("Curate: %v", err)
	}
	f.svc.wg.Wait()
	return f.jobs()[0]
}

func (f *fx) proposalList() []store.LearnedProposal {
	f.t.Helper()
	ps, err := f.st.ListProposals(f.ws.ID, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return ps
}

func (f *fx) skillByName(name string) store.LearnedSkill {
	f.t.Helper()
	ks, err := f.st.ListLearnedSkills(f.ws.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, k := range ks {
		if k.Name == name && k.Status != store.SkillArchived {
			return k
		}
	}
	for _, k := range ks {
		if k.Name == name {
			return k
		}
	}
	f.t.Fatalf("skill %q not found", name)
	return store.LearnedSkill{}
}

func patchIntent(name, desc, oldS, newS string) map[string]any {
	return map[string]any{"action": "patch", "name": name, "category": "", "description": desc,
		"old_string": oldS, "new_string": newS, "reason": "merge the overlapping skills", "evidence": []any{}}
}

func deleteIntent(name, into string) map[string]any {
	return map[string]any{"action": "delete", "name": name, "absorbed_into": into,
		"reason": "its steps live in the umbrella now", "evidence": []any{}}
}

// mergeReply folds learned-go-tempdir into learned-go-flaky-race.
func mergeReply() string {
	return reply(
		patchIntent("learned-go-flaky-race", "Use when a Go test is flaky under the race detector or a shared TempDir.",
			"Join them before the test returns.", "Join them before the test returns.\nGive each parallel test its own directory."),
		deleteIntent("learned-go-tempdir", "learned-go-flaky-race"),
	)
}

func TestCurator_CadenceAndWeeklyLimit(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(8)
	f.clk.advance(time.Minute)
	f.landings(4)
	f.svc.scheduleCurators()
	f.svc.wg.Wait()
	if len(f.jobs()) != 0 {
		t.Fatalf("4 landings scheduled a job: %+v", f.jobs())
	}

	f.landings(1)
	f.svc.scheduleCurators()
	f.svc.wg.Wait()
	jobs := f.jobs()
	if len(jobs) != 1 || jobs[0].Kind != "curate" || jobs[0].TriggerKind != "schedule" || jobs[0].ChatID != nil || jobs[0].Status != "empty" {
		t.Fatalf("jobs = %+v, want exactly one scheduled empty curate job", jobs)
	}
	if f.exec.last().Purpose != "curate" || f.exec.last().Model != config.DefaultLearning().CuratorModel || f.exec.last().MaxTurns != 1 {
		t.Fatalf("request = %+v", f.exec.last())
	}

	// The same day, with enough new landings: the weekly limit holds.
	f.clk.advance(time.Hour)
	f.landings(5)
	f.svc.scheduleCurators()
	f.svc.wg.Wait()
	if len(f.jobs()) != 1 {
		t.Fatalf("second tick the same day scheduled: %d jobs", len(f.jobs()))
	}

	// Once 7 days passed, the 5 landings since the last job schedule again,
	// and not before they are 5.
	f.clk.advance(7 * 24 * time.Hour)
	f.svc.scheduleCurators()
	f.svc.wg.Wait()
	if len(f.jobs()) != 2 {
		t.Fatalf("5 landings after the interval: %d jobs, want 2", len(f.jobs()))
	}
	f.clk.advance(7*24*time.Hour + time.Minute)
	f.landings(4)
	f.svc.scheduleCurators()
	f.svc.wg.Wait()
	if len(f.jobs()) != 2 {
		t.Fatalf("4 landings after the interval scheduled: %d jobs", len(f.jobs()))
	}
}

func TestCurator_CadenceGuards(t *testing.T) {
	t.Run("7 skills never", func(t *testing.T) {
		f := newFx(t, "propose")
		f.landSkills(7)
		f.landings(20)
		f.svc.scheduleCurators()
		f.svc.wg.Wait()
		if len(f.jobs()) != 0 {
			t.Fatalf("7 skills scheduled: %+v", f.jobs())
		}
	})
	t.Run("archived skills do not count", func(t *testing.T) {
		f := newFx(t, "propose")
		ks := f.landSkills(8)
		if err := f.st.SetSkillStatus(ks[0].ID, store.SkillArchived, "human", "", ""); err != nil {
			t.Fatal(err)
		}
		f.landings(20)
		f.svc.scheduleCurators()
		f.svc.wg.Wait()
		if len(f.jobs()) != 0 {
			t.Fatalf("7 live skills scheduled: %+v", f.jobs())
		}
	})
	t.Run("mode off and kill switch", func(t *testing.T) {
		f := newFx(t, "off")
		f.landSkills(8)
		f.landings(20)
		f.svc.scheduleCurators()
		if err := f.st.SetWorkspaceLearningMode(f.ws.ID, "propose"); err != nil {
			t.Fatal(err)
		}
		f.setConfig(func(c *config.LearningConfig) { c.Enabled = false })
		f.svc.scheduleCurators()
		f.svc.wg.Wait()
		if len(f.jobs()) != 0 || f.exec.count() != 0 {
			t.Fatalf("scheduled while off/disabled: %+v", f.jobs())
		}
	})
	t.Run("active job blocks", func(t *testing.T) {
		f := newFx(t, "propose")
		f.landSkills(8)
		f.landings(20)
		if _, err := f.st.CreateLearningJob(store.LearningJob{WorkspaceID: f.ws.ID, Kind: "reflect", TriggerKind: "tail", Status: "running", CreatedAt: f.clk.Now()}); err != nil {
			t.Fatal(err)
		}
		f.svc.scheduleCurators()
		f.svc.wg.Wait()
		if len(f.jobs()) != 1 {
			t.Fatalf("scheduled behind an active job: %+v", f.jobs())
		}
	})
	t.Run("failed job does not start the weekly limit but backs off", func(t *testing.T) {
		f := newFx(t, "propose")
		f.landSkills(8)
		f.landings(5)
		f.say("not json")
		f.svc.scheduleCurators()
		f.svc.wg.Wait()
		if js := f.jobs(); len(js) != 1 || js[0].Status != "failed" {
			t.Fatalf("jobs = %+v, want one failed", js)
		}
		f.svc.scheduleCurators() // inside the cool-down: no retry storm
		f.svc.wg.Wait()
		if len(f.jobs()) != 1 {
			t.Fatalf("retried immediately after failure: %d jobs", len(f.jobs()))
		}
		f.clk.advance(time.Duration(f.config().CooldownMinutes+1) * time.Minute)
		f.say(`{"intents":[]}`)
		f.svc.scheduleCurators()
		f.svc.wg.Wait()
		if js := f.jobs(); len(js) != 2 || js[0].Status != "empty" {
			t.Fatalf("jobs = %+v, want a retry after the cool-down", js)
		}
	})
}

func TestCurator_SeesOnlyLearnedSkills(t *testing.T) {
	f := newFx(t, "propose")
	f.svc.d.Human = func(int64) []gate.LiveSkill {
		return []gate.LiveSkill{{Name: "human-deploy-" + humanMarker, Description: "Use when " + humanMarker, Human: true}}
	}
	ks := f.landSkills(3)
	archived, err := f.st.LandSkill(store.LandSkillInput{
		Skill: store.LearnedSkill{WorkspaceID: f.ws.ID, Name: "learned-old-thing", Description: "Use when old.", Body: "ARCHIVED-BODY-MARKER", Source: "reflector"}, Actor: "reflector",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetSkillStatus(archived.ID, store.SkillArchived, "human", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetSkillStatus(ks[2].ID, store.SkillDisabled, "human", "", ""); err != nil {
		t.Fatal(err)
	}
	f.curate()
	if f.exec.count() != 1 {
		t.Fatalf("executor calls = %d", f.exec.count())
	}
	req := f.exec.last()
	all := req.SystemPrompt + "\n" + req.UserPrompt
	for _, sp := range curateSkills[:3] { // the disabled one is shown too
		if !strings.Contains(req.UserPrompt, sp.body[:20]) || !strings.Contains(req.UserPrompt, sp.name) {
			t.Fatalf("learned skill %s missing from the prompt", sp.name)
		}
	}
	for _, bad := range []string{humanMarker, "human-deploy", "ARCHIVED-BODY-MARKER", "learned-old-thing"} {
		if strings.Contains(all, bad) {
			t.Fatalf("prompt leaks %q", bad)
		}
	}
	if req.SystemPrompt == "" || req.Purpose != "curate" {
		t.Fatalf("request = %+v", req)
	}
}

func TestCurator_MergeBundleApprovedAtomically(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(3)
	f.say(mergeReply())
	job := f.curate()
	if job.Status != "landed" || job.Error != "" || job.RunID != "cur-1" || job.ChatID != nil {
		t.Fatalf("job = %+v", job)
	}
	ps := f.proposalList()
	if len(ps) != 1 || ps[0].Kind != "bundle" || ps[0].Source != "curator" || ps[0].Name != "learned-go-flaky-race" || ps[0].Status != "pending" {
		t.Fatalf("proposals = %+v", ps)
	}
	items, err := approval.DecodeItems(ps[0].Intents)
	if err != nil || len(items) != 2 || items[0].Intent.Action != gate.ActionPatch || items[1].Intent.Action != gate.ActionDelete {
		t.Fatalf("items = %+v, %v", items, err)
	}
	umb, gone := f.skillByName("learned-go-flaky-race"), f.skillByName("learned-go-tempdir")
	if items[0].TargetSkillID != umb.ID || items[0].BaseVersion != umb.Version || items[1].TargetSkillID != gone.ID ||
		!strings.Contains(items[0].Result.Body, "Give each parallel test its own directory.") {
		t.Fatalf("items = %+v", items)
	}
	if len(f.notify.proposed) != 1 || f.notify.proposed[0] != "learned-go-flaky-race/bundle" {
		t.Fatalf("notified = %v", f.notify.proposed)
	}

	if _, err := approval.New(approval.Deps{Store: f.st}).Approve(ps[0].ID, approval.ApproveOptions{}); err != nil {
		t.Fatalf("approve bundle: %v", err)
	}
	umb2, gone2 := f.skillByName("learned-go-flaky-race"), f.skillByName("learned-go-tempdir")
	if umb2.Version != umb.Version+1 || !strings.Contains(umb2.Body, "own directory") || gone2.Status != store.SkillArchived {
		t.Fatalf("after approval: umbrella %+v absorbed %+v", umb2, gone2)
	}
	if other := f.skillByName("learned-docker-prune"); other.Status != store.SkillActive || other.Version != 1 {
		t.Fatalf("unrelated skill changed: %+v", other)
	}
}

func TestCurator_AbsorbedArchivedOnlyOnApproval(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(3)
	f.say(mergeReply())
	f.curate()
	assertUntouched := func(when string) {
		t.Helper()
		for _, sp := range curateSkills[:3] {
			k := f.skillByName(sp.name)
			if k.Status != store.SkillActive || k.Version != 1 || k.Body != sp.body {
				t.Fatalf("%s: %s changed: %+v", when, sp.name, k)
			}
		}
	}
	assertUntouched("after the job")
	ps := f.proposalList()
	if err := approval.New(approval.Deps{Store: f.st}).Reject(ps[0].ID, "no", "human"); err != nil {
		t.Fatal(err)
	}
	assertUntouched("after reject")
}

func TestLedger_MergeVsRetire(t *testing.T) {
	f := newFx(t, "propose")
	ks := f.landSkills(4)
	f.say(mergeReply())
	f.curate()
	ps := f.proposalList()
	if _, err := approval.New(approval.Deps{Store: f.st}).Approve(ps[0].ID, approval.ApproveOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetSkillStatus(ks[3].ID, store.SkillArchived, "human", "stale", ""); err != nil {
		t.Fatal(err)
	}
	led, err := f.st.ListLedger(f.ws.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var merges, merged, retired int
	for _, e := range led {
		switch {
		case e.Action == "merge" && e.SkillName == "learned-go-flaky-race":
			merges++
		case e.Action == "archive" && e.SkillName == "learned-go-tempdir" && e.AbsorbedInto == "learned-go-flaky-race":
			merged++
		case e.Action == "archive" && e.SkillName == "learned-sql-index" && e.AbsorbedInto == "":
			retired++
		}
	}
	if merges != 1 || merged != 1 || retired != 1 {
		t.Fatalf("ledger merge=%d archive-with-absorbed_into=%d plain-archive=%d, want 1/1/1: %+v", merges, merged, retired, led)
	}
}

func TestCurator_BadBundleMemberRejectsWholeBundle(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(4)
	f.say(reply(
		// The umbrella patch cites a code span no learned skill contains.
		patchIntent("learned-go-flaky-race", "Use when a Go test is flaky under the race detector or a shared TempDir.",
			"Join them before the test returns.", "Join them before the test returns.\nRun `rm -rf /` first."),
		deleteIntent("learned-go-tempdir", "learned-go-flaky-race"),
		// An independent description backfill is still fine.
		patchIntent("learned-sql-index", "Use when a query scans a whole table on a very large column.", "", ""),
	))
	job := f.curate()
	if job.Status != "landed" {
		t.Fatalf("job = %+v", job)
	}
	ps := f.proposalList()
	if len(ps) != 1 || ps[0].Kind != "patch" || ps[0].Name != "learned-sql-index" {
		t.Fatalf("proposals = %+v, want only the standalone patch", ps)
	}
	var vs []verdict
	if err := json.Unmarshal([]byte(job.Verdicts), &vs); err != nil || len(vs) != 3 {
		t.Fatalf("verdicts = %s, %v", job.Verdicts, err)
	}
	for _, v := range vs {
		switch v.Name {
		case "learned-go-flaky-race", "learned-go-tempdir":
			if v.Accepted || len(v.Findings) == 0 || v.ProposalID != 0 {
				t.Fatalf("bundle member %+v, want rejected with findings", v)
			}
		case "learned-sql-index":
			if !v.Accepted || v.ProposalID != ps[0].ID {
				t.Fatalf("standalone %+v, want accepted", v)
			}
		}
	}

	// And a bad delete rejects a good umbrella patch just the same.
	f2 := newFx(t, "propose")
	f2.landSkills(3)
	// Archive one absorbed skill between snapshot and run: the gate sees the
	// live library, so the delete of a non-live skill fails the bundle.
	f2.exec.mu.Lock()
	f2.exec.fn = func(executor.Request) (executor.Result, error) {
		d, _ := f2.st.ListLearnedSkills(f2.ws.ID)
		for _, k := range d {
			if k.Name == "learned-docker-prune" {
				_ = f2.st.SetSkillStatus(k.ID, store.SkillArchived, "human", "", "")
			}
		}
		return executor.Result{RunID: "cur-2", FinalText: reply(
			patchIntent("learned-go-flaky-race", "Use when a Go test is flaky under the race detector or a shared TempDir.",
				"Join them before the test returns.", "Join them before the test returns.\nGive each parallel test its own directory."),
			deleteIntent("learned-go-tempdir", "learned-go-flaky-race"),
			deleteIntent("learned-docker-prune", "learned-go-flaky-race"),
		)}, nil
	}
	f2.exec.mu.Unlock()
	job2 := f2.curate()
	if job2.Status != "empty" || len(f2.proposalList()) != 0 {
		t.Fatalf("job = %+v proposals = %+v, want empty and nothing created", job2, f2.proposalList())
	}
}

func TestCurator_StandalonePatchBecomesPatchProposal(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(4)
	f.say(reply(patchIntent("learned-sql-index", "Use when a query scans a whole table on a very large column.", "", "")))
	// A description-only patch carries a category backfill too.
	job := f.curate()
	if job.Status != "landed" {
		t.Fatalf("job = %+v", job)
	}
	ps := f.proposalList()
	if len(ps) != 1 || ps[0].Kind != "patch" || ps[0].Source != "curator" || ps[0].Name != "learned-sql-index" || ps[0].TargetSkillID == nil {
		t.Fatalf("proposals = %+v", ps)
	}
	items, _ := approval.DecodeItems(ps[0].Intents)
	k := f.skillByName("learned-sql-index")
	if len(items) != 1 || items[0].TargetSkillID != k.ID || items[0].BaseVersion != k.Version || !strings.Contains(items[0].Result.Description, "very large column") ||
		items[0].Result.Body != k.Body {
		t.Fatalf("items = %+v", items)
	}
	if k.Description != curateSkills[3].desc {
		t.Fatalf("skill changed by the job: %+v", k)
	}
}

func TestCurator_SnapshotTakenBeforeRunAndJobStopsIfSnapshotFails(t *testing.T) {
	t.Run("snapshot exists when the model runs", func(t *testing.T) {
		f := newFx(t, "propose")
		f.landSkills(3)
		var snaps []store.LearningSnapshot
		var jobID int64
		f.exec.fn = func(executor.Request) (executor.Result, error) {
			snaps, _ = f.st.ListLearningSnapshots(f.ws.ID)
			js, _ := f.st.ListLearningJobs(f.ws.ID, 1)
			jobID = js[0].ID
			return executor.Result{FinalText: `{"intents":[]}`}, nil
		}
		f.curate()
		if len(snaps) != 1 || snaps[0].Reason != "before curation job "+itoa(jobID) {
			t.Fatalf("snapshots at run time = %+v (job %d)", snaps, jobID)
		}
		full, err := f.st.GetLearningSnapshot(snaps[0].ID)
		if err != nil || !strings.Contains(full.Payload, "learned-go-tempdir") {
			t.Fatalf("snapshot payload = %q, %v", full.Payload, err)
		}
	})
	t.Run("a failed snapshot stops the job", func(t *testing.T) {
		f := newFx(t, "propose")
		f.landSkills(3)
		f.svc.snapshotFn = func(int64, string) error { return errors.New("disk full") }
		job := f.curate()
		if job.Status != "failed" || !strings.Contains(job.Error, "snapshot") || f.exec.count() != 0 {
			t.Fatalf("job = %+v, exec calls %d, want failed before any run", job, f.exec.count())
		}
	})
}

func TestCurator_BudgetSkipsWithoutStartingRun(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(3)
	rf := &reflectFx{fx: f}
	rf.learningRun("spent", &store.RunUsage{CostUSD: ptrF(f.config().DailyBudgetUSD), InputTokens: i64(1), OutputTokens: i64(1)})
	job := f.curate()
	if f.exec.count() != 0 || job.Status != "skipped" || job.SkipReason != "budget" {
		t.Fatalf("job = %+v exec calls %d, want skipped: budget", job, f.exec.count())
	}
	if snaps, _ := f.st.ListLearningSnapshots(f.ws.ID); len(snaps) != 0 {
		t.Fatalf("snapshot taken for a budget-skipped job: %d", len(snaps))
	}
}

func TestCurator_ExplicitBypassesCadenceNotBudget(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(3) // below CuratorMinSkills, no landings
	job := f.curate()
	if job.TriggerKind != "explicit" || job.Status != "empty" || f.exec.count() != 1 {
		t.Fatalf("explicit curate = %+v, exec calls %d", job, f.exec.count())
	}
	// A second explicit run right away is fine too (no weekly limit).
	if job := f.curate(); job.Status != "empty" || f.exec.count() != 2 {
		t.Fatalf("second explicit curate = %+v", job)
	}
	rf := &reflectFx{fx: f}
	rf.learningRun("spent", &store.RunUsage{CostUSD: ptrF(f.config().DailyBudgetUSD + 1), InputTokens: i64(1), OutputTokens: i64(1)})
	if job := f.curate(); job.SkipReason != "budget" || f.exec.count() != 2 {
		t.Fatalf("over budget = %+v, exec calls %d", job, f.exec.count())
	}
}

func TestCurator_ExplicitRefusals(t *testing.T) {
	f := newFx(t, "off")
	f.landSkills(3)
	if err := f.svc.Curate(context.Background(), f.ws.ID); !errors.Is(err, ErrModeOff) {
		t.Fatalf("mode off: %v", err)
	}
	if err := f.st.SetWorkspaceLearningMode(f.ws.ID, "propose"); err != nil {
		t.Fatal(err)
	}
	// Busy: block the executor, then a second trigger is refused.
	release := make(chan struct{})
	started := make(chan struct{})
	f.exec.fn = func(executor.Request) (executor.Result, error) {
		close(started)
		<-release
		return executor.Result{FinalText: `{"intents":[]}`}, nil
	}
	if err := f.svc.Curate(context.Background(), f.ws.ID); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := f.svc.Curate(context.Background(), f.ws.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("second curate: %v, want ErrBusy", err)
	}
	close(release)
	f.svc.wg.Wait()
}

func TestCurator_NotEnoughSkills(t *testing.T) {
	f := newFx(t, "propose")
	if err := f.svc.Curate(context.Background(), f.ws.ID); !errors.Is(err, ErrNotEnoughSkills) {
		t.Fatalf("no skills: %v", err)
	}
	ks := f.landSkills(2)
	if err := f.st.SetSkillStatus(ks[0].ID, store.SkillArchived, "human", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Curate(context.Background(), f.ws.ID); !errors.Is(err, ErrNotEnoughSkills) {
		t.Fatalf("one live skill: %v", err)
	}
	if len(f.jobs()) != 0 || f.exec.count() != 0 {
		t.Fatalf("jobs/exec created: %+v", f.jobs())
	}
}

func TestCurate_DoesNotRetriggerLearning(t *testing.T) {
	if learning.Eligible("curate", "") || learning.Eligible("curate", "task") || learning.Eligible("user", "system") {
		t.Fatal("curate runs / system tasks are eligible for collection")
	}
	f := newFx(t, "propose")
	f.landSkills(3)
	f.say(mergeReply())
	f.curate()
	// The curator's own run finishing, with plenty of tool calls, is ignored.
	f.svc.handleFinished(runs.RunFinished{RunID: "r", TaskID: f.chat.TaskID, ChatID: f.chat.ID, Provider: "claude-native", Purpose: "curate", ToolCalls: 500})
	_, sysChat, err := f.st.CreateSystemTask(f.ws.ID, "learning", "/cwd")
	if err != nil {
		t.Fatal(err)
	}
	f.svc.handleFinished(runs.RunFinished{RunID: "r2", TaskID: sysChat.TaskID, ChatID: sysChat.ID, Provider: "claude-native", Purpose: "user", ToolCalls: 500})
	f.svc.wg.Wait()
	for _, j := range f.jobs() {
		if j.Kind != "curate" {
			t.Fatalf("a %s job was started: %+v", j.Kind, j)
		}
	}
	if f.pending(f.chat) != 0 {
		t.Fatalf("pending = %d", f.pending(f.chat))
	}
}

func TestKillSwitch_StopsCurator(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(8)
	f.landings(10)
	f.setConfig(func(c *config.LearningConfig) { c.Enabled = false })
	if err := f.svc.Curate(context.Background(), f.ws.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Curate = %v, want ErrDisabled", err)
	}
	f.svc.scheduleCurators()
	if len(f.jobs()) != 0 || f.exec.count() != 0 {
		t.Fatal("curator acted with the kill switch off")
	}
	// A job queued before the switch flipped ends skipped: disabled.
	job, err := f.st.CreateLearningJob(store.LearningJob{WorkspaceID: f.ws.ID, Kind: "curate", TriggerKind: "schedule", Status: "queued", CreatedAt: f.clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.runJob(job.ID)
	got, _ := f.st.GetLearningJob(job.ID)
	if got.Status != "skipped" || got.SkipReason != "disabled" || f.exec.count() != 0 {
		t.Fatalf("job = %+v", got)
	}
}

func TestCurator_ParseFailureMarksJobFailed(t *testing.T) {
	f := newFx(t, "propose")
	f.landSkills(3)
	junk := strings.Repeat("SECRET-MODEL-OUTPUT ", 100)
	f.say(junk)
	job := f.curate()
	if job.Status != "failed" || job.Error == "" || len(job.Error) > 200 || job.FinishedAt == nil || len(f.proposalList()) != 0 {
		t.Fatalf("job = %+v", job)
	}

	// A reply that touches a skill the model was never shown fails too.
	f.say(reply(patchIntent("learned-never-shown", "Use when something.", "a", "b")))
	if job := f.curate(); job.Status != "failed" {
		t.Fatalf("scope failure job = %+v", job)
	}
	// So does a create (the curator may not create).
	f.say(reply(map[string]any{"action": "create", "name": "learned-new-thing", "description": "Use when x.", "body": "one\ntwo", "reason": "r", "evidence": []any{}}))
	if job := f.curate(); job.Status != "failed" {
		t.Fatalf("create job = %+v", job)
	}
	// An executor error is clipped and failed.
	f.exec.mu.Lock()
	f.exec.fn = func(executor.Request) (executor.Result, error) { return executor.Result{}, errors.New(junk) }
	f.exec.mu.Unlock()
	if job := f.curate(); job.Status != "failed" || len(job.Error) > 200 {
		t.Fatalf("executor failure job = %+v", job)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
