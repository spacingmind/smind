package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeReviewHead is the head sha a STALE seeded review carries (40 hex
// chars): it is not the task's real head, so the daemon reads "head moved".
const fakeReviewHead = "0123456789abcdef0123456789abcdef01234567"

// writeFakeFrame writes a small valid PNG, 390 px wide, of one flat colour.
func writeFakeFrame(path string, c color.Color) (int64, error) {
	img := image.NewRGBA(image.Rect(0, 0, 390, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 390; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// seedReview handles seed-review <taskId> <ACCEPTED|CHANGES|BLOCKED> [stale]:
// a FINISHED fake steward review of the task, written straight through the
// store (no model call, no steward run). The frames are real PNG files under
// the harness work dir, so steward.frame serves them. A fresh review carries
// the task's REAL worktree head (the daemon compares it on every read; a
// head that differs reads stale and drops out of the inbox). "stale" stores a
// head that is not the task's, so steward.View reads "head moved". The reply
// carries "review-id". An optional trailing `axes=<scenario>` makes it a
// two-axis review (ADR-0054): both-accepted, disagreement, criteria, skipped,
// criteria-many, blocked-axis, running, partial; the pass rows, their findings and the
// standards-changed hint are written through the store like the verdict is.
func (h *harness) seedReview(args []string) (map[string]any, error) {
	if err := need(args, 2, "seed-review <taskId> <ACCEPTED|CHANGES|BLOCKED> [stale]"); err != nil {
		return nil, err
	}
	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad taskId %q", args[0])
	}
	verdict := args[1]
	switch verdict {
	case steward.VerdictAccepted, steward.VerdictChanges, steward.VerdictBlocked:
	default:
		return nil, fmt.Errorf("bad verdict %q (want ACCEPTED|CHANGES|BLOCKED)", verdict)
	}
	stale, scenario := false, ""
	for _, a := range args[2:] {
		if a == "stale" {
			stale = true
		}
		if v, ok := strings.CutPrefix(a, "axes="); ok {
			scenario = v
		}
	}
	task, err := h.store.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	head := fakeReviewHead
	if !stale && task.WorktreePath != nil {
		if real, err := workspace.GitHead(context.Background(), *task.WorktreePath); err == nil {
			head = real
		}
	}

	dir, err := os.MkdirTemp(h.workDir, "steward-frames-")
	if err != nil {
		return nil, err
	}
	var items []steward.FrameInfo
	for _, f := range []struct {
		name, theme string
		c           color.Color
	}{
		{"inbox-light-390.png", "light", color.RGBA{0xf4, 0xf4, 0xf5, 0xff}},
		{"review-dark-390.png", "dark", color.RGBA{0x1c, 0x1c, 0x1e, 0xff}},
		{"form-light-390.png", "light", color.RGBA{0xe8, 0xee, 0xf7, 0xff}},
	} {
		size, err := writeFakeFrame(filepath.Join(dir, f.name), f.c)
		if err != nil {
			return nil, err
		}
		items = append(items, steward.FrameInfo{Path: filepath.Join(dir, f.name), SizeBytes: size,
			CapturedSHA: head, Viewport: 390, Theme: f.theme})
	}

	// An ACCEPTED review cannot carry a failing gate (the daemon clamps it to CHANGES), so no_secrets passes there.
	secretsGate := steward.Gate{ID: "no_secrets", State: steward.GateFail, Detail: "1 possible token in mobile/src/api.ts"}
	if verdict == steward.VerdictAccepted {
		secretsGate = steward.Gate{ID: "no_secrets", State: steward.GatePass, Detail: "no token-shaped strings"}
	}
	// Passing gates first, the failing one after them: the screen is the one
	// that must show failing gates first.
	gates := []steward.Gate{
		{ID: "ci", State: steward.GatePass},
		{ID: "validator", State: steward.GatePass, Detail: "contract v2 satisfied"},
		{ID: "frames_fresh", State: steward.GateUnknown, Detail: "no frame matrix declared"},
		{ID: "no_binaries", State: steward.GatePass},
		secretsGate,
		{ID: "not_stacked", State: steward.GateNA},
		{ID: "pr_hygiene", State: steward.GatePass, Detail: "title conventional, Validated section present"},
	}
	dg := steward.Digest{
		V:      1,
		Task:   steward.TaskInfo{ID: taskID, HeadSHA: head},
		PR:     &steward.PRSummary{Number: 482, Base: "develop", TitleConventional: true, HasValidated: true},
		Frames: steward.FramesSection{Dir: dir, Items: items},
		Gates:  gates,
	}
	if scenario == "disagreement" {
		dg.Standards = &steward.StandardsSection{BaseRef: strings.Repeat("b", 40), ChangedInDiff: true,
			Files: []steward.StandardsFile{{Path: "AGENTS.md", ChangedInDiff: true}}}
	}
	digest, err := json.Marshal(dg)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(digest)

	row, err := h.store.InsertStewardReview(store.StewardReview{TaskID: taskID, TriggerKind: steward.TriggerHuman,
		HeadSHA: head, Status: "pending"}, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := h.store.SetStewardReviewDigest(row.ID, string(digest), hex.EncodeToString(sum[:]), "success", "", "", 0); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	switch verdict {
	case steward.VerdictBlocked:
		err = h.store.SetStewardReviewBlocked(row.ID, "timeout", "The steward ran out of time before it could judge this change.", 0.12, now)
	default:
		var findings []steward.SeatFinding
		summary := "Frames and gates agree with the PR; nothing to change."
		if verdict == steward.VerdictChanges {
			summary = "Two files need work: a token-shaped string and an unguarded retry."
			findings = []steward.SeatFinding{
				{File: "mobile/src/api.ts", Line: 41, Severity: "P1", Check: "no_secrets",
					Why: "A bearer-token-shaped string is committed in the client constants."},
				{File: "mobile/src/api.ts", Line: 88, Check: "retry-bounded",
					Why: "The retry loop has no upper bound and no backoff, so an offline phone spins forever while the user watches a frozen banner."},
				{File: "mobile/src/screens/review.tsx", Line: 12, Severity: "P2",
					Why:   strings.Repeat("The failing gates are listed after the passing ones, which hides the one thing a reviewer must read first on a narrow screen. ", 4),
					Check: "Open the review on a 390 px device, scroll to the gates section and confirm the failing gate is the first row, then re-run the frame matrix for the light and dark themes and compare each pair side by side."},
			}
		}
		var fj []byte
		var merr error
		if scenario != "" {
			af, sum := axisSeedFindings(scenario)
			fj, merr = json.Marshal(af)
			if sum != "" {
				summary = sum
			}
		} else {
			fj, merr = json.Marshal(append([]steward.SeatFinding{}, findings...))
		}
		if merr != nil {
			return nil, merr
		}
		gj, merr := json.Marshal(gates)
		if merr != nil {
			return nil, merr
		}
		err = h.store.SetStewardReviewVerdict(row.ID, verdict, verdict, string(fj), string(gj), summary, 0.34, now)
	}
	if err != nil {
		return nil, err
	}
	if scenario != "" {
		if err := h.seedAxes(row.ID, scenario); err != nil {
			return nil, err
		}
	}
	return map[string]any{"review-id": row.ID, "reviewId": row.ID, "taskId": taskID, "verdict": verdict, "stale": stale, "headSha": head}, nil
}

// seedAudit handles seed-audit <taskId> <n>: appends n fake policy_audit rows
// (a rotating mix of decisions and rule ids, one empty rule id, one 300-char
// target). The stage writes the policy file and config.yaml itself, like
// 75-plugins.mjs does: config.yaml must hold steward.policy.enabled: true and
// $SMIND_HOME/steward/policy.yaml two rules (e.g. git-status allow, deny-force-push
// deny) for policy.get to answer.
func (h *harness) seedAudit(args []string) (map[string]any, error) {
	if err := need(args, 2, "seed-audit <taskId> <n>"); err != nil {
		return nil, err
	}
	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad taskId %q", args[0])
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 1 || n > 500 {
		return nil, errors.New("seed-audit: n must be 1..500")
	}
	if _, err := h.store.GetTask(taskID); err != nil {
		return nil, err
	}
	type spec struct{ decision, rule, tool, target string }
	specs := []spec{
		{"allow", "git-status", "execute", "git status"},
		{"deny", "deny-force-push", "execute", "git push --force origin main"},
		{"ask", "", "execute", "npm install left-pad"},
		{"ask_human_allow", "", "execute", "make deploy"},
		{"ask_timeout_deny", "", "execute", "rm -rf build"},
		{"allow", "git-status", "execute", "git status --short"},
		{"deny", "deny-force-push", "execute", "git push -f"},
		{"ask", "git-status", "edit", "docs/" + strings.Repeat("very-long-segment/", 16) + "file.md"},
	}
	// A 300-char target on the first row of the second lap would be easy to
	// miss in a short seed, so one of the first rows carries it.
	specs[2].target = strings.Repeat("x", 300)
	base := time.Now().UTC().Add(-time.Duration(n) * time.Minute)
	last := int64(0)
	for i := 0; i < n; i++ {
		s := specs[i%len(specs)]
		id, err := h.store.AppendPolicyAudit(store.PolicyAuditRow{
			CreatedAt: base.Add(time.Duration(i) * time.Minute), TaskID: taskID,
			RunID: "seed-run", RequestID: fmt.Sprintf("seed-req-%d", i), Seat: "coder",
			Tool: s.tool, Target: s.target, Decision: s.decision, RuleID: s.rule,
			PolicySha: "seedpolicysha", Reasons: "seeded by the dev harness", LatencyMs: int64(2 + i%7),
		})
		if err != nil {
			return nil, err
		}
		last = id
	}
	return map[string]any{"taskId": taskID, "rows": n, "lastId": last}, nil
}

// axisSeedFindings are the tagged findings (and the raw-tag combined summary the
// daemon writes) of a two-axis scenario.
func axisSeedFindings(scenario string) ([]steward.AxisFinding, string) {
	switch scenario {
	case "disagreement":
		return []steward.AxisFinding{
			{SeatFinding: steward.SeatFinding{File: "internal/steward/combine.go", Line: 58, Severity: "P2", Check: "go test ./internal/steward -run TestCombine_NotRun",
				Why: "New production code has no test for the not_run branch."}, Axis: steward.AxisStandards, Rule: ".smind-standards/AGENTS.md: \"tests for production code\""},
			{SeatFinding: steward.SeatFinding{File: "internal/wsapi/steward_passes.go", Line: 301, Severity: "P3", Check: "read the comment against the code",
				Why: "A comment states an invariant the code does not enforce."}, Axis: steward.AxisStandards, Rule: ".smind-standards/AGENTS.md: \"comments state what the code does\""},
		}, "[standards] One rule is not followed. [spec] Every criterion is met."
	case "criteria", "criteria-many":
		return []steward.AxisFinding{{SeatFinding: steward.SeatFinding{File: "internal/wsapi/steward.go", Line: 97, Severity: "P1", Check: "go test ./internal/wsapi -run TestTwoAxisE2E_AxisRerunValidation",
			Why: "An unknown axis name is accepted by steward.review."}, Axis: steward.AxisSpec}}, "[spec] One criterion is not met and one cannot be verified."
	case "skipped":
		return []steward.AxisFinding{}, "[standards] Follows the documented standards. [spec] axis spec skipped"
	case "blocked-axis":
		return []steward.AxisFinding{}, "[spec] Every criterion is met. [axis standards did not complete (timeout)]"
	case "partial":
		return []steward.AxisFinding{}, "[standards] Follows the documented standards. [axis spec did not complete (not_run)]"
	default:
		return []steward.AxisFinding{}, "[standards] Follows the documented standards. [spec] Every acceptance criterion is met."
	}
}

// seedAxes writes the two pass rows of a two-axis scenario for review id.
func (h *harness) seedAxes(id int64, scenario string) error {
	now := time.Now().UTC()
	pass := func(axis steward.Axis, model string) error {
		_, err := h.store.InsertStewardReviewPass(store.StewardReviewPass{ReviewID: id, Axis: string(axis), Status: steward.PassRunning, Profile: "steward", Model: model}, now)
		return err
	}
	verdict := func(axis steward.Axis, v, summary string, criteria []steward.CriterionResult) error {
		cj, err := json.Marshal(criteria)
		if err != nil {
			return err
		}
		return h.store.SetStewardPassVerdict(id, string(axis), v, summary, "[]", string(cj), 0.3, now)
	}
	const haiku, sonnet = "claude-haiku-5-5", "claude-sonnet-5-5"
	met := func(cid, ev string) steward.CriterionResult {
		return steward.CriterionResult{ID: cid, Status: steward.CriterionMet, Evidence: ev}
	}
	specOK := []steward.CriterionResult{
		met("AC1", "internal/steward/combine.go implements the min over the axes; TestCombine_MinOverAxes covers it."),
		met("AC2", "TestTwoAxisOff_ReviewIsByteIdentical pins the flag-off output."),
	}
	for _, axis := range []struct {
		a steward.Axis
		m string
	}{{steward.AxisStandards, haiku}, {steward.AxisSpec, sonnet}} {
		if err := pass(axis.a, axis.m); err != nil {
			return err
		}
	}
	stdOK := func() error {
		return verdict(steward.AxisStandards, steward.VerdictAccepted, "Commit messages, naming and layering follow AGENTS.md.", nil)
	}
	specAccepted := func() error {
		return verdict(steward.AxisSpec, steward.VerdictAccepted, "Both acceptance criteria are covered by the diff and its tests.", specOK)
	}
	switch scenario {
	case "disagreement":
		if err := verdict(steward.AxisStandards, steward.VerdictChanges, "The change adds production code without a test, which AGENTS.md requires.", nil); err != nil {
			return err
		}
		return specAccepted()
	case "criteria", "criteria-many":
		if err := stdOK(); err != nil {
			return err
		}
		crit := []steward.CriterionResult{
			met("AC1", "The inbox item carries axes; TestInbox_ReviewItemCarriesAxesAdditively covers it."),
			{ID: "AC2", Status: steward.CriterionUnmet, Evidence: "steward.review accepts an unknown axis name without refusing it."},
			{ID: "AC3", Status: steward.CriterionUnverifiable, Evidence: "Light and dark frames are required but none were captured at this commit."},
		}
		if scenario == "criteria-many" {
			crit = append(crit,
				met("AC4", "The CLI prints the AXES block; TestStewardShow_PrintsAxesBlock covers it."),
				met("AC5", "The web card shows no dollar amount; the cost test covers it."))
		}
		return verdict(steward.AxisSpec, steward.VerdictChanges, "The re-run path is missing its validation; one criterion cannot be checked from the diff.", crit)
	case "skipped":
		if err := stdOK(); err != nil {
			return err
		}
		return h.store.SetStewardPassSkipped(id, string(steward.AxisSpec), "no spec available", now)
	case "blocked-axis":
		if err := h.store.SetStewardPassBlocked(id, string(steward.AxisStandards), "timeout", "the steward seat ran out of its wall-clock limit", 0.4, now); err != nil {
			return err
		}
		return specAccepted()
	case "partial":
		if err := stdOK(); err != nil {
			return err
		}
		return h.store.SetStewardPassBlocked(id, string(steward.AxisSpec), store.StewardPassNotRun, "not requested: this review re-runs one axis", 0, now)
	case "running":
		// The second pass is left running: a state only a stubbed or racing read can see.
		return stdOK()
	default: // both-accepted
		if err := stdOK(); err != nil {
			return err
		}
		return specAccepted()
	}
}
