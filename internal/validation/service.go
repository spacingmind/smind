package validation

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// Typed errors the RPC layer maps onto wire codes.
var (
	ErrNotFound       = errors.New("not found")
	ErrBadToken       = errors.New("verdict token does not match")
	ErrWrongRun       = errors.New("this run is not the validator of that validation")
	ErrVerdictExists  = store.ErrVerdictExists
	ErrValidationOpen = store.ErrValidationOpen
)

// Service holds the contract and verdict rules over the store. Every
// dependency is injected so tests drive it without a daemon.
type Service struct {
	DB *store.Store
	// EvidenceRoot is $SMIND_HOME/validation: evidence is copied to
	// <root>/<validationId>/ so it outlives the validator's worktree.
	EvidenceRoot string
	Limits       func() Limits
	// Head resolves a task's current branch head (the commit a verdict must
	// be at to be merge-ready).
	Head func(taskID int64) (string, error)
	// TaskDir resolves a task's worktree directory (for contract files).
	TaskDir func(taskID int64) (string, error)
	// Publish announces a changed contract or verdict (validation.updated).
	Publish func(taskID, validationID int64, status string)
	Now     func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) limits() Limits {
	if s.Limits != nil {
		return s.Limits()
	}
	return DefaultLimits
}

func (s *Service) publish(taskID, id int64, status string) {
	if s.Publish != nil {
		s.Publish(taskID, id, status)
	}
}

// ContractRecord is a stored contract version.
type ContractRecord struct {
	ID       int64
	TaskID   int64
	Version  int
	Sha256   string
	Contract Contract
	Body     string // markdown, for humans; "" when omitted
	Created  time.Time
}

// SubmitContract parses, validates and stores src as the next version of
// taskID's contract.
func (s *Service) SubmitContract(taskID int64, src []byte) (ContractRecord, error) {
	if _, err := s.DB.GetTask(taskID); err != nil {
		return ContractRecord{}, fmt.Errorf("task %d: %w", taskID, ErrNotFound)
	}
	p, err := ParseDocument(src, s.limits())
	if err != nil {
		return ContractRecord{}, err
	}
	row, err := s.DB.InsertValidationContract(taskID, p.Sha256, string(p.JSON), p.Body, s.now())
	if err != nil {
		return ContractRecord{}, err
	}
	s.publish(taskID, 0, "contract")
	return recordOf(row)
}

func recordOf(row store.ValidationContract) (ContractRecord, error) {
	var c Contract
	if err := json.Unmarshal([]byte(row.BodyJSON), &c); err != nil {
		return ContractRecord{}, fmt.Errorf("contract %d: %w", row.ID, err)
	}
	return ContractRecord{ID: row.ID, TaskID: row.TaskID, Version: row.Version, Sha256: row.Sha256, Contract: c, Body: row.BodyMD, Created: row.CreatedAt}, nil
}

// ReadContractFile reads a contract file from inside taskID's worktree
// (rel is relative, no symlink may leave the worktree, size-capped).
func (s *Service) ReadContractFile(taskID int64, rel string) ([]byte, error) {
	if s.TaskDir == nil {
		return nil, errors.New("contract files are not available")
	}
	if !SafeRelPath(rel) {
		return nil, fe("path", "must be a relative path inside the task worktree")
	}
	dir, err := s.TaskDir(taskID)
	if err != nil {
		return nil, fmt.Errorf("task %d: %w", taskID, err)
	}
	full, err := resolveInside(dir, rel)
	if err != nil {
		return nil, fe("path", "%s", err.Error())
	}
	st, err := os.Stat(full)
	if err != nil || !st.Mode().IsRegular() {
		return nil, fe("path", "is not a regular file")
	}
	if st.Size() > MaxFrontMatterBytes+MaxBodyBytes+64 {
		return nil, fe("path", "is larger than a contract may be")
	}
	return os.ReadFile(full)
}

// resolveInside resolves rel under root, following symlinks, and fails when
// the result is outside root.
func resolveInside(root, rel string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("directory is not available")
	}
	full, err := filepath.EvalSymlinks(filepath.Join(rootReal, filepath.FromSlash(strings.ReplaceAll(rel, "\\", "/"))))
	if err != nil {
		return "", fmt.Errorf("%q does not exist", rel)
	}
	r, err := filepath.Rel(rootReal, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q leaves the directory", rel)
	}
	return full, nil
}

// GetContract returns one contract version by id.
func (s *Service) GetContract(id int64) (ContractRecord, error) {
	row, err := s.DB.GetValidationContract(id)
	if errors.Is(err, sql.ErrNoRows) {
		return ContractRecord{}, fmt.Errorf("contract %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return ContractRecord{}, err
	}
	return recordOf(row)
}

// LatestContract returns taskID's highest-version contract.
func (s *Service) LatestContract(taskID int64) (ContractRecord, error) {
	row, err := s.DB.LatestValidationContract(taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return ContractRecord{}, fmt.Errorf("task %d has no contract: %w", taskID, ErrNotFound)
	}
	if err != nil {
		return ContractRecord{}, err
	}
	return recordOf(row)
}

// CreateValidation records a validation of taskID's contract at commitSHA and
// returns the one-time verdict token (the caller hands it only to the
// validator's run; it is stored hashed). The run is bound with BindRun once
// the seat starts it. One open validation per task.
func (s *Service) CreateValidation(taskID, contractID int64, commitSHA string) (store.Validation, string, error) {
	if !IsSHA(commitSHA) {
		return store.Validation{}, "", fe("commitSha", "must be a full 40-hex commit id")
	}
	ct, err := s.DB.GetValidationContract(contractID)
	if err != nil || ct.TaskID != taskID {
		return store.Validation{}, "", fmt.Errorf("contract %d of task %d: %w", contractID, taskID, ErrNotFound)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return store.Validation{}, "", fmt.Errorf("verdict token: %w", err)
	}
	token := hex.EncodeToString(buf)
	v, err := s.DB.InsertValidation(store.Validation{TaskID: taskID, ContractID: contractID, CommitSHA: commitSHA, TokenHash: hashToken(token)}, s.now())
	if err != nil {
		return store.Validation{}, "", err
	}
	s.publish(taskID, v.ID, "running")
	return v, token, nil
}

// BindRun records the validator's run, task and scratch worktree.
func (s *Service) BindRun(id int64, runID string, validatorTaskID int64, scratchDir string) error {
	return s.DB.SetValidationRun(id, runID, validatorTaskID, scratchDir)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// SubmitVerdict stores the validator's verdict for validation id. It is
// accepted only from the validation's own run with its one-time token, once,
// and only when the verdict passes the completeness rules.
func (s *Service) SubmitVerdict(id int64, token, runID string, raw []byte) (View, error) {
	val, err := s.DB.GetValidation(id)
	if errors.Is(err, sql.ErrNoRows) {
		return View{}, fmt.Errorf("validation %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return View{}, err
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(val.TokenHash)) != 1 || token == "" {
		return View{}, ErrBadToken
	}
	if val.RunID == "" || runID != val.RunID {
		return View{}, ErrWrongRun
	}
	if val.VerdictJSON != "" {
		return View{}, ErrVerdictExists
	}
	v, err := ParseVerdict(raw)
	if err != nil {
		return View{}, err
	}
	ct, err := s.GetContract(val.ContractID)
	if err != nil {
		return View{}, err
	}
	if v.ContractID != val.ContractID || v.ContractSha256 != ct.Sha256 {
		return View{}, fe("contractId", "the verdict must judge contract %d (sha256 %s)", val.ContractID, ct.Sha256)
	}
	if v.CommitSHA != val.CommitSHA {
		return View{}, fe("commitSha", "the verdict must judge commit %s", val.CommitSHA)
	}
	if err := CheckVerdict(ct.Contract, v); err != nil {
		return View{}, err
	}
	if err := s.copyEvidence(val, &v); err != nil {
		return View{}, err
	}
	stored, err := json.Marshal(v)
	if err != nil {
		return View{}, err
	}
	if err := s.DB.SetValidationVerdict(id, string(stored), v.Status, false, s.now()); err != nil {
		return View{}, err
	}
	s.publish(val.TaskID, id, v.Status)
	return s.View(id)
}

// WriteBlocked is the daemon's own verdict: blocked with one of the
// daemon-owned reasons, when a validator run ended without submitting. It can
// never write a pass or a fail.
func (s *Service) WriteBlocked(id int64, reason, summary string) error {
	if !daemonBlockedReasons[reason] {
		return fmt.Errorf("the daemon may only write blocked verdicts for timeout, budget, crashed or scope_violation, not %q", reason)
	}
	val, err := s.DB.GetValidation(id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("validation %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return err
	}
	ct, err := s.GetContract(val.ContractID)
	if err != nil {
		return err
	}
	if len(summary) > 800 {
		summary = summary[:800]
	}
	raw, _ := json.Marshal(Verdict{V: 1, ContractID: val.ContractID, ContractSha256: ct.Sha256, CommitSHA: val.CommitSHA,
		Status: StatusBlocked, BlockedReason: reason, Summary: summary, Items: []Item{}})
	if err := s.DB.SetValidationVerdict(id, string(raw), StatusBlocked, reason == ReasonScopeViolation, s.now()); err != nil {
		return err
	}
	s.publish(val.TaskID, id, StatusBlocked)
	return nil
}

// FlagScopeViolation marks validation id as having changed the lead's
// worktree, whether or not a verdict exists yet.
func (s *Service) FlagScopeViolation(id int64) error {
	val, err := s.DB.GetValidation(id)
	if err != nil {
		return fmt.Errorf("validation %d: %w", id, ErrNotFound)
	}
	if err := s.DB.SetValidationScopeViolation(id); err != nil {
		return err
	}
	s.publish(val.TaskID, id, "scope_violation")
	return nil
}

// EvidenceDir is where validation id's evidence files live.
func (s *Service) EvidenceDir(id int64) string {
	return filepath.Join(s.EvidenceRoot, strconv.FormatInt(id, 10))
}

// copyEvidence copies every file evidence of v out of the validator's scratch
// worktree into the stable evidence dir, recomputing its hash. A path that
// resolves outside the scratch worktree, a non-regular file or an oversized
// one is rejected.
func (s *Service) copyEvidence(val store.Validation, v *Verdict) error {
	for i := range v.Items {
		for j := range v.Items[i].Evidence {
			ev := &v.Items[i].Evidence[j]
			if ev.Type != "file" {
				continue
			}
			p := fmt.Sprintf("items[%d].evidence[%d].path", i, j)
			if val.ScratchDir == "" || s.EvidenceRoot == "" {
				return fe(p, "file evidence needs the validator's scratch worktree")
			}
			src, err := resolveInside(val.ScratchDir, ev.Path)
			if err != nil {
				return fe(p, "%s", err.Error())
			}
			st, err := os.Stat(src)
			if err != nil || !st.Mode().IsRegular() {
				return fe(p, "is not a regular file")
			}
			if st.Size() > MaxFileEvidence {
				return fe(p, "is %d bytes, the cap is %d", st.Size(), MaxFileEvidence)
			}
			rel := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(ev.Path, "\\", "/")))
			dst := filepath.Join(s.EvidenceDir(val.ID), filepath.FromSlash(rel))
			sum, err := copyFile(src, dst)
			if err != nil {
				return fmt.Errorf("copy evidence %s: %w", rel, err)
			}
			ev.Path, ev.Sha256 = rel, sum
		}
	}
	return nil
}

func copyFile(src, dst string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), in); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// View is a validation as read: its verdict and the computed merge-ready
// label. MergeReady is derived on every read, never stored.
type View struct {
	ID              int64      `json:"id"`
	TaskID          int64      `json:"taskId"`
	ContractID      int64      `json:"contractId"`
	ContractVersion int        `json:"contractVersion"`
	ContractSha256  string     `json:"contractSha256"`
	CommitSHA       string     `json:"commitSha"`
	RunID           string     `json:"runId,omitempty"`
	ValidatorTaskID *int64     `json:"validatorTaskId,omitempty"`
	Status          string     `json:"status"` // running | pass | fail | blocked
	Verdict         *Verdict   `json:"verdict,omitempty"`
	ScopeViolation  bool       `json:"scopeViolation,omitempty"`
	MergeReady      bool       `json:"mergeReady"`
	Stale           bool       `json:"stale,omitempty"`
	NotReadyReason  string     `json:"notReadyReason,omitempty"`
	EvidenceDir     string     `json:"evidenceDir,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
}

// View reads validation id with its merge-ready label.
func (s *Service) View(id int64) (View, error) {
	val, err := s.DB.GetValidation(id)
	if errors.Is(err, sql.ErrNoRows) {
		return View{}, fmt.Errorf("validation %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return View{}, err
	}
	all, err := s.DB.ListValidations(val.TaskID)
	if err != nil {
		return View{}, err
	}
	return s.view(val, len(all) > 0 && all[0].ID == val.ID)
}

// ViewOwnedBy returns validation id when it belongs to taskID; any other id
// reads as not found.
func (s *Service) ViewOwnedBy(id, taskID int64) (View, error) {
	v, err := s.View(id)
	if err == nil && v.TaskID != taskID {
		return View{}, fmt.Errorf("validation %d: %w", id, ErrNotFound)
	}
	return v, err
}

// List returns taskID's validations, newest first; only the newest can be
// merge-ready.
func (s *Service) List(taskID int64) ([]View, error) {
	rows, err := s.DB.ListValidations(taskID)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(rows))
	for i, val := range rows {
		v, err := s.view(val, i == 0)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) view(val store.Validation, newest bool) (View, error) {
	ct, err := s.DB.GetValidationContract(val.ContractID)
	if err != nil {
		return View{}, fmt.Errorf("validation %d: contract: %w", val.ID, err)
	}
	v := View{ID: val.ID, TaskID: val.TaskID, ContractID: val.ContractID, ContractVersion: ct.Version, ContractSha256: ct.Sha256,
		CommitSHA: val.CommitSHA, RunID: val.RunID, ValidatorTaskID: val.ValidatorTaskID, Status: "running",
		ScopeViolation: val.ScopeViolation, CreatedAt: val.CreatedAt, FinishedAt: val.FinishedAt}
	if val.VerdictJSON == "" {
		v.NotReadyReason = "no verdict yet"
		return v, nil
	}
	var verdict Verdict
	if err := json.Unmarshal([]byte(val.VerdictJSON), &verdict); err != nil {
		return View{}, fmt.Errorf("validation %d: verdict: %w", val.ID, err)
	}
	v.Verdict, v.Status = &verdict, val.VerdictStatus
	if verdict.Items != nil && s.EvidenceRoot != "" {
		v.EvidenceDir = s.EvidenceDir(val.ID)
	}
	if !newest {
		v.Stale, v.NotReadyReason = true, "superseded by a newer validation"
		return v, nil
	}
	latest, err := s.DB.LatestValidationContract(val.TaskID)
	if err != nil {
		return View{}, err
	}
	var head string
	if s.Head != nil {
		head, _ = s.Head(val.TaskID)
	}
	ready, why := MergeReady(val.VerdictStatus, val.CommitSHA, val.ScopeViolation, head, latest.ID == val.ContractID)
	v.MergeReady, v.NotReadyReason = ready, why
	v.Stale = (head != "" && head != val.CommitSHA) || latest.ID != val.ContractID
	return v, nil
}
