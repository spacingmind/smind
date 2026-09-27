package workspace

import "testing"

func TestManager_CreateTask_CreatesDefaultChat(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)

	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Fix the bug")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	chats, err := m.ListChats(task.ID, true)
	if err != nil {
		t.Fatalf("ListChats() error = %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("ListChats() = %+v, want exactly 1 default chat", chats)
	}
	if chats[0].Title != defaultChatTitle {
		t.Fatalf("default chat title = %q, want %q", chats[0].Title, defaultChatTitle)
	}
	if chats[0].Provider != nil {
		t.Fatalf("default chat Provider = %v, want nil (unbound)", chats[0].Provider)
	}

	def, err := m.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if def.ID != chats[0].ID {
		t.Fatalf("DefaultChat() = %+v, want the task's only chat %+v", def, chats[0])
	}
}

func TestManager_CreateChat(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	second, err := m.CreateChat(task.ID, "Second chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if second.Title != "Second chat" {
		t.Fatalf("CreateChat() Title = %q, want %q", second.Title, "Second chat")
	}

	chats, err := m.ListChats(task.ID, true)
	if err != nil {
		t.Fatalf("ListChats() error = %v", err)
	}
	if len(chats) != 2 {
		t.Fatalf("ListChats() = %+v, want 2 chats (default + created)", chats)
	}

	// DefaultChat must still resolve to the task's original (oldest) chat,
	// not the newly created one -- old clients that never learn about
	// chats must keep landing on the same conversation.
	def, err := m.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if def.ID == second.ID {
		t.Fatalf("DefaultChat() = %+v, want the original default chat, not the newly created one", def)
	}
}

func TestManager_CreateChat_UnknownTask(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)

	if _, err := m.CreateChat(999999, "x"); err == nil {
		t.Fatal("CreateChat(unknown task) error = nil, want an error")
	}
}

func TestManager_RenameChat(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	def, err := m.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}

	renamed, err := m.RenameChat(def.ID, "Renamed")
	if err != nil {
		t.Fatalf("RenameChat() error = %v", err)
	}
	if renamed.Title != "Renamed" {
		t.Fatalf("RenameChat() Title = %q, want %q", renamed.Title, "Renamed")
	}

	if _, err := m.RenameChat(def.ID, ""); err == nil {
		t.Fatal("RenameChat(empty title) error = nil, want an error")
	}
}

func TestManager_ArchiveChat(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	repo := newTestRepo(t)
	w, err := m.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := m.CreateTask(w.ID, nil, "Task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	def, err := m.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}

	archived, err := m.ArchiveChat(def.ID)
	if err != nil {
		t.Fatalf("ArchiveChat() error = %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatalf("ArchiveChat() ArchivedAt = nil, want set")
	}

	activeOnly, err := m.ListChats(task.ID, false)
	if err != nil {
		t.Fatalf("ListChats(includeArchived=false) error = %v", err)
	}
	if len(activeOnly) != 0 {
		t.Fatalf("ListChats(includeArchived=false) = %+v, want none (the only chat is archived)", activeOnly)
	}
}
