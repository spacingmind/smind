package runs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestRegistry_Start_OmittedChatID_LandsOnDefaultChat proves ADR-0016
// P1.3's backward-compat path: a Start call with chatID 0 (task.prompt/
// run.start's omitted wire chatId) resolves to the task's default chat --
// the one workspace.Manager.CreateTask always creates.
func TestRegistry_Start_OmittedChatID_LandsOnDefaultChat(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	def, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	_, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status.ChatID != def.ID {
		t.Fatalf("ChatID = %d, want the task's default chat %d", status.ChatID, def.ID)
	}
}

// TestRegistry_Start_ExplicitChatID_LandsOnThatChat proves a Start call
// naming a specific chat lands its run there, not the task's default chat.
func TestRegistry_Start_ExplicitChatID_LandsOnThatChat(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	second, err := wm.CreateChat(task.ID, "Second chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, second.ID, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	_, status, err := reg.History(runID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status.ChatID != second.ID {
		t.Fatalf("ChatID = %d, want %d", status.ChatID, second.ID)
	}
}

// TestRegistry_Start_ChatIDFromDifferentTask_IsRejected proves a chatId
// belonging to a different task than taskId is a clear error, not a
// same-worktree convenience.
func TestRegistry_Start_ChatIDFromDifferentTask_IsRejected(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	taskA := newTestTask(t, wm, "")
	taskB := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	chatB, err := wm.DefaultChat(taskB.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}

	if _, err := reg.Start(context.Background(), wm, runner, taskA.ID, chatB.ID, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, ""); err == nil {
		t.Fatal("Start() with a chatId from a different task: error = nil, want an error")
	}
}

// TestRegistry_Start_BindsProviderOnFirstRun proves a chat's provider binds
// on its first run and stays bound, readable back via wm.GetChat.
func TestRegistry_Start_BindsProviderOnFirstRun(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	def, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	if def.Provider != nil {
		t.Fatalf("default chat Provider = %v before any run, want nil", def.Provider)
	}

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, def.ID, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	bound, err := wm.GetChat(def.ID)
	if err != nil {
		t.Fatalf("GetChat() error = %v", err)
	}
	if bound.Provider == nil || *bound.Provider != string(taskrunner.ProviderGLM) {
		t.Fatalf("GetChat() Provider = %v, want %q", bound.Provider, taskrunner.ProviderGLM)
	}
}

// TestRegistry_Start_ProviderMismatchOnBoundChat_IsRejected proves a second
// prompt to an already-bound chat with a different provider is rejected --
// ADR-0016 P1.3's "provider mismatch on a bound chat is rejected" scenario.
func TestRegistry_Start_ProviderMismatchOnBoundChat_IsRejected(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	def, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, def.ID, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, runID, StatusDone, 5*time.Second)

	if _, err := reg.Start(context.Background(), wm, runner, task.ID, def.ID, taskrunner.ProviderCodexNative, "hi again", taskrunner.ApprovalPolicyManual, ""); err == nil {
		t.Fatal("Start() with a mismatched provider on a bound chat: error = nil, want an error")
	}
}

// TestRegistry_Start_SecondPromptToBusyChat_IsRejected proves at most one
// running run per chat (ADR-0016 P1.5): a second Start on a chat that
// already has a running run is rejected.
func TestRegistry_Start_SecondPromptToBusyChat_IsRejected(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	runID, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(runID) })
	waitForStatus(t, reg, runID, StatusRunning, 2*time.Second)

	if _, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi again", taskrunner.ApprovalPolicyManual, ""); err == nil {
		t.Fatal("Start() on a busy chat: error = nil, want an error")
	}
}

// TestRegistry_Start_TwoChatsOfSameTask_RunConcurrently proves the
// concurrency guard is scoped to a chat, not its task: two chats of the
// same task may each have a running run at the same time.
func TestRegistry_Start_TwoChatsOfSameTask_RunConcurrently(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "hang")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	def, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	second, err := wm.CreateChat(task.ID, "Second chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	runA, err := reg.Start(context.Background(), wm, runner, task.ID, def.ID, taskrunner.ProviderGLM, "hi a", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() chat A error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(runA) })

	runB, err := reg.Start(context.Background(), wm, runner, task.ID, second.ID, taskrunner.ProviderGLM, "hi b", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() chat B error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Stop(runB) })

	waitForStatus(t, reg, runA, StatusRunning, 2*time.Second)
	waitForStatus(t, reg, runB, StatusRunning, 2*time.Second)
}

// TestRegistry_Start_ConcurrentFirstPromptsWithDifferentProviders_OnlyOneBinds
// proves the race window in provider-binding a never-run chat: two
// concurrent Start calls both observe Provider == nil and both attempt to
// bind, but store.BindChatProvider's write-if-still-NULL means only one
// actually wins -- the other must be rejected as a provider mismatch
// against whichever provider won, not silently proceed as if it had bound
// its own.
func TestRegistry_Start_ConcurrentFirstPromptsWithDifferentProviders_OnlyOneBinds(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	def, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	providers := []taskrunner.Provider{taskrunner.ProviderGLM, taskrunner.ProviderCodexNative}
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := reg.Start(context.Background(), wm, runner, task.ID, def.ID, providers[i], "hi", taskrunner.ApprovalPolicyManual, "")
			results[i] = err
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("results = %v, want exactly one of the two concurrent binds to succeed", results)
	}

	bound, err := wm.GetChat(def.ID)
	if err != nil {
		t.Fatalf("GetChat() error = %v", err)
	}
	if bound.Provider == nil {
		t.Fatal("GetChat() Provider = nil, want bound to whichever provider won")
	}
}

// TestRegistry_List_FiltersByChatID proves run.list's additive chatId
// filter (ADR-0016 P1.4): List(chatID) returns only that chat's runs, and
// List(0) is unfiltered, exactly as before this ADR.
func TestRegistry_List_FiltersByChatID(t *testing.T) {
	t.Parallel()
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	runner := newTestRunner(wm)
	reg := newTestRegistry(t, st)

	def, err := wm.DefaultChat(task.ID)
	if err != nil {
		t.Fatalf("DefaultChat() error = %v", err)
	}
	second, err := wm.CreateChat(task.ID, "Second chat")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}

	runA, err := reg.Start(context.Background(), wm, runner, task.ID, def.ID, taskrunner.ProviderGLM, "hi a", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() chat A error = %v", err)
	}
	waitForStatus(t, reg, runA, StatusDone, 5*time.Second)

	runB, err := reg.Start(context.Background(), wm, runner, task.ID, second.ID, taskrunner.ProviderGLM, "hi b", taskrunner.ApprovalPolicyManual, "")
	if err != nil {
		t.Fatalf("Start() chat B error = %v", err)
	}
	waitForStatus(t, reg, runB, StatusDone, 5*time.Second)

	filtered := reg.List(def.ID)
	if len(filtered) != 1 || filtered[0].ID != runA {
		t.Fatalf("List(%d) = %+v, want exactly [%s]", def.ID, filtered, runA)
	}

	all := reg.List(0)
	if len(all) != 2 {
		t.Fatalf("List(0) = %+v, want both runs", all)
	}
}
