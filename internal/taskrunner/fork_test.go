package taskrunner

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func runClaudeTurn(t *testing.T, r *Runner, taskID int64, prompt string) []Event {
	t.Helper()
	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), taskID, taskID, ProviderClaudeNative, prompt, nil, PermissionSettings{}, "", testClaudeModel, events)
	}()
	got := drainEvents(events)
	if err := <-errCh; err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	return got
}

// A pending fork launches the first turn with --resume of the parent's session,
// --fork-session and --resume-session-at; the next turn resumes the child's own
// session (the id the CLI reported) and never forks again.
func TestRunner_ForkHandleUsesResumeForkAndResumeSessionAt(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "parent-session", Fork: &ForkAt{EntryUUID: "entry-42"}})

	runClaudeTurn(t, r, task.ID, "try it another way")
	first := readArgsFile(t, *task.WorktreePath)
	for _, want := range []string{"--resume=parent-session", "--fork-session", "--resume-session-at=entry-42"} {
		if !strings.Contains(first, want) {
			t.Fatalf("first fork turn args = %q, want %s", first, want)
		}
	}
	if h, _ := r.sessionStore.Get(task.ID); h.Fork != nil || h.SessionID == "parent-session" {
		t.Fatalf("handle after the first turn = %+v, want the child's own session and no fork marker", h)
	}

	runClaudeTurn(t, r, task.ID, "and then?")
	second := readArgsFile(t, *task.WorktreePath)
	if !strings.Contains(second, "--resume=sess-1") || strings.Contains(second, "--fork-session") || strings.Contains(second, "--resume-session-at") {
		t.Fatalf("second turn args = %q, want a plain resume of the child's session", second)
	}
}

// A fork whose resume fails at start-up degrades like any stale session: a fresh
// session and a session note, never a failed turn.
func TestRunner_ForkFallsBackToFreshSessionWhenResumeFails(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "resume-fails")
	r := claudeNativeRunner(t, wm)
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "gone", Fork: &ForkAt{EntryUUID: "entry-1"}})

	got := runClaudeTurn(t, r, task.ID, "hi")
	if !hasSessionNote(got) || got[len(got)-1].Type != EventTypeDone {
		t.Fatalf("events = %+v, want a session note and a finished turn", got)
	}
}

// A handle without a fork marker behaves exactly as before: no fork flags.
func TestRunner_PlainHandleNeverForks(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "plain"})
	runClaudeTurn(t, r, task.ID, "hi")
	args := readArgsFile(t, *task.WorktreePath)
	if strings.Contains(args, "--fork-session") || strings.Contains(args, "--resume-session-at") {
		t.Fatalf("args = %q, want no fork flags for a plain handle", args)
	}
}

type totalsSpyStore struct {
	*MemorySessionStore
	mu  sync.Mutex
	got []*ClaudeUsageTotals
}

func (s *totalsSpyStore) LogClaudeTotals(_ int64, t *ClaudeUsageTotals) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, t)
}

// A claude turn's end totals go to the store that keeps a log, once per turn.
func TestRunner_ClaudeTurnLogsItsTotals(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	spy := &totalsSpyStore{MemorySessionStore: NewMemorySessionStore()}
	r.sessionStore = spy
	runClaudeTurn(t, r, task.ID, "hi")
	runClaudeTurn(t, r, task.ID, "again")
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.got) != 2 || spy.got[0] == nil {
		t.Fatalf("logged totals = %v, want one per turn", spy.got)
	}
}

// The marker is cleared as soon as the CLI reports the child's own session id,
// not only at the result: a first turn that dies after the init message leaves a
// handle that resumes the child's session and never forks the parent again.
func TestRunner_ForkMarkerClearedAtInitEvenWhenTheTurnDies(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "init-then-exit")
	r := claudeNativeRunner(t, wm)
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "parent-session", Fork: &ForkAt{EntryUUID: "entry-42"}})

	events := make(chan Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.RunPrompt(context.Background(), task.ID, task.ID, ProviderClaudeNative, "hi", nil, PermissionSettings{}, "", testClaudeModel, events)
	}()
	drainEvents(events)
	<-errCh // the turn failed: that is the point
	h, ok := r.sessionStore.Get(task.ID)
	if !ok || h.SessionID != "fork-sess" || h.Fork != nil {
		t.Fatalf("handle after the dead turn = %+v, want the child's session fork-sess and no fork marker", h)
	}
}

func TestValidForkEntry(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		"00000000-0000-4000-8000-000000000019": true,
		"abcdef01":                             true,
		"":                                     false,
		"a":                                    false,
		"-leading-dash-0123":                   false,
		"has space 0123456":                    false,
		"new\nline-0123456":                    false,
		"with=equals-0123":                     false,
	} {
		if got := ValidForkEntry(s); got != want {
			t.Errorf("ValidForkEntry(%q) = %v, want %v", s, got, want)
		}
	}
}

// A marker whose entry id has an odd shape (an edited transcript) never reaches the
// CLI as a fork flag: the turn resumes the parent's session plainly.
func TestRunner_ForkWithMalformedEntryNeverPassesForkFlags(t *testing.T) {
	t.Parallel()
	wm, task := newTestTask(t, "echo-args")
	r := claudeNativeRunner(t, wm)
	r.sessionStore.Set(task.ID, SessionHandle{Provider: ProviderClaudeNative, SessionID: "parent-session", Fork: &ForkAt{EntryUUID: "--dangerous=1"}})
	runClaudeTurn(t, r, task.ID, "hi")
	args := readArgsFile(t, *task.WorktreePath)
	if strings.Contains(args, "--fork-session") || strings.Contains(args, "--resume-session-at") {
		t.Fatalf("args = %q, want no fork flags for a malformed entry id", args)
	}
}
