package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Run events retention (docs/plans/active/run-events-retention.md). Pruning a
// run is two-phase so a crash never leaves it half visible: MarkRunPruned
// stores the run's final text and stamps history_pruned_at (from then on every
// reader serves the summary and ignores leftover rows), then
// DeleteRunEventsBatch removes the rows in bounded batches.

// pruneEligible selects the runs whose detailed events may go: finished before
// the cutoff, never a running run, and either not a live task's run (archived
// task, system task, or a non-user purpose such as a learning probe) or already
// marked pruned (its leftover rows are finished off whatever the task became).
// The one ? is the cutoff.
const pruneEligible = `r.finished_at IS NOT NULL AND r.finished_at < ? AND r.status != 'running'
	AND (r.history_pruned_at IS NOT NULL OR r.purpose != 'user' OR t.kind = 'system' OR t.archived_at IS NOT NULL)`

// PruneCounts is what a prune pass would delete (or did). The Rows fields
// count the two kinds separately: full prunes delete a run's event rows,
// excerpt prunes only the excerpt tier of a skeleton run.
type PruneCounts struct {
	Runs         int64 `json:"runs"`
	Rows         int64 `json:"rows"`
	Bytes        int64 `json:"bytes"`
	ExcerptRuns  int64 `json:"excerptRuns"`
	ExcerptRows  int64 `json:"excerptRows"`
	ExcerptBytes int64 `json:"excerptBytes"`
}

// PrunePreview counts the runs with eligible events left, their rows and the
// event_data bytes, without changing anything.
func (s *Store) PrunePreview(cutoff time.Time) (PruneCounts, error) {
	var c PruneCounts
	err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT r.id), COUNT(e.id), COALESCE(SUM(length(e.event_data)), 0)
		 FROM runs r JOIN tasks t ON t.id = r.task_id JOIN run_events e ON e.run_id = r.id
		 WHERE r.skeleton_at IS NULL AND `+pruneEligible, cutoff.UTC()).Scan(&c.Runs, &c.Rows, &c.Bytes)
	if err != nil {
		return PruneCounts{}, fmt.Errorf("prune preview: %w", err)
	}
	err = s.db.QueryRow(
		`SELECT COUNT(DISTINCT r.id), COUNT(*), COALESCE(SUM(length(x.head)+length(x.tail)), 0)
		 FROM runs r JOIN tasks t ON t.id = r.task_id JOIN run_excerpts x ON x.run_id = r.id
		 WHERE r.skeleton_at IS NOT NULL AND `+pruneEligible, cutoff.UTC()).Scan(&c.ExcerptRuns, &c.ExcerptRows, &c.ExcerptBytes)
	if err != nil {
		return PruneCounts{}, fmt.Errorf("prune preview: excerpts: %w", err)
	}
	return c, nil
}

// PruneCandidates returns up to limit eligible run ids that still have work
// left -- event rows for a run in full form, excerpt rows for a skeleton run
// -- oldest finished first.
func (s *Store) PruneCandidates(cutoff time.Time, limit int) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT r.id FROM runs r JOIN tasks t ON t.id = r.task_id
		 WHERE `+pruneEligible+` AND (
		   (r.skeleton_at IS NULL AND EXISTS (SELECT 1 FROM run_events e WHERE e.run_id = r.id))
		   OR (r.skeleton_at IS NOT NULL AND EXISTS (SELECT 1 FROM run_excerpts x WHERE x.run_id = r.id)))
		 ORDER BY r.finished_at, r.id LIMIT ?`, cutoff.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("prune candidates: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkRunPruned is phase 1: it stores finalText and stamps history_pruned_at
// if the run is still eligible (the predicate is re-checked in the UPDATE, so
// a run that became live or running in between is left alone) and not yet
// marked. A skeleton run is never marked: its rows stay, only its excerpt
// tier may go (PruneExcerptsBatch). It reports whether this call marked it.
func (s *Store) MarkRunPruned(runID, finalText string, at, cutoff time.Time) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE runs SET final_text = ?, history_pruned_at = ?
		 WHERE id = ? AND history_pruned_at IS NULL AND skeleton_at IS NULL AND EXISTS (
		   SELECT 1 FROM runs r JOIN tasks t ON t.id = r.task_id WHERE r.id = runs.id AND `+pruneEligible+`)`,
		finalText, at.UTC(), runID, cutoff.UTC())
	if err != nil {
		return false, fmt.Errorf("mark run %q pruned: %w", runID, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteRunEventsBatch is phase 2: it deletes at most limit event rows of a run
// that is already marked pruned (never of an unmarked run) and returns how many.
func (s *Store) DeleteRunEventsBatch(runID string, limit int) (int64, error) {
	// The excerpt tier (ADR-0053) goes with the first batch: a pruned run keeps
	// no payload copies.
	if _, err := s.db.Exec(`DELETE FROM run_excerpts WHERE run_id = ? AND EXISTS (SELECT 1 FROM runs r WHERE r.id = ? AND r.history_pruned_at IS NOT NULL)`, runID, runID); err != nil {
		return 0, fmt.Errorf("delete run excerpts of %q: %w", runID, err)
	}
	res, err := s.db.Exec(
		`DELETE FROM run_events WHERE id IN (
		   SELECT e.id FROM run_events e WHERE e.run_id = ?
		     AND EXISTS (SELECT 1 FROM runs r WHERE r.id = e.run_id AND r.history_pruned_at IS NOT NULL)
		   ORDER BY e.seq LIMIT ?)`, runID, limit)
	if err != nil {
		return 0, fmt.Errorf("delete run events of %q: %w", runID, err)
	}
	return res.RowsAffected()
}

// PruneExcerptsBatch is the skeleton form's prune (ADR-0053 PR C): in one
// transaction it stamps excerpts_pruned_at and deletes at most limit excerpt
// rows of an eligible skeleton run, returning how many. run_events, the run
// row and its final text are never touched, so every reader keeps serving the
// skeleton (a payload whose excerpt is gone reads back as its summary). The
// eligibility predicate is re-checked in both statements, as MarkRunPruned
// does, so a run that is running, from a live task or not yet old enough is
// left alone -- and a crash after the mark is resumed by the next call.
func (s *Store) PruneExcerptsBatch(runID string, limit int, at, cutoff time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("prune excerpts of %q: %w", runID, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`UPDATE runs SET excerpts_pruned_at = ?
		 WHERE id = ? AND excerpts_pruned_at IS NULL AND skeleton_at IS NOT NULL AND EXISTS (
		   SELECT 1 FROM runs r JOIN tasks t ON t.id = r.task_id WHERE r.id = runs.id AND `+pruneEligible+`)`,
		at.UTC(), runID, cutoff.UTC()); err != nil {
		return 0, fmt.Errorf("prune excerpts of %q: mark: %w", runID, err)
	}
	res, err := tx.Exec(
		`DELETE FROM run_excerpts WHERE rowid IN (
		   SELECT x.rowid FROM run_excerpts x JOIN runs r ON r.id = x.run_id JOIN tasks t ON t.id = r.task_id
		    WHERE x.run_id = ? AND r.skeleton_at IS NOT NULL AND `+pruneEligible+` LIMIT ?)`, runID, cutoff.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("prune excerpts of %q: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// GetRunFinalText returns the final text kept on a pruned run ("" for none).
func (s *Store) GetRunFinalText(runID string) (string, error) {
	var t string
	err := s.db.QueryRow(`SELECT final_text FROM runs WHERE id = ?`, runID).Scan(&t)
	if err != nil {
		return "", fmt.Errorf("run %q final text: %w", runID, err)
	}
	return t, nil
}

// Checkpoint truncates the write-ahead log (a no-op outside WAL mode).
func (s *Store) Checkpoint() error {
	var busy, logFrames, done int
	if err := s.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &done); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	return nil
}

// FreePages is the number of unused pages in the file and the page size.
func (s *Store) FreePages() (pages, pageSize int64, err error) {
	if err = s.db.QueryRow(`PRAGMA freelist_count`).Scan(&pages); err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	return pages, pageSize, err
}

// AutoVacuumIncremental reports whether the file is in incremental auto_vacuum mode.
func (s *Store) AutoVacuumIncremental() bool {
	var mode int
	return s.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode) == nil && mode == 2
}

// IncrementalVacuum returns up to pages unused pages to the file system (only
// effective in incremental auto_vacuum mode) and reports the bytes freed.
func (s *Store) IncrementalVacuum(pages int) (int64, error) {
	if !s.AutoVacuumIncremental() {
		return 0, nil
	}
	before, size, err := s.FreePages()
	if err != nil {
		return 0, err
	}
	if _, err := s.db.Exec(fmt.Sprintf(`PRAGMA incremental_vacuum(%d)`, pages)); err != nil {
		return 0, fmt.Errorf("incremental vacuum: %w", err)
	}
	after, _, err := s.FreePages()
	if err != nil {
		return 0, err
	}
	return (before - after) * size, nil
}

// CompactResult is what Compact did to the file.
type CompactResult struct {
	BeforeBytes int64 `json:"beforeBytes"`
	AfterBytes  int64 `json:"afterBytes"`
}

// CompactMinFreeFactor is how much free disk Compact needs, as a multiple of
// the database size (VACUUM writes a full copy next to it).
const CompactMinFreeFactor = 1.2

// Compact rewrites the database file (VACUUM) and switches it to incremental
// auto_vacuum, returning the space freed rows left behind. It holds the write
// lock for the whole rewrite, so callers decide when it is safe (no run is
// running); Compact itself refuses when the disk cannot hold the copy.
func (s *Store) Compact(ctx context.Context) (CompactResult, error) {
	if s.path == "" || isMemoryDSN(s.path) {
		return CompactResult{}, fmt.Errorf("compact: not a file database")
	}
	st, err := os.Stat(s.path)
	if err != nil {
		return CompactResult{}, fmt.Errorf("compact: %w", err)
	}
	free, err := freeDiskBytes(filepath.Dir(s.path))
	if err != nil {
		return CompactResult{}, fmt.Errorf("compact: cannot read free disk space: %w", err)
	}
	if need := int64(float64(st.Size()) * CompactMinFreeFactor); free < need {
		return CompactResult{}, fmt.Errorf("compact: needs %d bytes free next to the database, %d available", need, free)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return CompactResult{}, err
	}
	defer conn.Close()
	for _, q := range []string{`PRAGMA wal_checkpoint(TRUNCATE)`, `PRAGMA auto_vacuum = INCREMENTAL`, `VACUUM`, `PRAGMA wal_checkpoint(TRUNCATE)`} {
		if err := execAny(ctx, conn, q); err != nil {
			return CompactResult{}, fmt.Errorf("compact: %s: %w", q, err)
		}
	}
	after, err := os.Stat(s.path)
	if err != nil {
		return CompactResult{}, err
	}
	return CompactResult{BeforeBytes: st.Size(), AfterBytes: after.Size()}, nil
}

// execAny runs q and discards any rows (wal_checkpoint returns one).
func execAny(ctx context.Context, conn *sql.Conn, q string) error {
	rows, err := conn.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	return err
}
