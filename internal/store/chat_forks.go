package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ChatFork is the record of a chat forked from a finished turn of another chat
// of the same task (ADR-0057). It lives beside chats, not in it, so no query
// over chats changes.
type ChatFork struct {
	ChatID        int64
	ParentChatID  int64
	ForkedAtRunID string
	Kind          string // ChatForkKindFork | ChatForkKindRetry
	Mode          string // ChatForkModeNative | ChatForkModeSeeded
	Seed          string
	SeedUsedAt    *time.Time
	CreatedAt     time.Time
}

// Fork kinds and modes.
const (
	ChatForkKindFork  = "fork"
	ChatForkKindRetry = "retry"

	ChatForkModeNative = "native"
	ChatForkModeSeeded = "seeded"
)

// InsertChatFork records that chatID was forked. Write-once per chat.
func (s *Store) InsertChatFork(f ChatFork) error {
	if f.Kind == "" {
		f.Kind = ChatForkKindFork
	}
	if f.Mode == "" {
		f.Mode = ChatForkModeNative
	}
	now := f.CreatedAt
	if now.IsZero() {
		now = time.Now()
	}
	if _, err := s.db.Exec(`INSERT INTO chat_forks (chat_id, parent_chat_id, forked_at_run_id, fork_kind, mode, seed, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.ChatID, f.ParentChatID, f.ForkedAtRunID, f.Kind, f.Mode, f.Seed, now.UTC()); err != nil {
		return fmt.Errorf("insert chat fork %d: %w", f.ChatID, err)
	}
	return nil
}

// GetChatFork returns chatID's fork record, or sql.ErrNoRows for a chat that
// is not a fork.
func (s *Store) GetChatFork(chatID int64) (ChatFork, error) {
	var f ChatFork
	var used sql.NullTime
	err := s.db.QueryRow(`SELECT chat_id, parent_chat_id, forked_at_run_id, fork_kind, mode, seed, seed_used_at, created_at FROM chat_forks WHERE chat_id = ?`, chatID).
		Scan(&f.ChatID, &f.ParentChatID, &f.ForkedAtRunID, &f.Kind, &f.Mode, &f.Seed, &used, &f.CreatedAt)
	if err != nil {
		return ChatFork{}, err
	}
	f.SeedUsedAt = nullToTimePtr(used)
	return f, nil
}

// ListChatForksByTask returns the fork records of a task's chats by chat id.
func (s *Store) ListChatForksByTask(taskID int64) (map[int64]ChatFork, error) {
	rows, err := s.db.Query(`SELECT f.chat_id, f.parent_chat_id, f.forked_at_run_id, f.fork_kind, f.mode, f.seed, f.seed_used_at, f.created_at
		FROM chat_forks f JOIN chats c ON c.id = f.chat_id WHERE c.task_id = ?`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list chat forks for task %d: %w", taskID, err)
	}
	defer rows.Close()
	out := map[int64]ChatFork{}
	for rows.Next() {
		var f ChatFork
		var used sql.NullTime
		if err := rows.Scan(&f.ChatID, &f.ParentChatID, &f.ForkedAtRunID, &f.Kind, &f.Mode, &f.Seed, &used, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.SeedUsedAt = nullToTimePtr(used)
		out[f.ChatID] = f
	}
	return out, rows.Err()
}

// ClaimChatForkSeed returns the seed of chatID's fork the first time it is
// asked for and marks it used; every later call (and a chat that is not a seeded
// fork) gets "", false. The claim is a single conditional UPDATE, so two runs
// racing for the same chat cannot both receive the seed.
func (s *Store) ClaimChatForkSeed(chatID int64, now time.Time) (string, bool, error) {
	res, err := s.db.Exec(`UPDATE chat_forks SET seed_used_at = ? WHERE chat_id = ? AND mode = 'seeded' AND seed <> '' AND seed_used_at IS NULL`, now.UTC(), chatID)
	if err != nil {
		return "", false, fmt.Errorf("claim chat fork seed %d: %w", chatID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", false, nil
	}
	var seed string
	if err := s.db.QueryRow(`SELECT seed FROM chat_forks WHERE chat_id = ?`, chatID).Scan(&seed); err != nil {
		return "", false, fmt.Errorf("read chat fork seed %d: %w", chatID, err)
	}
	return seed, true, nil
}

// AppendChatClaudeTotals logs the cumulative usage totals (JSON) a claude
// session reported at the end of a turn of chatID.
func (s *Store) AppendChatClaudeTotals(chatID int64, at time.Time, totalsJSON string) error {
	if _, err := s.db.Exec(`INSERT INTO chat_claude_totals (chat_id, at, totals_json) VALUES (?, ?, ?)`, chatID, at.UTC(), totalsJSON); err != nil {
		return fmt.Errorf("append chat claude totals %d: %w", chatID, err)
	}
	return nil
}

// ChatClaudeTotalsAt returns the newest logged totals of chatID at or before
// at; "" when none was logged (a turn before the log existed).
func (s *Store) ChatClaudeTotalsAt(chatID int64, at time.Time) (string, error) {
	var js string
	err := s.db.QueryRow(`SELECT totals_json FROM chat_claude_totals WHERE chat_id = ? AND at <= ? ORDER BY at DESC, rowid DESC LIMIT 1`, chatID, at.UTC()).Scan(&js)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("chat claude totals %d: %w", chatID, err)
	}
	return js, nil
}

// ListUserRunsByChat returns chatID's user runs, oldest first.
func (s *Store) ListUserRunsByChat(chatID int64) ([]Run, error) {
	rows, err := s.db.Query(`SELECT `+runColumns+` FROM runs WHERE purpose = 'user' AND chat_id = ? ORDER BY started_at ASC, rowid ASC`, chatID)
	if err != nil {
		return nil, fmt.Errorf("list runs of chat %d: %w", chatID, err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
