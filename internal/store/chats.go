package store

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateChat inserts a new chat under taskID, stamping created_at. provider
// and agent_session start NULL -- provider binds on the chat's first run
// (see BindChatProvider), agent_session is P2's to populate.
func (s *Store) CreateChat(taskID int64, title string) (Chat, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO chats (task_id, title, provider, agent_session, created_at) VALUES (?, ?, NULL, NULL, ?)`,
		taskID, title, now,
	)
	if err != nil {
		return Chat{}, fmt.Errorf("insert chat: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Chat{}, fmt.Errorf("chat id: %w", err)
	}
	return Chat{ID: id, TaskID: taskID, Title: title, CreatedAt: now}, nil
}

// GetChat returns the chat with the given id.
func (s *Store) GetChat(id int64) (Chat, error) {
	row := s.db.QueryRow(
		`SELECT id, task_id, title, provider, agent_session, created_at, archived_at FROM chats WHERE id = ?`, id,
	)
	c, err := scanChat(row)
	if err != nil {
		return Chat{}, fmt.Errorf("get chat %d: %w", id, err)
	}
	return c, nil
}

// ListChatsByTask returns taskID's chats ordered by id (oldest -- and so the
// task's default chat, see workspace.Manager.DefaultChat -- first).
// includeArchived controls whether archived chats are included.
func (s *Store) ListChatsByTask(taskID int64, includeArchived bool) ([]Chat, error) {
	query := `SELECT id, task_id, title, provider, agent_session, created_at, archived_at FROM chats WHERE task_id = ?`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY id`

	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("list chats for task %d: %w", taskID, err)
	}
	defer rows.Close()

	chats := make([]Chat, 0)
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, fmt.Errorf("scan chat: %w", err)
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

// RenameChat sets a chat's title and returns the updated row. Renaming a
// nonexistent id is a clear not-found error (via GetChat), never a silent
// no-op.
func (s *Store) RenameChat(id int64, title string) (Chat, error) {
	if _, err := s.GetChat(id); err != nil {
		return Chat{}, fmt.Errorf("rename chat %d: %w", id, err)
	}
	if _, err := s.db.Exec(`UPDATE chats SET title = ? WHERE id = ?`, title, id); err != nil {
		return Chat{}, fmt.Errorf("rename chat %d: %w", id, err)
	}
	return s.GetChat(id)
}

// ArchiveChat stamps archived_at and returns the updated row. Calling it
// again on an already-archived chat is a no-op (matching ArchiveTask): the
// WHERE clause matches no rows, so archived_at is left as the first
// archival time.
func (s *Store) ArchiveChat(id int64) (Chat, error) {
	now := time.Now().UTC()
	if _, err := s.db.Exec(`UPDATE chats SET archived_at = ? WHERE id = ? AND archived_at IS NULL`, now, id); err != nil {
		return Chat{}, fmt.Errorf("archive chat %d: %w", id, err)
	}
	return s.GetChat(id)
}

// BindChatProvider sets a chat's provider the first time it runs, and is a
// no-op if the chat is already bound -- callers (internal/runs.Registry.Start)
// are responsible for checking a bound chat's existing provider matches the
// requested one *before* calling this; this method itself never overwrites
// an already-set provider, so it's safe to call unconditionally on every
// first-run path without racing a concurrent bind onto a different value.
func (s *Store) BindChatProvider(id int64, provider string) (Chat, error) {
	if _, err := s.db.Exec(`UPDATE chats SET provider = ? WHERE id = ? AND provider IS NULL`, provider, id); err != nil {
		return Chat{}, fmt.Errorf("bind chat %d provider: %w", id, err)
	}
	return s.GetChat(id)
}

// SetChatAgentSession stores sessionJSON (an opaque, provider-native session
// handle -- see Chat.AgentSession's doc comment) on chatID, overwriting any
// previous value. Nothing in P1 calls this; it exists so P2's runners have
// somewhere to persist a session handle once resume support lands.
func (s *Store) SetChatAgentSession(chatID int64, sessionJSON string) (Chat, error) {
	if _, err := s.db.Exec(`UPDATE chats SET agent_session = ? WHERE id = ?`, sessionJSON, chatID); err != nil {
		return Chat{}, fmt.Errorf("set chat %d agent session: %w", chatID, err)
	}
	return s.GetChat(chatID)
}

func scanChat(row rowScanner) (Chat, error) {
	var c Chat
	var provider, agentSession sql.NullString
	var archivedAt sql.NullTime

	if err := row.Scan(&c.ID, &c.TaskID, &c.Title, &provider, &agentSession, &c.CreatedAt, &archivedAt); err != nil {
		return Chat{}, err
	}
	c.Provider = nullToStringPtr(provider)
	c.AgentSession = nullToStringPtr(agentSession)
	c.ArchivedAt = nullToTimePtr(archivedAt)
	return c, nil
}
