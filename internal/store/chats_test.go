package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestStore_CreateChat(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)

	c, err := s.CreateChat(task.ID, "My chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if c.ID == 0 {
		t.Fatalf("CreateChat() ID = 0, want nonzero")
	}
	if c.TaskID != task.ID || c.Title != "My chat" {
		t.Fatalf("CreateChat() = %+v, want TaskID=%d Title=%q", c, task.ID, "My chat")
	}
	if c.Provider != nil {
		t.Fatalf("CreateChat() Provider = %v, want nil (unbound)", c.Provider)
	}
	if c.AgentSession != nil {
		t.Fatalf("CreateChat() AgentSession = %v, want nil", c.AgentSession)
	}
	if c.ArchivedAt != nil {
		t.Fatalf("CreateChat() ArchivedAt = %v, want nil", c.ArchivedAt)
	}

	got, err := s.GetChat(c.ID)
	if err != nil {
		t.Fatalf("GetChat() error = %v", err)
	}
	if got.ID != c.ID || got.TaskID != c.TaskID || got.Title != c.Title {
		t.Fatalf("GetChat() = %+v, want %+v", got, c)
	}
}

func TestStore_GetChat_UnknownID(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	if _, err := s.GetChat(999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChat(unknown) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStore_ListChatsByTask(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	other := newTestTaskForRuns(t, s)

	c1, err := s.CreateChat(task.ID, "first")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	c2, err := s.CreateChat(task.ID, "second")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := s.CreateChat(other.ID, "unrelated"); err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	got, err := s.ListChatsByTask(task.ID, true)
	if err != nil {
		t.Fatalf("ListChatsByTask() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != c1.ID || got[1].ID != c2.ID {
		t.Fatalf("ListChatsByTask() = %+v, want [%d, %d] ordered by id", got, c1.ID, c2.ID)
	}

	if _, err := s.ArchiveChat(c1.ID); err != nil {
		t.Fatalf("ArchiveChat() error = %v", err)
	}

	activeOnly, err := s.ListChatsByTask(task.ID, false)
	if err != nil {
		t.Fatalf("ListChatsByTask(includeArchived=false) error = %v", err)
	}
	if len(activeOnly) != 1 || activeOnly[0].ID != c2.ID {
		t.Fatalf("ListChatsByTask(includeArchived=false) = %+v, want only [%d]", activeOnly, c2.ID)
	}

	withArchived, err := s.ListChatsByTask(task.ID, true)
	if err != nil {
		t.Fatalf("ListChatsByTask(includeArchived=true) error = %v", err)
	}
	if len(withArchived) != 2 {
		t.Fatalf("ListChatsByTask(includeArchived=true) = %+v, want 2 chats", withArchived)
	}
}

func TestStore_RenameChat(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	c, err := s.CreateChat(task.ID, "old title")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	renamed, err := s.RenameChat(c.ID, "new title")
	if err != nil {
		t.Fatalf("RenameChat() error = %v", err)
	}
	if renamed.Title != "new title" {
		t.Fatalf("RenameChat() Title = %q, want %q", renamed.Title, "new title")
	}

	if _, err := s.RenameChat(999999, "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("RenameChat(unknown) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStore_ArchiveChat(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	c, err := s.CreateChat(task.ID, "chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	archived, err := s.ArchiveChat(c.ID)
	if err != nil {
		t.Fatalf("ArchiveChat() error = %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatalf("ArchiveChat() ArchivedAt = nil, want set")
	}
	firstArchival := *archived.ArchivedAt

	time.Sleep(2 * time.Millisecond)
	again, err := s.ArchiveChat(c.ID)
	if err != nil {
		t.Fatalf("ArchiveChat() (repeat) error = %v", err)
	}
	if !again.ArchivedAt.Equal(firstArchival) {
		t.Fatalf("ArchiveChat() (repeat) ArchivedAt = %v, want unchanged %v", again.ArchivedAt, firstArchival)
	}
}

func TestStore_BindChatProvider(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	c, err := s.CreateChat(task.ID, "chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	bound, err := s.BindChatProvider(c.ID, "glm")
	if err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}
	if bound.Provider == nil || *bound.Provider != "glm" {
		t.Fatalf("BindChatProvider() Provider = %v, want \"glm\"", bound.Provider)
	}

	// Binding again with a different provider is a no-op at the store
	// layer -- the immutable-after-bind check is internal/runs.Registry's
	// job (it compares against the existing value before ever calling
	// this), not this method's.
	unchanged, err := s.BindChatProvider(c.ID, "codex-native")
	if err != nil {
		t.Fatalf("BindChatProvider() (rebind) error = %v", err)
	}
	if unchanged.Provider == nil || *unchanged.Provider != "glm" {
		t.Fatalf("BindChatProvider() (rebind) Provider = %v, want unchanged \"glm\"", unchanged.Provider)
	}
}

func TestStore_SetChatAgentSession(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	c, err := s.CreateChat(task.ID, "chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	updated, err := s.SetChatAgentSession(c.ID, `{"provider":"glm","sessionId":"abc"}`)
	if err != nil {
		t.Fatalf("SetChatAgentSession() error = %v", err)
	}
	if updated.AgentSession == nil || *updated.AgentSession != `{"provider":"glm","sessionId":"abc"}` {
		t.Fatalf("SetChatAgentSession() AgentSession = %v, want the stored JSON", updated.AgentSession)
	}
}

func TestStore_DeleteTask_CascadesChats(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	task := newTestTaskForRuns(t, s)
	c, err := s.CreateChat(task.ID, "chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := s.CreateRun(Run{
		ID: "run-1", TaskID: task.ID, ChatID: c.ID, Provider: "glm", Prompt: "hi",
		Status: "done", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	if _, err := s.GetChat(c.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChat() after DeleteTask error = %v, want sql.ErrNoRows", err)
	}
}
