package validation

import (
	"fmt"
	"strings"
)

// EvidenceSubdir is where, inside its scratch worktree, a validator writes
// the files it cites as evidence (screenshots, logs).
const EvidenceSubdir = ".validation-evidence"

// BuildPrompt builds the validator's one and only first prompt (ADR-0045 §4).
// It is assembled from the stored contract's structured fields, the validation
// ids and fixed instructions: nothing the Lead typed beyond the contract
// itself. In particular it never includes the contract's markdown body, a
// Peer handoff, the PR description or the Lead's chat.
func BuildPrompt(rec ContractRecord, validationID int64, commitSHA string) string {
	c := rec.Contract
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("You are the validator for validation #%d. You judge one commit against one contract, in a fresh context.", validationID)
	w("")
	w("Commit under validation: %s (your worktree is checked out at exactly this commit).", commitSHA)
	w("Diff to review: `git diff %s...%s` (base ref %s). Read it adversarially: a change the contract does not mention is a finding.", c.Base, commitSHA, c.Base)
	w("Contract: #%d, sha256 %s, milestone %q. `validation_contract_get` returns the stored copy.", rec.ID, rec.Sha256, c.Milestone)
	w("")
	w("## Acceptance criteria")
	for _, a := range c.ACs {
		w("- %s: %s", a.ID, a.Text)
		var covers []string
		for _, t := range a.Covers.Tests {
			covers = append(covers, "test "+t)
		}
		for _, m := range a.Covers.Mutations {
			covers = append(covers, "mutation "+m)
		}
		for _, f := range a.Covers.Frames {
			covers = append(covers, "frame "+f)
		}
		for _, m := range a.Covers.Commands {
			covers = append(covers, "command "+m)
		}
		w("  rests on: %s", strings.Join(covers, ", "))
		if a.Visual {
			w("  (visual: %s)", a.VisualReason)
		}
	}
	if len(c.Tests) > 0 {
		w("")
		w("## Tests (run each, record the output; a failing test is re-run once, fail-then-pass is flaky and reported)")
		for _, t := range c.Tests {
			q := ""
			if t.Quarantine {
				q = " [quarantined: a failure is reported as a finding, not a fail]"
			}
			w("- %s: `%s`%s", t.Name, t.Run, q)
		}
	}
	if len(c.Mutations) > 0 {
		w("")
		w("## Mutations (make ONE change, run the named test, it must FAIL, then restore the file with `git checkout -- <file>`; say restored: true)")
		for _, m := range c.Mutations {
			w("- %s: in %s: %s; expect failing: %s", m.ID, m.File, m.Change, strings.Join(m.ExpectFail, ", "))
		}
	}
	if len(c.Commands) > 0 {
		w("")
		w("## Commands (run once, record the exit status)")
		for _, cm := range c.Commands {
			w("- %s: `%s`", cm.ID, cm.Run)
		}
	}
	if len(c.Frames) > 0 {
		w("")
		w("## Frames (screens to look at; write images under %s/ in your worktree and cite them)", EvidenceSubdir)
		for _, f := range c.Frames {
			w("- %s: %s, state %q, viewports %v, themes %v", f.ID, f.Surface, f.State, f.Viewport, f.Theme)
		}
		w("If you cannot run the app (no browser, no toolchain), report that item blocked with the reason; do not guess.")
	}
	w("")
	w("## Rules")
	w("- Write only inside your own worktree. You may write: %s. Anything else, ask.", writeScopeText(c.WriteScope))
	w("- Never touch the lead's worktree, push, open a PR or merge.")
	w("- Every test, mutation, command and frame item needs evidence: an excerpt of the real output (at most 2 KiB) or a file under %s/. A frame item also needs a note saying what you observed.", EvidenceSubdir)
	w("- Every AC item names, in via, the checks it rests on; an AC that rests only on frames is not a pass unless marked visual.")
	w("- Limits: about %d minutes and USD %.2f; stop and report blocked rather than exceed them.", c.Limits.MaxMinutes, c.Limits.BudgetUSD)
	w("")
	w("## Reporting")
	w("Call `validation_verdict_submit` once with the verdict object: {v:1, contractId:%d, contractSha256:%q, commitSha:%q, status: pass|fail|blocked, blockedReason (blocked only: no_browser, no_toolchain, flaky_unresolved, contract_unrunnable), summary, items:[{kind: test|mutation|command|frame|ac, id, status: pass|fail|blocked|skipped, attempts, flaky, restored, note, via, evidence:[{type: excerpt|file, text|path}]}], findings:[{severity: P0..P3, file, line, text}]}.", rec.ID, rec.Sha256, commitSHA)
	w("A pass needs a passing, evidenced item for every test, mutation, command, frame and AC in the contract; the daemon rejects an incomplete pass and tells you which item is missing.")
	w("Your final message starts with one fenced json block {\"status\":\"done\",\"validationId\":%d,\"verdict\":\"pass|fail|blocked\"} and nothing before it.", validationID)
	return b.String()
}

func writeScopeText(ws WriteScope) string {
	if len(ws.Paths) == 0 {
		return "your scratch worktree only, and only to run the mutations and write evidence"
	}
	return "your scratch worktree, plus these paths for generated files: " + strings.Join(ws.Paths, ", ")
}
