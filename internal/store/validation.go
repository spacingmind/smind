package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ValidationContract is one immutable version of a task's validation
// contract (ADR-0045 §1). BodyJSON is the validated front matter; BodyMD is
// the markdown prose kept for humans only.
type ValidationContract struct {
	ID        int64
	TaskID    int64
	Version   int
	Sha256    string
	BodyJSON  string
	BodyMD    string
	CreatedAt time.Time
}

// Validation is one validator run bound to {contract, commit}. VerdictJSON is
// "" until the verdict is written, and is never rewritten.
type Validation struct {
	ID              int64
	TaskID          int64
	ContractID      int64
	CommitSHA       string
	RunID           string
	ValidatorTaskID *int64
	ScratchDir      string
	TokenHash       string
	VerdictJSON     string
	VerdictStatus   string
	ScopeViolation  bool
	CreatedAt       time.Time
	FinishedAt      *time.Time
}

// ErrVerdictExists is returned when a validation already has its verdict.
var ErrVerdictExists = errors.New("validation already has a verdict")

// ErrValidationOpen is returned by InsertValidation when the task already has
// a validation without a verdict (one validation at a time per task).
var ErrValidationOpen = errors.New("task already has an open validation")

const validationContractCols = `id, task_id, version, sha256, body_json, body_md, created_at`

func scanValidationContract(row interface{ Scan(...any) error }) (ValidationContract, error) {
	var c ValidationContract
	err := row.Scan(&c.ID, &c.TaskID, &c.Version, &c.Sha256, &c.BodyJSON, &c.BodyMD, &c.CreatedAt)
	return c, err
}

// InsertValidationContract stores the next version of taskID's contract.
func (s *Store) InsertValidationContract(taskID int64, sha, bodyJSON, bodyMD string, now time.Time) (ValidationContract, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ValidationContract{}, fmt.Errorf("insert validation contract: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var max sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(version) FROM validation_contracts WHERE task_id = ?`, taskID).Scan(&max); err != nil {
		return ValidationContract{}, fmt.Errorf("insert validation contract: %w", err)
	}
	version := int(max.Int64) + 1
	res, err := tx.Exec(`INSERT INTO validation_contracts (task_id, version, sha256, body_json, body_md, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		taskID, version, sha, bodyJSON, bodyMD, now.UTC())
	if err != nil {
		return ValidationContract{}, fmt.Errorf("insert validation contract: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return ValidationContract{}, fmt.Errorf("insert validation contract: %w", err)
	}
	return s.GetValidationContract(id)
}

// GetValidationContract returns contract id, or sql.ErrNoRows.
func (s *Store) GetValidationContract(id int64) (ValidationContract, error) {
	return scanValidationContract(s.db.QueryRow(`SELECT `+validationContractCols+` FROM validation_contracts WHERE id = ?`, id))
}

// LatestValidationContract returns taskID's highest-version contract, or sql.ErrNoRows.
func (s *Store) LatestValidationContract(taskID int64) (ValidationContract, error) {
	return scanValidationContract(s.db.QueryRow(`SELECT `+validationContractCols+` FROM validation_contracts WHERE task_id = ? ORDER BY version DESC LIMIT 1`, taskID))
}

const validationCols = `id, task_id, contract_id, commit_sha, run_id, validator_task_id, scratch_dir, token_hash,
	verdict_json, verdict_status, scope_violation, created_at, finished_at`

func scanValidation(row interface{ Scan(...any) error }) (Validation, error) {
	var v Validation
	var scope int
	err := row.Scan(&v.ID, &v.TaskID, &v.ContractID, &v.CommitSHA, &v.RunID, &v.ValidatorTaskID, &v.ScratchDir, &v.TokenHash,
		&v.VerdictJSON, &v.VerdictStatus, &scope, &v.CreatedAt, &v.FinishedAt)
	v.ScopeViolation = scope != 0
	return v, err
}

// InsertValidation records a validation (ID and CreatedAt are assigned). It
// fails with ErrValidationOpen when the task has one without a verdict.
func (s *Store) InsertValidation(v Validation, now time.Time) (Validation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Validation{}, fmt.Errorf("insert validation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var open int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM validations WHERE task_id = ? AND verdict_json = ''`, v.TaskID).Scan(&open); err != nil {
		return Validation{}, fmt.Errorf("insert validation: %w", err)
	}
	if open > 0 {
		return Validation{}, ErrValidationOpen
	}
	res, err := tx.Exec(`INSERT INTO validations (task_id, contract_id, commit_sha, run_id, validator_task_id, scratch_dir, token_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.TaskID, v.ContractID, v.CommitSHA, v.RunID, v.ValidatorTaskID, v.ScratchDir, v.TokenHash, now.UTC())
	if err != nil {
		return Validation{}, fmt.Errorf("insert validation: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return Validation{}, fmt.Errorf("insert validation: %w", err)
	}
	return s.GetValidation(id)
}

// GetValidation returns validation id, or sql.ErrNoRows.
func (s *Store) GetValidation(id int64) (Validation, error) {
	return scanValidation(s.db.QueryRow(`SELECT `+validationCols+` FROM validations WHERE id = ?`, id))
}

// ListValidations returns taskID's validations, newest first.
func (s *Store) ListValidations(taskID int64) ([]Validation, error) {
	rows, err := s.db.Query(`SELECT `+validationCols+` FROM validations WHERE task_id = ? ORDER BY id DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list validations: %w", err)
	}
	defer rows.Close()
	var out []Validation
	for rows.Next() {
		v, err := scanValidation(rows)
		if err != nil {
			return nil, fmt.Errorf("list validations: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetValidationRun records the validator's run, task and scratch directory
// once the seat has started it (before any verdict exists).
func (s *Store) SetValidationRun(id int64, runID string, validatorTaskID int64, scratchDir string) error {
	res, err := s.db.Exec(`UPDATE validations SET run_id = ?, validator_task_id = ?, scratch_dir = ? WHERE id = ? AND verdict_json = ''`,
		runID, validatorTaskID, scratchDir, id)
	if err != nil {
		return fmt.Errorf("set validation run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrVerdictExists
	}
	return nil
}

// SetValidationVerdict writes the one verdict of validation id. It is
// write-once: a second call fails with ErrVerdictExists.
func (s *Store) SetValidationVerdict(id int64, verdictJSON, status string, scopeViolation bool, now time.Time) error {
	res, err := s.db.Exec(`UPDATE validations SET verdict_json = ?, verdict_status = ?, scope_violation = ?, finished_at = ? WHERE id = ? AND verdict_json = ''`,
		verdictJSON, status, boolInt(scopeViolation), now.UTC(), id)
	if err != nil {
		return fmt.Errorf("set validation verdict: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrVerdictExists
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SetValidationScopeViolation flags validation id as having changed the
// lead's worktree. Daemon-only; allowed after the verdict was written (the
// post-check runs after the validator's run ends).
func (s *Store) SetValidationScopeViolation(id int64) error {
	if _, err := s.db.Exec(`UPDATE validations SET scope_violation = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("flag scope violation: %w", err)
	}
	return nil
}

// ListValidationsByRun returns the validations whose validator run is runID.
func (s *Store) ListValidationsByRun(runID string) ([]Validation, error) {
	if runID == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT `+validationCols+` FROM validations WHERE run_id = ?`, runID)
	if err != nil {
		return nil, fmt.Errorf("list validations by run: %w", err)
	}
	defer rows.Close()
	var out []Validation
	for rows.Next() {
		v, err := scanValidation(rows)
		if err != nil {
			return nil, fmt.Errorf("list validations by run: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
