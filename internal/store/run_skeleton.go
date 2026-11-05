package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Skeleton storage (ADR-0053). A finished run's event rows are rewritten in
// place to a slim "v":2 form; the tool payloads they lose live in run_excerpts.
// Every read of run_events rows rehydrates a v2 row back to the shape a v1 row
// had, so a reader that does not care sees the same events (payloads over
// ExcerptWhole come back as head + marker + tail, thinking as a placeholder).
//
// A v2 row is a JSON object with "v":2 and "skel":
//
//	"skel": {"in": {"n": 1234, "sha": "..", "ex": true, "doc": [{"i":0,"n":87,"sha":".."}]},
//	         "out": {"n": 9000, "sha": "..", "ex": true},
//	         "think": {"n": 4096},
//	         "txt": {"n": 70000}}
//
// n is the whole payload's size, sha its sha256 (only when n > ExcerptInline),
// ex says the excerpt tier holds it, doc describes elided heredoc bodies.

// The payload thresholds, frozen in ADR-0053 (H6) from the PR0 measurement.
const (
	ExcerptInline = 512      // up to this, a payload stays in its row
	ExcerptWhole  = 8 << 10  // up to this the excerpt tier holds it whole
	ExcerptHalf   = 4 << 10  // above ExcerptWhole: head and tail of this size
	TextRowCap    = 32 << 10 // an answer row; a longer one keeps head and tail
	UserRowCap    = 16 << 10 // a user message row
)

// Excerpt is one tool payload moved out of a row.
type Excerpt struct {
	Field  string // "in" or "out"
	Head   string // the whole payload when Tail is ""
	Tail   string
	Enc    string // "" or "raw": head/tail cut the JSON text; "text": they cut a JSON string's decoded text
	Bytes  int64
	SHA256 string
}

// SkeletonRow is the finalizer's decision for one row.
type SkeletonRow struct {
	ID        int64
	Seq       int64
	Delete    bool   // the row was merged into an earlier one
	EventData string // the slim row (ignored when Delete)
	Excerpts  []Excerpt
}

// ListRunEventsRaw is ListRunEvents without rehydration: the rows as stored.
func (s *Store) ListRunEventsRaw(runID string) ([]RunEvent, error) {
	rows, err := s.db.Query(`SELECT id, run_id, seq, event_data, created_at FROM run_events WHERE run_id = ? ORDER BY seq`, runID)
	if err != nil {
		return nil, fmt.Errorf("list raw run events for %q: %w", runID, err)
	}
	defer rows.Close()
	var out []RunEvent
	for rows.Next() {
		var e RunEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.Seq, &e.EventData, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan run event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RunEventBytes is the total event_data size of runID, to bound a conversion.
func (s *Store) RunEventBytes(runID string) (int64, error) {
	var n sql.NullInt64
	if err := s.db.QueryRow(`SELECT SUM(length(event_data)) FROM run_events WHERE run_id = ?`, runID).Scan(&n); err != nil {
		return 0, fmt.Errorf("size of run %q events: %w", runID, err)
	}
	return n.Int64, nil
}

// SkeletonCandidates returns up to limit runs the finalizer should convert:
// finished, eligible (skeleton_rev = 0, so not a run that finished before
// skeleton storage), not pruned, not yet converted, with events.
func (s *Store) SkeletonCandidates(limit int) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT r.id FROM runs r WHERE r.status != 'running' AND r.skeleton_rev = 0 AND r.skeleton_at IS NULL
		   AND r.history_pruned_at IS NULL AND EXISTS (SELECT 1 FROM run_events e WHERE e.run_id = r.id)
		 ORDER BY r.started_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("skeleton candidates: %w", err)
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

// ApplySkeleton rewrites runID's rows in one transaction and marks the run
// converted (skeleton_at, skeleton_rev + 1). The predicate is re-checked in the
// transaction: a run that is running, pruned, converted or from before
// skeleton storage is left untouched (false, nil). A failure rolls everything
// back, so a crash leaves the run in full form for the next pass.
func (s *Store) ApplySkeleton(runID string, rows []SkeletonRow, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("apply skeleton to %q: %w", runID, err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM runs WHERE id = ? AND status != 'running' AND skeleton_rev = 0 AND skeleton_at IS NULL AND history_pruned_at IS NULL`, runID).Scan(&n); err != nil {
		return false, fmt.Errorf("apply skeleton to %q: %w", runID, err)
	}
	if n == 0 {
		return false, nil
	}
	for _, r := range rows {
		if r.Delete {
			if _, err := tx.Exec(`DELETE FROM run_events WHERE id = ? AND run_id = ?`, r.ID, runID); err != nil {
				return false, fmt.Errorf("apply skeleton to %q: delete row: %w", runID, err)
			}
			continue
		}
		if _, err := tx.Exec(`UPDATE run_events SET event_data = ? WHERE id = ? AND run_id = ?`, r.EventData, r.ID, runID); err != nil {
			return false, fmt.Errorf("apply skeleton to %q: update row: %w", runID, err)
		}
		for _, x := range r.Excerpts {
			enc := x.Enc
			if enc == "" {
				enc = "raw"
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO run_excerpts (run_id, seq, field, head, tail, enc, bytes, sha256) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				runID, r.Seq, x.Field, x.Head, x.Tail, enc, x.Bytes, x.SHA256); err != nil {
				return false, fmt.Errorf("apply skeleton to %q: excerpt: %w", runID, err)
			}
		}
	}
	if _, err := tx.Exec(`UPDATE runs SET skeleton_at = ?, skeleton_rev = skeleton_rev + 1 WHERE id = ?`, at.UTC(), runID); err != nil {
		return false, fmt.Errorf("apply skeleton to %q: mark: %w", runID, err)
	}
	return true, tx.Commit()
}

// SetRunNativeRef stores where the provider keeps the run's session (JSON).
func (s *Store) SetRunNativeRef(runID, ref string) error {
	if _, err := s.db.Exec(`UPDATE runs SET native_ref = ? WHERE id = ?`, ref, runID); err != nil {
		return fmt.Errorf("set run %q native ref: %w", runID, err)
	}
	return nil
}

// RunSkeleton is a run's skeleton columns.
type RunSkeleton struct {
	NativeRef  string
	SkeletonAt *time.Time
	Rev        int64
}

// GetRunSkeleton reads the skeleton columns of runID.
func (s *Store) GetRunSkeleton(runID string) (RunSkeleton, error) {
	var out RunSkeleton
	var at sql.NullTime
	if err := s.db.QueryRow(`SELECT native_ref, skeleton_at, skeleton_rev FROM runs WHERE id = ?`, runID).Scan(&out.NativeRef, &at, &out.Rev); err != nil {
		return RunSkeleton{}, fmt.Errorf("run %q skeleton: %w", runID, err)
	}
	out.SkeletonAt = nullToTimePtr(at)
	return out, nil
}

// OmittedMarker joins the head and tail of a payload the excerpt tier cut.
func OmittedMarker(omitted int64) string {
	return fmt.Sprintf("\n…[%d bytes omitted]…\n", omitted)
}

// CutRunes splits s into the longest prefix of at most n bytes and the longest
// suffix of at most n bytes, both on rune boundaries.
func CutRunes(s string, n int) (head, tail string) {
	h := n
	if h > len(s) {
		h = len(s)
	}
	for h > 0 && h < len(s) && !utf8.RuneStart(s[h]) {
		h--
	}
	t := len(s) - n
	if t < h {
		t = h
	}
	for t < len(s) && !utf8.RuneStart(s[t]) {
		t++
	}
	return s[:h], s[t:]
}

// rehydrate turns a v2 row back into the shape of a v1 row. Rows that are not
// v2 are returned unchanged and cost one substring test. truncated reports a
// payload the excerpt tier could not hold whole, or text it does not keep.
func (s *Store) rehydrate(e RunEvent) (RunEvent, error) {
	if !strings.Contains(e.EventData, `"v":2`) {
		return e, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(e.EventData), &m); err != nil {
		return e, nil
	}
	if string(m["v"]) != "2" || m["skel"] == nil {
		return e, nil
	}
	var sk struct {
		In    *skelField `json:"in"`
		Out   *skelField `json:"out"`
		Think *skelField `json:"think"`
		Txt   *skelField `json:"txt"`
	}
	if err := json.Unmarshal(m["skel"], &sk); err != nil {
		return e, nil
	}
	delete(m, "v")
	delete(m, "skel")
	for _, f := range []struct {
		name  string
		field string
		sk    *skelField
	}{{"toolInput", "in", sk.In}, {"toolResult", "out", sk.Out}} {
		if f.sk == nil || !f.sk.Ex {
			continue
		}
		raw, cut, err := s.excerptPayload(e.RunID, e.Seq, f.field)
		if err != nil {
			return e, err
		}
		if raw != nil {
			m[f.name] = raw
			e.Truncated = e.Truncated || cut
		} else {
			e.Truncated = true // the excerpt is gone: the row keeps its summary
		}
	}
	if sk.Txt != nil {
		e.Truncated = true
	}
	if sk.Think != nil {
		b, _ := json.Marshal(fmt.Sprintf("[thinking not kept: %d bytes]", sk.Think.N))
		m["text"] = b
		e.Truncated = true
	}
	out, err := json.Marshal(m)
	if err != nil {
		return e, nil
	}
	e.EventData = string(out)
	return e, nil
}

type skelField struct {
	N   int64  `json:"n"`
	SHA string `json:"sha,omitempty"`
	Ex  bool   `json:"ex,omitempty"`
}

// excerptPayload returns the payload of (runID, seq, field) as raw JSON: the
// original bytes when the tier holds it whole, else a JSON string of head +
// marker + tail with cut = true. nil when the excerpt is gone (pruned): the
// row keeps its summary.
func (s *Store) excerptPayload(runID string, seq int64, field string) (raw json.RawMessage, cut bool, err error) {
	var head, tail, enc string
	var bytes int64
	err = s.db.QueryRow(`SELECT head, tail, enc, bytes FROM run_excerpts WHERE run_id = ? AND seq = ? AND field = ?`, runID, seq, field).Scan(&head, &tail, &enc, &bytes)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read excerpt of %q seq %d: %w", runID, seq, err)
	}
	if tail == "" {
		return json.RawMessage(head), false, nil
	}
	b, _ := json.Marshal(head + OmittedMarker(bytes-int64(len(head))-int64(len(tail))) + tail)
	return b, true, nil
}

// ResetSkeletonForTest clears a run's converted mark, as a crash between the
// rewrite and the mark would leave it. For tests of the finalizer.
func (s *Store) ResetSkeletonForTest(runID string) (int64, error) {
	res, err := s.db.Exec(`UPDATE runs SET skeleton_at = NULL, skeleton_rev = 0 WHERE id = ?`, runID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkLegacyForTest marks a run as finished before skeleton storage.
func (s *Store) MarkLegacyForTest(runID string) error {
	_, err := s.db.Exec(`UPDATE runs SET skeleton_rev = -1 WHERE id = ?`, runID)
	return err
}

// DB exposes the handle to tests of other packages that need a raw query.
func (s *Store) DB() *sql.DB { return s.db }
