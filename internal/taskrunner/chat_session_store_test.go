package taskrunner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// newTestStoreAndManager returns a *store.Store and a workspace.Manager
// backed by the same on-disk database, at path so a caller (the reopen
// test) can later re-Open it at the same location to simulate a daemon
// restart -- newTestWorkspaceManager (taskrunner_test.go) doesn't expose
// the underlying store or its path, which ChatSessionStore needs directly.
func newTestStoreAndManager(t *testing.T) (*store.Store, *workspace.Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smind.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	return s, workspace.New(s), path
}

func TestChatSessionStore_GetSet_RoundTrips(t *testing.T) {
	t.Parallel()
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	chat, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	// Bind the chat's provider first -- ChatSessionStore.Get only honors a
	// handle whose Provider matches the chat's own bound provider column.
	if _, err := wm.BindChatProvider(chat.ID, string(ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}

	s := NewChatSessionStore(st)

	if _, ok := s.Get(chat.ID); ok {
		t.Fatal("Get() before any Set() = ok, want no handle")
	}

	want := SessionHandle{Provider: ProviderGLM, SessionID: "sess-1"}
	s.Set(chat.ID, want)

	got, ok := s.Get(chat.ID)
	if !ok {
		t.Fatal("Get() after Set() = not ok, want the stored handle")
	}
	if got.Provider != want.Provider || got.SessionID != want.SessionID {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
}

func TestChatSessionStore_TwoChatsOfOneTask_KeepSeparateHandles(t *testing.T) {
	t.Parallel()
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	chatA, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	chatB, err := wm.CreateChat(task.ID, "Second chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if _, err := wm.BindChatProvider(chatA.ID, string(ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider(A) error = %v", err)
	}
	if _, err := wm.BindChatProvider(chatB.ID, string(ProviderCodexNative)); err != nil {
		t.Fatalf("BindChatProvider(B) error = %v", err)
	}

	s := NewChatSessionStore(st)
	s.Set(chatA.ID, SessionHandle{Provider: ProviderGLM, SessionID: "sess-a"})
	s.Set(chatB.ID, SessionHandle{Provider: ProviderCodexNative, SessionID: "sess-b"})

	gotA, ok := s.Get(chatA.ID)
	if !ok || gotA.SessionID != "sess-a" {
		t.Fatalf("Get(chatA) = %+v, ok=%v, want sess-a", gotA, ok)
	}
	gotB, ok := s.Get(chatB.ID)
	if !ok || gotB.SessionID != "sess-b" {
		t.Fatalf("Get(chatB) = %+v, ok=%v, want sess-b", gotB, ok)
	}
}

func TestChatSessionStore_HandleSurvivesStoreReopen(t *testing.T) {
	t.Parallel()
	st, wm, path := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	chat, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if _, err := wm.BindChatProvider(chat.ID, string(ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}

	NewChatSessionStore(st).Set(chat.ID, SessionHandle{Provider: ProviderGLM, SessionID: "sess-restart"})

	// Close and reopen at the same path -- simulating a daemon restart --
	// rather than reusing st, which the reopened store must not need.
	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatalf("re-Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("Close() (reopened) error = %v", err)
		}
	})

	got, ok := NewChatSessionStore(reopened).Get(chat.ID)
	if !ok {
		t.Fatal("Get() after reopen = not ok, want the handle to survive")
	}
	if got.SessionID != "sess-restart" {
		t.Fatalf("Get() after reopen = %+v, want SessionID %q", got, "sess-restart")
	}
}

func TestChatSessionStore_MigratedDefaultChat_StartsWithNoHandle(t *testing.T) {
	t.Parallel()
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	chat, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	// A freshly created (or migrated) chat has agent_session NULL and no
	// provider bound yet -- Get must report "no handle", not an error, so
	// the very next run starts a fresh session (ADR-0016's "migrated
	// default chats resume starting from their next run").
	if _, ok := NewChatSessionStore(st).Get(chat.ID); ok {
		t.Fatal("Get() on an unbound, session-less chat = ok, want no handle")
	}
}

func TestChatSessionStore_Get_ProviderMismatchAgainstBoundChat_IsIgnored(t *testing.T) {
	t.Parallel()
	st, wm, _ := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	chat, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if _, err := wm.BindChatProvider(chat.ID, string(ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}

	s := NewChatSessionStore(st)
	// Write a handle for a provider that doesn't match the chat's bound
	// provider column -- shouldn't happen in normal operation (Set is only
	// ever called with the same provider RunPrompt was just driven with,
	// which Registry.Start already validated against the bound provider),
	// but Get must still never hand back a mismatched handle.
	s.Set(chat.ID, SessionHandle{Provider: ProviderCodexNative, SessionID: "sess-wrong-provider"})

	if _, ok := s.Get(chat.ID); ok {
		t.Fatal("Get() with a handle whose Provider mismatches the bound chat = ok, want no handle")
	}
}

// TestRunner_RunPrompt_WithChatSessionStore_ResumesAcrossRunsAndRestart
// proves the ChatSessionStore integration end to end through the real
// Runner.RunPrompt path (not just ChatSessionStore's own Get/Set): a
// second RunPrompt call on the same chat resumes (session/load) using the
// handle the first call persisted to chats.agent_session, and that handle
// is readable through a freshly reopened store -- simulating the handle
// surviving a daemon restart the same way
// TestChatSessionStore_HandleSurvivesStoreReopen proves for the store
// layer alone.
func TestRunner_RunPrompt_WithChatSessionStore_ResumesAcrossRunsAndRestart(t *testing.T) {
	t.Parallel()
	st, wm, path := newTestStoreAndManager(t)
	task := newTestTaskUnder(t, wm)
	chat, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if _, err := wm.BindChatProvider(chat.ID, string(ProviderGLM)); err != nil {
		t.Fatalf("BindChatProvider() error = %v", err)
	}

	r := New(wm, WithSessionStore(NewChatSessionStore(st)))
	r.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		return acp.New([]string{fakeACPAgentPath, "loadSession"}, opts...)
	}

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, chat.ID, ProviderGLM, "hi", nil, "", "", events)
	}()
	drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() (first run) error = %v", err)
	}
	if m := sessionInitMethod(t, *task.WorktreePath); m != "session/new" {
		t.Fatalf("first run session-init-method = %q, want %q", m, "session/new")
	}

	stored, err := st.GetChat(chat.ID)
	if err != nil {
		t.Fatalf("GetChat() error = %v", err)
	}
	if stored.AgentSession == nil {
		t.Fatal("chats.agent_session is still NULL after a successful run, want it persisted")
	}

	events2 := make(chan Event)
	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- r.RunPrompt(context.Background(), task.ID, chat.ID, ProviderGLM, "what did I say before?", nil, "", "", events2)
	}()
	drainEvents(events2)
	if err := <-errCh2; err != nil {
		t.Fatalf("RunPrompt() (second run) error = %v", err)
	}
	if m := sessionInitMethod(t, *task.WorktreePath); m != "session/load" {
		t.Fatalf("second run session-init-method = %q, want %q (resumed)", m, "session/load")
	}

	// Simulate a daemon restart: reopen the store and drive a third run
	// through a brand-new Runner/ChatSessionStore pair, proving the resume
	// handle survived independently of any in-process state.
	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatalf("re-Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("Close() (reopened) error = %v", err)
		}
	})

	wm2 := workspace.New(reopened)
	r2 := New(wm2, WithSessionStore(NewChatSessionStore(reopened)))
	r2.newACPClient = func(_ []string, opts ...acp.Option) (acpBackend, error) {
		return acp.New([]string{fakeACPAgentPath, "loadSession"}, opts...)
	}
	events3 := make(chan Event)
	errCh3 := make(chan error, 1)
	go func() {
		errCh3 <- r2.RunPrompt(context.Background(), task.ID, chat.ID, ProviderGLM, "and after that?", nil, "", "", events3)
	}()
	drainEvents(events3)
	if err := <-errCh3; err != nil {
		t.Fatalf("RunPrompt() (post-restart run) error = %v", err)
	}
	if m := sessionInitMethod(t, *task.WorktreePath); m != "session/load" {
		t.Fatalf("post-restart run session-init-method = %q, want %q (resumed after simulated restart)", m, "session/load")
	}
}

// newTestTaskUnder is newTestTask (taskrunner_test.go) without the
// scenario-file argument, for tests that don't need a fake-agent scenario
// and already have their own *workspace.Manager (ChatSessionStore's tests
// need the underlying *store.Store too, which newTestTask's own helper,
// newTestWorkspaceManager, doesn't expose).
func newTestTaskUnder(t *testing.T, wm *workspace.Manager) store.Task {
	t.Helper()
	repo := newTestRepo(t)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}
