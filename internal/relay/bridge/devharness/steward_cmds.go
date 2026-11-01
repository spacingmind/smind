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
// carries "review-id".
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
	stale := len(args) > 2 && args[2] == "stale"
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

	// Passing gates first, the failing one after them: the screen is the one
	// that must show failing gates first.
	gates := []steward.Gate{
		{ID: "ci", State: steward.GatePass},
		{ID: "validator", State: steward.GatePass, Detail: "contract v2 satisfied"},
		{ID: "frames_fresh", State: steward.GateUnknown, Detail: "no frame matrix declared"},
		{ID: "no_binaries", State: steward.GatePass},
		{ID: "no_secrets", State: steward.GateFail, Detail: "1 possible token in mobile/src/api.ts"},
		{ID: "not_stacked", State: steward.GateNA},
		{ID: "pr_hygiene", State: steward.GatePass, Detail: "title conventional, Validated section present"},
	}
	digest, err := json.Marshal(steward.Digest{
		V:      1,
		Task:   steward.TaskInfo{ID: taskID, HeadSHA: head},
		PR:     &steward.PRSummary{Number: 482, Base: "develop", TitleConventional: true, HasValidated: true},
		Frames: steward.FramesSection{Dir: dir, Items: items},
		Gates:  gates,
	})
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
		fj, merr := json.Marshal(append([]steward.SeatFinding{}, findings...))
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
