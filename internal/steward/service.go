package steward

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Typed errors the RPC layer maps onto wire codes. store.ErrStewardTokenUsed
// passes through unchanged.
var (
	ErrNotFound      = errors.New("steward review not found")
	ErrBadToken      = errors.New("steward verdict token does not match")
	ErrWrongRun      = errors.New("this run is not the steward seat of that review")
	ErrVerdictExists = store.ErrStewardReviewClosed
)

// Service holds the verdict rules over the store (ADR-0047 §3.5, §5): the
// seat's verdict is the only way a review ends ACCEPTED, and the daemon may
// only write blocked ones. Every dependency is injected so tests drive it
// without a daemon.
type Service struct {
	DB *store.Store
	// Head resolves a task's current branch head; "" + error = unknown.
	Head func(taskID int64) (string, error)
	// ContractSha is the sha256 of the task's newest validation contract,
	// "" when there is none.
	ContractSha func(taskID int64) string
	// Publish announces a changed review (steward_review.updated).
	Publish func(taskID, reviewID int64, status string)
	Now     func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) publish(taskID, id int64, status string) {
	if s.Publish != nil {
		s.Publish(taskID, id, status)
	}
}

// HashToken is how a one-time verdict token is stored: hex sha256, never the
// token itself.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken mints a one-time verdict token (32 random bytes, hex).
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("steward verdict token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SubmitVerdict stores the seat's verdict for review id. It is accepted only
// from the review's own run with its one-time token, once, and only when the
// verdict is complete against the digest it judged. A VerdictError consumes
// nothing: the seat fixes the document and resubmits.
func (s *Service) SubmitVerdict(reviewID int64, token, runID string, raw []byte) (View, error) {
	row, err := s.DB.GetStewardReview(reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return View{}, fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
	}
	if err != nil {
		return View{}, err
	}
	if row.Status != "pending" && row.Status != "running" {
		return View{}, ErrVerdictExists
	}
	if token == "" || subtle.ConstantTimeCompare([]byte(HashToken(token)), []byte(row.TokenHash)) != 1 {
		return View{}, ErrBadToken
	}
	if !s.isSeatRun(row, runID) {
		return View{}, ErrWrongRun
	}
	if row.TokenUsed {
		return View{}, store.ErrStewardTokenUsed
	}
	v, err := ParseSeatVerdict(raw)
	if err != nil {
		return View{}, err
	}
	if v.ReviewID != reviewID {
		return View{}, VerdictError{Field: "reviewId", Msg: fmt.Sprintf("the verdict must judge review %d", reviewID)}
	}
	var digest Digest
	if strings.TrimSpace(row.DigestJSON) == "" || json.Unmarshal([]byte(row.DigestJSON), &digest) != nil {
		return View{}, ve("digest", "review has no digest")
	}
	if err := CheckSeatVerdict(v, digest); err != nil {
		return View{}, err
	}
	res := Clamp(v.Verdict, digest.Gates)
	if res.Hold {
		return View{}, errors.New("a gate is still unknown; the review cannot be decided yet")
	}
	findingsJSON, err := json.Marshal(v.Findings)
	if err != nil {
		return View{}, err
	}
	gatesJSON, err := json.Marshal(digest.Gates)
	if err != nil {
		return View{}, err
	}
	summary := v.Summary
	if v.Verdict == VerdictBlocked {
		summary += " [blocked: " + v.BlockedReason + "]"
	}
	if res.Capped {
		summary += " [clamped to " + res.Verdict + ": " + strings.Join(res.Reasons, "; ") + "]"
	}
	if err := s.DB.ConsumeStewardToken(reviewID); err != nil {
		return View{}, err
	}
	if err := s.DB.SetStewardReviewVerdict(reviewID, v.Verdict, res.Verdict, string(findingsJSON), string(gatesJSON), summary, row.CostUSD, s.now()); err != nil {
		return View{}, err
	}
	s.publish(row.TaskID, reviewID, "done")
	return s.View(reviewID)
}

// isSeatRun reports whether runID is the steward seat's: the bound run, or
// any run of the seat's own task (a rate-limit wait re-queues the prompt as a
// new run of the same chat).
func (s *Service) isSeatRun(row store.StewardReview, runID string) bool {
	if runID == "" {
		return false
	}
	if row.RunID != "" && runID == row.RunID {
		return true
	}
	if row.StewardTaskID == 0 {
		return false
	}
	r, err := s.DB.GetRun(runID)
	return err == nil && r.TaskID == row.StewardTaskID
}

// WriteBlocked is the daemon's own end for a review: blocked with one of the
// daemon-owned reasons, when the seat's run ended without a verdict. It can
// never store ACCEPTED.
func (s *Service) WriteBlocked(reviewID int64, reason, summary string, costUSD float64) error {
	if !IsDaemonBlockedReason(reason) {
		return errors.New("the daemon may only write blocked reviews for timeout, budget, crashed, cancelled or scope_violation")
	}
	row, err := s.DB.GetStewardReview(reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if err := s.DB.SetStewardReviewBlocked(reviewID, reason, summary, costUSD, s.now()); err != nil {
		if errors.Is(err, store.ErrStewardReviewClosed) {
			return ErrVerdictExists
		}
		return err
	}
	s.publish(row.TaskID, reviewID, "blocked")
	return nil
}

// FlagScopeViolation marks review id as having touched outside its scratch
// scope, whether or not a verdict exists yet.
func (s *Service) FlagScopeViolation(reviewID int64) error {
	row, err := s.DB.GetStewardReview(reviewID)
	if err != nil {
		return fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
	}
	if err := s.DB.SetStewardReviewScopeViolation(reviewID); err != nil {
		return err
	}
	s.publish(row.TaskID, reviewID, "scope_violation")
	return nil
}

// FrameView is one evidence frame of the digest as the review card reads it:
// the base name only, never the absolute path.
type FrameView struct {
	Name        string `json:"name"`
	Viewport    int    `json:"viewport"`
	Theme       string `json:"theme"`
	CapturedSHA string `json:"capturedSha"`
}

// View is a review as read: its verdicts, the computed staleness and the
// merge-ready label. MergeReady is derived on every read, never stored. The
// view never carries the token hash or the digest; the frames carry only
// their base names, so the scratch path stays out.
type View struct {
	ID             int64         `json:"id"`
	TaskID         int64         `json:"taskId"`
	TriggerKind    string        `json:"triggerKind"`
	HeadSHA        string        `json:"headSha"`
	ContractSha256 string        `json:"contractSha256"`
	ValidationID   int64         `json:"validationId"`
	CIState        string        `json:"ciState"`
	Status         string        `json:"status"` // pending | running | done | blocked
	Verdict        string        `json:"verdict,omitempty"`
	SeatVerdict    string        `json:"seatVerdict,omitempty"`
	Summary        string        `json:"summary,omitempty"`
	BlockedReason  string        `json:"blockedReason,omitempty"`
	Findings       []SeatFinding `json:"findings"`
	Gates          []Gate        `json:"gates"`
	PRNumber       int           `json:"prNumber,omitempty"`
	Frames         []FrameView   `json:"frames"`
	Stale          string        `json:"stale,omitempty"`
	MergeReady     bool          `json:"mergeReady"`
	NotReadyReason string        `json:"notReadyReason,omitempty"`
	ScopeViolation bool          `json:"scopeViolation,omitempty"`
	CostUSD        float64       `json:"costUsd"`
	RunID          string        `json:"runId,omitempty"`
	StewardTaskID  int64         `json:"stewardTaskId"`
	CreatedAt      time.Time     `json:"createdAt"`
	FinishedAt     *time.Time    `json:"finishedAt,omitempty"`
}

// View reads review id with its staleness and merge-ready label.
func (s *Service) View(reviewID int64) (View, error) {
	row, err := s.DB.GetStewardReview(reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return View{}, fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
	}
	if err != nil {
		return View{}, err
	}
	latest, err := s.DB.LatestStewardReview(row.TaskID)
	if err != nil {
		return View{}, err
	}
	return s.view(row, latest.ID)
}

// List returns taskID's reviews, newest first (taskID 0: every task's).
func (s *Service) List(taskID int64, limit int) ([]View, error) {
	rows, err := s.DB.ListStewardReviews(taskID, limit)
	if err != nil {
		return nil, err
	}
	latest := map[int64]int64{}
	out := make([]View, 0, len(rows))
	for _, row := range rows {
		id, ok := latest[row.TaskID]
		if !ok {
			l, err := s.DB.LatestStewardReview(row.TaskID)
			if err != nil {
				return nil, err
			}
			id, latest[row.TaskID] = l.ID, l.ID
		}
		v, err := s.view(row, id)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// view maps one row with the newest review id of its task (0 = no rows).
func (s *Service) view(row store.StewardReview, latestReviewID int64) (View, error) {
	v := View{ID: row.ID, TaskID: row.TaskID, TriggerKind: row.TriggerKind, HeadSHA: row.HeadSHA, ContractSha256: row.ContractSha256,
		ValidationID: row.ValidationID, CIState: row.CIState, Status: row.Status, Verdict: row.Verdict, SeatVerdict: row.SeatVerdict,
		Summary: row.Summary, BlockedReason: row.BlockedReason, ScopeViolation: row.ScopeViolation, CostUSD: row.CostUSD,
		RunID: row.RunID, StewardTaskID: row.StewardTaskID, CreatedAt: row.CreatedAt, FinishedAt: row.FinishedAt,
		Findings: []SeatFinding{}, Gates: []Gate{}, Frames: []FrameView{}}
	if row.FindingsJSON != "" {
		if err := json.Unmarshal([]byte(row.FindingsJSON), &v.Findings); err != nil {
			return View{}, fmt.Errorf("steward review %d: findings: %w", row.ID, err)
		}
	}
	var digest Digest
	haveDigest := false
	if strings.TrimSpace(row.DigestJSON) != "" {
		haveDigest = json.Unmarshal([]byte(row.DigestJSON), &digest) == nil
	}
	if row.GatesJSON != "" {
		if err := json.Unmarshal([]byte(row.GatesJSON), &v.Gates); err != nil {
			return View{}, fmt.Errorf("steward review %d: gates: %w", row.ID, err)
		}
	} else if haveDigest && len(digest.Gates) > 0 {
		// A blocked review has no seat verdict, so no stored gates: the
		// digest's gates are still the review's objective state.
		v.Gates = digest.Gates
	}
	if haveDigest {
		if digest.PR != nil {
			v.PRNumber = digest.PR.Number
		}
		for _, f := range digest.Frames.Items {
			v.Frames = append(v.Frames, FrameView{Name: filepath.Base(f.Path), Viewport: f.Viewport, Theme: f.Theme, CapturedSHA: f.CapturedSHA})
		}
	}
	headNow := ""
	if s.Head != nil {
		if h, err := s.Head(row.TaskID); err == nil {
			headNow = h
		}
	}
	contractShaNow := ""
	if s.ContractSha != nil {
		contractShaNow = s.ContractSha(row.TaskID)
	}
	v.Stale = StaleReason(StaleInput{ReviewID: row.ID, ReviewHeadSHA: row.HeadSHA, ReviewContractSha: row.ContractSha256,
		HeadNow: headNow, ContractShaNow: contractShaNow, LatestReviewID: latestReviewID})
	ready, why := MergeReady(row.Verdict, row.Status, row.ScopeViolation, v.Stale)
	v.MergeReady, v.NotReadyReason = ready, why
	return v, nil
}

// DigestJSON returns review id's digest exactly as stored, the document the
// seat judged (ToolDigestGet serves it verbatim).
func (s *Service) DigestJSON(reviewID int64) (json.RawMessage, error) {
	row, err := s.DB.GetStewardReview(reviewID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
		}
		return nil, err
	}
	if strings.TrimSpace(row.DigestJSON) == "" {
		return nil, fmt.Errorf("steward review %d: %w", reviewID, ErrNotFound)
	}
	return json.RawMessage(row.DigestJSON), nil
}
