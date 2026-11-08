package steward

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spacingmind/smind/internal/store"
)

// This file is the two-axis review's verdict path (ADR-0054). A two-axis
// review has one steward_reviews row (the aggregate, clamp, staleness and
// mergeReady) and one steward_review_passes row per axis. Each pass submits
// with its own single-use token; the review ends when every pass is terminal:
// the stored verdict is Clamp(Combine(passes), gates), so a pass can only
// lower it and neither can clear a gate. Nothing starts a pass yet (P2), and
// SubmitVerdict, the single-pass path, is untouched.

// SubmitPassVerdict stores the verdict of the pass of review reviewID bound to
// axis. It is accepted only from that pass's own run with its one-time token,
// once, and only when the verdict is complete for the axis against the digest
// it judged: the axis the verdict names must be the axis of the token, so a
// Standards seat can never satisfy the Spec pass. A VerdictError consumes
// nothing: the seat fixes the document and resubmits. When it is the last
// open pass the review itself is decided.
func (s *Service) SubmitPassVerdict(reviewID int64, axis Axis, token, runID string, raw []byte) (View, error) {
	row, pass, err := s.openPass(reviewID, axis)
	if err != nil {
		return View{}, err
	}
	if token == "" || !tokenMatches(token, pass.TokenHash) {
		return View{}, ErrBadToken
	}
	if !s.isPassRun(pass, runID) {
		return View{}, ErrWrongRun
	}
	if pass.TokenUsed {
		return View{}, store.ErrStewardTokenUsed
	}
	v, err := ParseAxisVerdict(raw)
	if err != nil {
		return View{}, err
	}
	if v.ReviewID != reviewID {
		return View{}, VerdictError{Field: "reviewId", Msg: fmt.Sprintf("the verdict must judge review %d", reviewID)}
	}
	digest, err := digestOf(row)
	if err != nil {
		return View{}, err
	}
	criteriaIDs := digest.criteriaIDs()
	if err := CheckAxisVerdict(v, digest, axis, criteriaIDs); err != nil {
		return View{}, err
	}
	if Clamp(v.Verdict, digest.Gates).Hold {
		return View{}, errGateUnknown
	}
	findings := make([]AxisFinding, len(v.Findings))
	copy(findings, v.Findings)
	for i := range findings {
		findings[i].Axis = axis
	}
	findingsJSON, err := json.Marshal(findings)
	if err != nil {
		return View{}, err
	}
	criteriaJSON, err := json.Marshal(v.Criteria)
	if err != nil {
		return View{}, err
	}
	summary := v.Summary
	if v.Verdict == VerdictBlocked {
		summary += " [blocked: " + v.BlockedReason + "]"
	}
	if err := s.DB.ConsumeStewardPassToken(reviewID, string(axis)); err != nil {
		return View{}, err
	}
	if err := s.DB.SetStewardPassVerdict(reviewID, string(axis), v.Verdict, summary, string(findingsJSON), string(criteriaJSON), pass.CostUSD, s.now()); err != nil {
		return View{}, passClosed(err)
	}
	s.publish(row.TaskID, reviewID, "running")
	return s.finalize(reviewID)
}

// WritePassBlocked is the daemon's own end for one pass: blocked with one of
// the daemon-owned reasons, when the pass's run ended without a verdict. It
// can never store a verdict. When it is the last open pass the review is
// decided.
func (s *Service) WritePassBlocked(reviewID int64, axis Axis, reason, summary string, costUSD float64) error {
	if !IsDaemonBlockedReason(reason) {
		return errors.New("the daemon may only write blocked passes for timeout, budget, crashed, cancelled, scope_violation or base_unresolved")
	}
	row, _, err := s.openPass(reviewID, axis)
	if err != nil {
		return err
	}
	if err := s.DB.SetStewardPassBlocked(reviewID, string(axis), reason, summary, costUSD, s.now()); err != nil {
		return passClosed(err)
	}
	s.publish(row.TaskID, reviewID, "running")
	_, err = s.finalize(reviewID)
	return err
}

// WritePassSkipped ends the Spec pass of a review with no spec to judge: no
// verdict, no cost, reported as "no spec available" (ADR-0054 §6). Only the
// Spec axis, and only while the digest carries no criteria: skipping is not a
// way to drop an axis that has work.
func (s *Service) WritePassSkipped(reviewID int64, axis Axis) error {
	if axis != AxisSpec {
		return errors.New("only the spec pass can be skipped: the standards pass always runs")
	}
	row, _, err := s.openPass(reviewID, axis)
	if err != nil {
		return err
	}
	digest, err := digestOf(row)
	if err != nil {
		return err
	}
	if digest.Spec.HasSpec() {
		return errors.New("the spec pass cannot be skipped: the digest resolved a spec to judge against")
	}
	if err := s.DB.SetStewardPassSkipped(reviewID, string(axis), "no spec available", s.now()); err != nil {
		return passClosed(err)
	}
	s.publish(row.TaskID, reviewID, "running")
	_, err = s.finalize(reviewID)
	return err
}

// openPass loads the review and its pass for axis and requires both to be
// open (pending or running).
func (s *Service) openPass(reviewID int64, axis Axis) (store.StewardReview, store.StewardReviewPass, error) {
	row, err := s.DB.GetStewardReview(reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return row, store.StewardReviewPass{}, fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
	}
	if err != nil {
		return row, store.StewardReviewPass{}, err
	}
	if row.Status != PassPending && row.Status != PassRunning {
		return row, store.StewardReviewPass{}, ErrVerdictExists
	}
	pass, err := s.DB.GetStewardReviewPass(reviewID, string(axis))
	if errors.Is(err, sql.ErrNoRows) {
		return row, pass, fmt.Errorf("steward review %d has no %s pass: %w", reviewID, axis, ErrNotFound)
	}
	if err != nil {
		return row, pass, err
	}
	if pass.Status != PassPending && pass.Status != PassRunning {
		return row, pass, ErrVerdictExists
	}
	return row, pass, nil
}

// isPassRun reports whether runID is the pass's seat: its bound run, or any
// run of the pass's own task (a rate-limit wait re-queues the prompt as a new
// run of the same chat).
func (s *Service) isPassRun(pass store.StewardReviewPass, runID string) bool {
	if runID == "" {
		return false
	}
	if pass.RunID != "" && runID == pass.RunID {
		return true
	}
	if pass.StewardTaskID == 0 {
		return false
	}
	r, err := s.DB.GetRun(runID)
	return err == nil && r.TaskID == pass.StewardTaskID
}

// finalize decides the review once every pass is terminal and does nothing
// while one is open: the stored verdict is Clamp(Combine(passes), gates), a
// blocked review when no pass produced a verdict. Racing finalizers are
// harmless: the verdict is write-once.
func (s *Service) finalize(reviewID int64) (View, error) {
	row, err := s.DB.GetStewardReview(reviewID)
	if err != nil {
		return View{}, err
	}
	passes, err := s.DB.ListStewardReviewPasses(reviewID)
	if err != nil {
		return View{}, err
	}
	results := make([]PassResult, 0, len(passes))
	cost := 0.0
	for _, p := range passes {
		results = append(results, PassResult{Axis: Axis(p.Axis), Status: p.Status, SeatVerdict: p.SeatVerdict, BlockedReason: p.BlockedReason})
		cost += p.CostUSD
	}
	combined, err := Combine(results)
	if errors.Is(err, ErrPassesOpen) {
		return s.View(reviewID)
	}
	if err != nil {
		return View{}, err
	}
	summary := passSummaries(passes)
	if combined.Blocked {
		summary += " [" + strings.Join(combined.Notes, "; ") + "]"
		if combined.ScopeViolation {
			// As the single-pass watch does for a changed Lead worktree: the
			// flag is allowed on a closed review and survives it.
			if err := s.DB.SetStewardReviewScopeViolation(reviewID); err != nil {
				return View{}, err
			}
		}
		err = s.DB.SetStewardReviewBlocked(reviewID, combined.BlockedReason, summary, cost, s.now())
		if errors.Is(err, store.ErrStewardReviewClosed) {
			return s.View(reviewID) // already decided (a cancel won the race)
		}
		if err != nil {
			return View{}, err
		}
		s.publish(row.TaskID, reviewID, "blocked")
		return s.View(reviewID)
	}
	digest, err := digestOf(row)
	if err != nil {
		return View{}, err
	}
	res := Clamp(combined.SeatVerdict, digest.Gates)
	if res.Hold {
		return View{}, errGateUnknown
	}
	findings, err := mergedFindings(passes)
	if err != nil {
		return View{}, err
	}
	gatesJSON, err := json.Marshal(digest.Gates)
	if err != nil {
		return View{}, err
	}
	if len(combined.Notes) > 0 {
		summary += " [" + strings.Join(combined.Notes, "; ") + "]"
	}
	if res.Capped {
		summary += " [clamped to " + res.Verdict + ": " + strings.Join(res.Reasons, "; ") + "]"
	}
	err = s.DB.SetStewardReviewVerdict(reviewID, combined.SeatVerdict, res.Verdict, findings, string(gatesJSON), summary, cost, s.now())
	if errors.Is(err, store.ErrStewardReviewClosed) {
		return s.View(reviewID) // already decided by a racing finalizer or a cancel
	}
	if err != nil {
		return View{}, err
	}
	s.publish(row.TaskID, reviewID, "done")
	return s.View(reviewID)
}

// passSummaries joins the passes' own summaries, one tagged part per pass
// that has one, standards first.
func passSummaries(passes []store.StewardReviewPass) string {
	parts := make([]string, 0, len(passes))
	for _, p := range passes {
		if p.Summary != "" {
			parts = append(parts, "["+p.Axis+"] "+p.Summary)
		}
	}
	return strings.Join(parts, " ")
}

// mergedFindings is every pass's findings as one JSON array, standards first,
// each tagged with the axis of the pass that produced it.
func mergedFindings(passes []store.StewardReviewPass) (string, error) {
	all := []AxisFinding{}
	for _, p := range passes {
		if p.FindingsJSON == "" {
			continue
		}
		var fs []AxisFinding
		if err := json.Unmarshal([]byte(p.FindingsJSON), &fs); err != nil {
			return "", fmt.Errorf("steward review pass %s: findings: %w", p.Axis, err)
		}
		for i := range fs {
			fs[i].Axis = Axis(p.Axis)
		}
		all = append(all, fs...)
	}
	b, err := json.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// passClosed maps the store's closed-pass error onto ErrVerdictExists, the
// error a second verdict gets in the single-pass path.
func passClosed(err error) error {
	if errors.Is(err, store.ErrStewardPassClosed) {
		return ErrVerdictExists
	}
	return err
}

// errGateUnknown is the refusal while a gate is undecided, as in SubmitVerdict.
var errGateUnknown = errors.New("a gate is still unknown; the review cannot be decided yet")

// digestOf decodes the digest a review was judged on.
func digestOf(row store.StewardReview) (Digest, error) {
	var d Digest
	if strings.TrimSpace(row.DigestJSON) == "" || json.Unmarshal([]byte(row.DigestJSON), &d) != nil {
		return Digest{}, ve("digest", "review has no digest")
	}
	return d, nil
}

// tokenMatches compares a presented token with a stored hash in constant time.
func tokenMatches(token, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashToken(token)), []byte(storedHash)) == 1
}

// AxisDigestJSON is the digest as the seat of one pass may read it (ADR-0054
// §3): the shared objective fields plus only that axis's sections. The
// Standards seat gets no spec section, no criteria texts and no PR body (the
// text the spec is read from); the Spec seat gets no standards section, no
// head diff of the standards files and no rules. The stored digest stays whole
// (SubmitPassVerdict reads the criteria ids from it); this is a projection.
// Any axis but the two is refused rather than answered with the full digest.
func (s *Service) AxisDigestJSON(reviewID int64, axis Axis) (json.RawMessage, error) {
	row, err := s.DB.GetStewardReview(reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	d, err := digestOf(row)
	if err != nil {
		return nil, err
	}
	switch axis {
	case AxisStandards:
		d.Spec = nil
		d.Untrusted.SpecCriteria = nil
		d.Untrusted.PRBody = map[string]string{}
	case AxisSpec:
		d.Standards = nil
		d.Untrusted.StandardsHeadDiff = ""
		d.Untrusted.StandardsRules = nil
	default:
		return nil, fmt.Errorf("steward review %d: unknown axis %q", reviewID, axis)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
