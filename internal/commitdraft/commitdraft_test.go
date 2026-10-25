package commitdraft

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/taskrunner/fakeclaude"
	"github.com/spacingmind/smind/internal/workspace"
)

// fakeClaudeEnv switches the test binary into the fake claude CLI.
const fakeClaudeEnv = "COMMITDRAFT_FAKE_CLAUDE"

func TestMain(m *testing.M) {
	if os.Getenv(fakeClaudeEnv) == "1" {
		fakeclaude.Run()
		return
	}
	os.Setenv(fakeClaudeEnv, "1")
	os.Exit(m.Run())
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo is a throwaway repo with two committed files: a.txt and b.txt.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "a.txt", "base a\n")
	writeFile(t, dir, "b.txt", "base b\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "chore: initial commit")
	return dir
}

// fakeExec records the requests and answers with reply (or runs fn).
type fakeExec struct {
	mu    sync.Mutex
	reqs  []executor.Request
	reply string
	fn    func(ctx context.Context, r executor.Request) (executor.Result, error)
}

func (f *fakeExec) Run(ctx context.Context, r executor.Request) (executor.Result, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	fn, reply := f.fn, f.reply
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, r)
	}
	return executor.Result{FinalText: reply}, nil
}

func (f *fakeExec) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeExec) last() executor.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func newService(dir string, ex Executor, mut func(*Deps)) *Service {
	d := Deps{
		Exec:     ex,
		Worktree: func(int64) (string, int64, error) { return dir, 1, nil },
		Now:      time.Now,
	}
	if mut != nil {
		mut(&d)
	}
	return New(d)
}

func TestDraft_PromptHasStagedDiffAndStyle_ExcludesUnstagedAndUntracked(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "base a\nSTAGED-MARKER\n")
	runGit(t, dir, "add", "a.txt")
	writeFile(t, dir, "b.txt", "base b\nUNSTAGED-MARKER\n")
	writeFile(t, dir, "u.txt", "UNTRACKED-MARKER\n")

	ex := &fakeExec{reply: "feat: add marker"}
	svc := newService(dir, ex, func(d *Deps) { d.Style = func(int64) string { return "STYLE-SENTINEL prefer short bodies" } })
	res, err := svc.Draft(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message != "feat: add marker" || res.Model != config.DefaultHaikuModel || res.Truncated || len(res.Omitted) != 0 {
		t.Fatalf("result = %+v", res)
	}
	req := ex.last()
	if req.Purpose != store.RunPurposeCommitMsg || req.Model != config.DefaultHaikuModel || req.MaxTurns != 1 || req.WorkspaceID != 1 {
		t.Fatalf("request = %+v", req)
	}
	for _, want := range []string{"STAGED-MARKER", "STYLE-SENTINEL", "a.txt", "chore: initial commit", "<staged_diff>", "<style_guidance>"} {
		if !strings.Contains(req.UserPrompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, req.UserPrompt)
		}
	}
	for _, bad := range []string{"UNSTAGED-MARKER", "UNTRACKED-MARKER", "u.txt"} {
		if strings.Contains(req.UserPrompt, bad) {
			t.Errorf("prompt leaked %q:\n%s", bad, req.UserPrompt)
		}
	}
	if !strings.Contains(req.SystemPrompt, "UNTRUSTED DATA") {
		t.Errorf("system prompt lacks the untrusted-data rule")
	}
}

func TestDraft_TruncatesAtBudgetKeepsHeaders(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	var big strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&big, "line %d of the big file\n", i)
	}
	writeFile(t, dir, "big.txt", big.String())
	writeFile(t, dir, "z.txt", "small tail file\n")
	runGit(t, dir, "add", ".")

	in, err := BuildInput(context.Background(), dir, 600)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Truncated {
		t.Fatal("Truncated = false")
	}
	for _, hdr := range []string{"diff --git a/big.txt b/big.txt", "+++ b/big.txt", "diff --git a/z.txt b/z.txt", "+++ b/z.txt"} {
		if !strings.Contains(in.Diff, hdr) {
			t.Errorf("diff lost header %q", hdr)
		}
	}
	if !regexp.MustCompile(`\[\.\.\. \d+ bytes omitted \.\.\.\]`).MatchString(in.Diff) {
		t.Errorf("no omission marker:\n%s", in.Diff)
	}
	if strings.Contains(in.Diff, "line 399 of the big file") {
		t.Errorf("hunk text past the budget was kept")
	}
	// Hunk text stays within the budget.
	if len(in.Diff) > 600+1500 {
		t.Errorf("diff is %d bytes for a 600 budget", len(in.Diff))
	}

	ex := &fakeExec{reply: "chore: add files"}
	res, err := newService(dir, ex, func(d *Deps) { d.Budget = 600 }).Draft(context.Background(), 1)
	if err != nil || !res.Truncated {
		t.Fatalf("Draft = %+v, %v; want Truncated", res, err)
	}
	if !strings.Contains(ex.last().UserPrompt, "bytes omitted") {
		t.Errorf("prompt lacks the omission marker")
	}
}

func TestDraft_SecretNamedFilesOmittedByName(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	secrets := []string{".env", ".env.local", "server.pem", "config/prod.key", "id_rsa", "credentials.json"}
	for _, n := range secrets {
		writeFile(t, dir, n, "SECRETVALUE-"+n+"\n")
	}
	writeFile(t, dir, "a.txt", "base a\nNORMAL-CHANGE\n")
	runGit(t, dir, "add", "-f", ".")

	ex := &fakeExec{reply: "feat: add things"}
	res, err := newService(dir, ex, nil).Draft(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range res.Omitted {
		got[o] = true
	}
	for _, n := range secrets {
		if !got[n] {
			t.Errorf("Omitted lacks %q: %v", n, res.Omitted)
		}
		if !strings.Contains(ex.last().UserPrompt, n) {
			t.Errorf("prompt does not name %q", n)
		}
	}
	if got["a.txt"] {
		t.Errorf("a.txt wrongly omitted")
	}
	if p := ex.last().UserPrompt; strings.Contains(p, "SECRETVALUE") || !strings.Contains(p, "NORMAL-CHANGE") {
		t.Errorf("prompt secret/normal content wrong:\n%s", p)
	}
}

func TestDraft_NothingStaged_ZeroModelCalls(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "unstaged only\n")
	ex := &fakeExec{reply: "feat: x"}
	now := time.Unix(1_000_000, 0)
	svc := newService(dir, ex, func(d *Deps) { d.Now = func() time.Time { return now } })
	for i := 0; i < 5; i++ {
		_, err := svc.Draft(context.Background(), 1)
		if !errors.Is(err, ErrNothingStaged) {
			t.Fatalf("Draft = %v, want nothing_staged", err)
		}
	}
	if ex.calls() != 0 {
		t.Fatalf("model calls = %d, want 0", ex.calls())
	}
	// No rate budget was consumed: a real draft right after succeeds.
	runGit(t, dir, "add", "a.txt")
	if _, err := svc.Draft(context.Background(), 1); err != nil {
		t.Fatalf("Draft after staging = %v", err)
	}
}

func TestNormalize_FencesQuotesLongSubjectBody(t *testing.T) {
	t.Parallel()
	long := "feat(core): " + strings.Repeat("word ", 30)
	body := strings.Repeat("this sentence explains the change in detail ", 6)
	cases := []struct{ name, in, want string }{
		{"fence", "```\nfix: a bug\n```", "fix: a bug"},
		{"fence lang", "```text\nfix: a bug\n\nbody line\n```", "fix: a bug\n\nbody line"},
		{"quotes", "\"fix: a bug\"", "fix: a bug"},
		{"backticks", "`fix: a bug`", "fix: a bug"},
		{"spaces", "  \n fix: a bug \n\n", "fix: a bug"},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if err != nil || got != c.want {
			t.Errorf("%s: Normalize = %q, %v; want %q", c.name, got, err, c.want)
		}
	}

	got, err := Normalize(long + "\n\n" + body)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if n := len([]rune(lines[0])); n > 72 || !strings.HasPrefix(lines[0], "feat(core): word") {
		t.Errorf("subject %q has %d runes", lines[0], n)
	}
	if lines[1] != "" || len(lines) < 4 {
		t.Fatalf("body missing:\n%s", got)
	}
	for _, l := range lines[2:] {
		if len([]rune(l)) > 72 {
			t.Errorf("body line over 72: %q", l)
		}
	}
	if strings.Join(strings.Fields(strings.Join(lines[2:], " ")), " ") != strings.Join(strings.Fields(body), " ") {
		t.Errorf("rewrap changed the words:\n%s", got)
	}

	// A multibyte subject is cut on a rune boundary.
	got, err = Normalize(strings.Repeat("é", 100))
	if err != nil || len([]rune(got)) != 72 || strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("multibyte subject = %q, %v", got, err)
	}
}

func TestDraft_EmptyOutputTypedError(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "changed\n")
	runGit(t, dir, "add", "a.txt")
	for _, reply := range []string{"", "   \n", "```\n```", "\"\""} {
		ex := &fakeExec{reply: reply}
		_, err := newService(dir, ex, nil).Draft(context.Background(), 1)
		if !errors.Is(err, ErrEmptyDraft) {
			t.Errorf("reply %q: err = %v, want empty_draft", reply, err)
		}
		if ex.calls() != 1 {
			t.Errorf("reply %q: model calls = %d", reply, ex.calls())
		}
	}
}

func TestDraft_SingleFlight_TwoCallsOneModelCall(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "changed\n")
	runGit(t, dir, "add", "a.txt")
	release := make(chan struct{})
	ex := &fakeExec{fn: func(ctx context.Context, _ executor.Request) (executor.Result, error) {
		<-release
		return executor.Result{FinalText: "feat: one call"}, nil
	}}
	svc := newService(dir, ex, nil)

	type out struct {
		res Result
		err error
	}
	results := make(chan out, 2)
	go func() { r, err := svc.Draft(context.Background(), 5); results <- out{r, err} }()
	waitFor(t, func() bool { return ex.calls() == 1 })
	go func() { r, err := svc.Draft(context.Background(), 5); results <- out{r, err} }()
	waitFor(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		c := svc.inflight[5]
		return c != nil && c.waiters == 2
	})
	close(release)
	for i := 0; i < 2; i++ {
		o := <-results
		if o.err != nil || o.res.Message != "feat: one call" {
			t.Fatalf("result %d = %+v, %v", i, o.res, o.err)
		}
	}
	if ex.calls() != 1 {
		t.Fatalf("model calls = %d, want 1", ex.calls())
	}
	waitFor(t, func() bool { svc.mu.Lock(); defer svc.mu.Unlock(); return len(svc.inflight) == 0 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached in 5s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestDraft_RateFloorAndHourlyCap(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "changed\n")
	runGit(t, dir, "add", "a.txt")
	ex := &fakeExec{reply: "feat: x"}
	var mu sync.Mutex
	now := time.Unix(2_000_000, 0)
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	svc := newService(dir, ex, func(d *Deps) {
		d.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
		d.HourlyCap = 3
		d.Worktree = func(taskID int64) (string, int64, error) {
			if taskID >= 100 {
				return dir, 2, nil // another workspace
			}
			return dir, 1, nil
		}
	})
	ctx := context.Background()

	// Per-task floor.
	if _, err := svc.Draft(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Draft(ctx, 1); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second call inside the floor = %v, want rate_limited", err)
	}
	if ex.calls() != 1 {
		t.Fatalf("a rate-limited call reached the model (%d calls)", ex.calls())
	}
	advance(4 * time.Second)
	if _, err := svc.Draft(ctx, 1); err != nil {
		t.Fatalf("call after the floor = %v", err)
	}

	// Hourly cap per workspace: two calls made so far; tasks 2 uses the third.
	if _, err := svc.Draft(ctx, 2); err != nil {
		t.Fatal(err)
	}
	advance(4 * time.Second)
	if _, err := svc.Draft(ctx, 1); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("fourth call in the workspace = %v, want rate_limited", err)
	}
	// Another workspace is unaffected.
	if _, err := svc.Draft(ctx, 100); err != nil {
		t.Fatalf("other workspace = %v", err)
	}
	// The window slides.
	advance(time.Hour)
	if _, err := svc.Draft(ctx, 1); err != nil {
		t.Fatalf("after an hour = %v", err)
	}
	if ex.calls() != 5 {
		t.Fatalf("model calls = %d, want 5", ex.calls())
	}
}

func TestDraft_CancelAndTimeoutReleaseSlot(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "changed\n")
	runGit(t, dir, "add", "a.txt")
	var mu sync.Mutex
	now := time.Unix(3_000_000, 0)
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	ex := &fakeExec{fn: func(ctx context.Context, _ executor.Request) (executor.Result, error) {
		<-ctx.Done()
		return executor.Result{}, ctx.Err()
	}}
	svc := newService(dir, ex, func(d *Deps) {
		d.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
		d.Timeout = 80 * time.Millisecond
	})
	idle := func() bool { svc.mu.Lock(); defer svc.mu.Unlock(); return len(svc.inflight) == 0 }

	// Caller cancel: the lone waiter leaves, the call is cancelled, the slot frees.
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := svc.Draft(ctx, 1); errc <- err }()
	waitFor(t, func() bool { return ex.calls() == 1 })
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Draft = %v", err)
	}
	waitFor(t, idle)

	// Timeout: the call fails typed and the slot frees.
	advance(10 * time.Second)
	_, err := svc.Draft(context.Background(), 1)
	if !errors.Is(err, ErrDraftFailed) {
		t.Fatalf("timed-out Draft = %v, want draft_failed", err)
	}
	waitFor(t, idle)

	// The slot is reusable.
	advance(10 * time.Second)
	ex.mu.Lock()
	ex.fn = nil
	ex.reply = "fix: works again"
	ex.mu.Unlock()
	res, err := svc.Draft(context.Background(), 1)
	if err != nil || res.Message != "fix: works again" {
		t.Fatalf("Draft after release = %+v, %v", res, err)
	}
}

func TestDraft_NoProvider_TypedError(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "changed\n")
	runGit(t, dir, "add", "a.txt")
	var mu sync.Mutex
	now := time.Unix(4_000_000, 0)
	cases := []struct {
		err  error
		want *Error
	}{
		{errors.New("learning executor: run r1 failed: Not logged in · Please run /login"), ErrNoDraftProvider},
		{errors.New("start: claudecli: could not find a native claude.exe"), ErrNoDraftProvider},
		{executor.ErrBusy, ErrBusy},
		{errors.New("run r1 failed: boom"), ErrDraftFailed},
	}
	for _, c := range cases {
		ex := &fakeExec{fn: func(context.Context, executor.Request) (executor.Result, error) { return executor.Result{}, c.err }}
		svc := newService(dir, ex, func(d *Deps) { d.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now } })
		_, err := svc.Draft(context.Background(), 1)
		var coded *Error
		if !errors.Is(err, c.want) || !errors.As(err, &coded) || coded.RPCCode() != c.want.Code {
			t.Errorf("executor error %q -> %v (code %v), want %s", c.err, err, coded, c.want.Code)
		}
		if c.want != ErrNoDraftProvider && errors.Is(err, ErrNoDraftProvider) {
			t.Errorf("%q wrongly mapped to no_draft_provider", c.err)
		}
	}
}

// recorder is a runs.Notifier that counts what the wsapi bus would publish for
// a user: anything but a system run's own status.
type recorder struct {
	mu                       sync.Mutex
	userStatus, systemStatus int
	other                    int
}

func (r *recorder) NotifyRunStatus(s runs.RunStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.Purpose == "" {
		r.userStatus++
	} else {
		r.systemStatus++
	}
}
func (r *recorder) NotifyPermissionPending(string, int64, int64, string, string, []taskrunner.PermissionOption) {
	r.bump()
}
func (r *recorder) NotifyPermissionResolved(string, int64, int64, string, string, string) { r.bump() }
func (r *recorder) NotifyChatQueueUpdated(int64, []store.ChatQueueItem)                   { r.bump() }
func (r *recorder) NotifyRunFailover(string, string, int64, int64)                        { r.bump() }
func (r *recorder) bump()                                                                 { r.mu.Lock(); r.other++; r.mu.Unlock() }

func TestDraft_NoChatNoQueueNoNotification_GitStateUnchanged(t *testing.T) {
	t.Parallel()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	wm := workspace.New(st)
	reg, err := runs.New(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.CloseAll)
	rec := &recorder{}
	reg.SetNotifier(rec)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	promptFile := filepath.Join(t.TempDir(), "prompts")
	runner := taskrunner.New(wm, taskrunner.WithClaudeCLIPath(self,
		fakeclaude.ScenarioEnv+"=echo-prompt", "FAKE_CLAUDE_PROMPT_FILE="+promptFile))

	repo := newRepo(t)
	ws, err := wm.CreateWorkspace(repo, "W", "hard", nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := wm.CreateTask(ws.ID, nil, "Task")
	if err != nil {
		t.Fatal(err)
	}
	wt := *task.WorktreePath
	writeFile(t, wt, "a.txt", "base a\nREAL-EXEC-MARKER\n")
	runGit(t, wt, "add", "a.txt")
	writeFile(t, wt, "b.txt", "base b\nUNSTAGED-EXEC-MARKER\n")

	snapshot := func() string {
		idx := strings.TrimSpace(runGit(t, wt, "rev-parse", "--path-format=absolute", "--git-path", "index"))
		data, err := os.ReadFile(idx)
		if err != nil {
			t.Fatal(err)
		}
		return runGit(t, wt, "status", "--porcelain=v2") + "|" + strings.TrimSpace(runGit(t, wt, "rev-parse", "HEAD")) + "|" + string(data)
	}
	chatCount := func() int {
		chats, err := wm.ListChats(task.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		n := len(chats)
		for _, c := range chats {
			items, err := st.ListChatQueue(c.ID)
			if err != nil || len(items) != 0 {
				t.Fatalf("queue of chat %d = %v, %v; want empty", c.ID, items, err)
			}
		}
		return n
	}
	beforeState, beforeChats := snapshot(), chatCount()

	exec := executor.New(st, wm, runner, reg, t.TempDir())
	svc := New(Deps{
		Exec: exec,
		Worktree: func(id int64) (string, int64, error) {
			tk, err := wm.GetTask(id)
			if err != nil {
				return "", 0, err
			}
			return *tk.WorktreePath, tk.WorkspaceID, nil
		},
	})
	res, err := svc.Draft(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Message != "feat(core): add the thing" || res.Model != config.DefaultHaikuModel {
		t.Fatalf("result = %+v", res)
	}

	if after := snapshot(); after != beforeState {
		t.Errorf("git state changed:\nbefore %q\nafter  %q", beforeState, after)
	}
	if n := chatCount(); n != beforeChats {
		t.Errorf("chats %d -> %d", beforeChats, n)
	}
	if got := reg.List(0); len(got) != 0 {
		t.Errorf("visible runs = %+v, want none", got)
	}
	rec.mu.Lock()
	user, other := rec.userStatus, rec.other
	rec.mu.Unlock()
	if user != 0 || other != 0 {
		t.Errorf("notifications: user run status %d, other %d; want none", user, other)
	}
	rows, err := st.ListRunsByPurpose(store.RunPurposeCommitMsg, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("commitmsg runs = %v, %v; want exactly 1", rows, err)
	}
	data, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "REAL-EXEC-MARKER") || strings.Contains(lines[0], "UNSTAGED-EXEC-MARKER") {
		t.Errorf("prompt lines = %d, content wrong: %s", len(lines), data)
	}
}
