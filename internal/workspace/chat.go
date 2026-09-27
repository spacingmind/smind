package workspace

import (
	"fmt"

	"github.com/spacingmind/smind/internal/store"
)

// defaultChatTitle is the title given to a task's automatically-created
// first chat -- both the one CreateTask makes for a brand-new task and the
// one the chats migration backfills for a pre-existing one (see
// store.backfillDefaultChats), so both paths produce an identical-looking
// row.
const defaultChatTitle = "Chat"

// CreateChat creates a new chat under taskID. title may be empty (an
// explicit chat.create with no title supplied); it does not default to
// defaultChatTitle -- that name is reserved for a task's automatically
// created first chat, so a user-named chat with an empty title reads as
// genuinely untitled, not as a second "Chat".
func (m *Manager) CreateChat(taskID int64, title string) (store.Chat, error) {
	if _, err := m.store.GetTask(taskID); err != nil {
		return store.Chat{}, fmt.Errorf("create chat: %w", err)
	}
	c, err := m.store.CreateChat(taskID, title)
	if err != nil {
		return store.Chat{}, fmt.Errorf("create chat: %w", err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyChatCreated(c)
	}
	return c, nil
}

// createDefaultChat creates taskID's first chat, titled defaultChatTitle --
// called once, right after CreateTask inserts the task row, so every task
// always has at least one chat for task.prompt/run.start's "omitted chatId"
// resolution (DefaultChat) to find.
func (m *Manager) createDefaultChat(taskID int64) (store.Chat, error) {
	c, err := m.store.CreateChat(taskID, defaultChatTitle)
	if err != nil {
		return store.Chat{}, fmt.Errorf("create default chat: %w", err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyChatCreated(c)
	}
	return c, nil
}

// GetChat returns the chat with the given id.
func (m *Manager) GetChat(id int64) (store.Chat, error) {
	return m.store.GetChat(id)
}

// ListChats returns taskID's chats, ordered by id (oldest -- its default
// chat -- first). includeArchived controls whether archived chats are
// included.
func (m *Manager) ListChats(taskID int64, includeArchived bool) ([]store.Chat, error) {
	return m.store.ListChatsByTask(taskID, includeArchived)
}

// DefaultChat returns taskID's default chat -- the oldest (lowest id) chat
// under it, which CreateTask guarantees always exists. This is what
// task.prompt/run.start resolve an omitted chatId to (ADR-0016 P1.3), so old
// clients that have never heard of chats keep landing on the same
// conversation thread they always have.
func (m *Manager) DefaultChat(taskID int64) (store.Chat, error) {
	chats, err := m.store.ListChatsByTask(taskID, true)
	if err != nil {
		return store.Chat{}, fmt.Errorf("default chat for task %d: %w", taskID, err)
	}
	if len(chats) == 0 {
		// Shouldn't happen for any task created through CreateTask (or
		// migrated by store.backfillDefaultChats) -- both guarantee at
		// least one chat -- but a store bypassing that invariant (a raw
		// test insert, e.g.) gets a clear error rather than a panic on
		// chats[0] below.
		return store.Chat{}, fmt.Errorf("default chat for task %d: task has no chats", taskID)
	}
	return chats[0], nil
}

// RenameChat sets a chat's title. An empty title is rejected, matching
// account.rename's convention (internal/wsapi's handleAccountRename).
func (m *Manager) RenameChat(id int64, title string) (store.Chat, error) {
	if title == "" {
		return store.Chat{}, fmt.Errorf("rename chat %d: title is required", id)
	}
	c, err := m.store.RenameChat(id, title)
	if err != nil {
		return store.Chat{}, fmt.Errorf("rename chat %d: %w", id, err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyChatUpdated(c)
	}
	return c, nil
}

// ArchiveChat marks a chat archived. Refusing this while the chat has a
// running run is internal/wsapi's job (it has the *runs.Registry this
// package doesn't import -- see handleChatArchive), not this method's;
// ArchiveChat itself is unconditional, matching ArchiveTask's own shape
// (idempotent: archiving an already-archived chat leaves archived_at
// unchanged).
func (m *Manager) ArchiveChat(id int64) (store.Chat, error) {
	c, err := m.store.ArchiveChat(id)
	if err != nil {
		return store.Chat{}, fmt.Errorf("archive chat %d: %w", id, err)
	}
	if n := m.getNotifier(); n != nil {
		n.NotifyChatArchived(c)
	}
	return c, nil
}
