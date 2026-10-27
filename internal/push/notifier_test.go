package push

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSender struct {
	mu      sync.Mutex
	calls   [][]Message
	results []Result // scripted, applied by index; missing = OK
	err     error
}

func (f *fakeSender) Send(_ context.Context, msgs []Message) ([]Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]Message(nil), msgs...))
	if f.err != nil {
		return nil, f.err
	}
	out := make([]Result, len(msgs))
	for i, m := range msgs {
		out[i] = Result{Token: m.Token, OK: true}
		if i < len(f.results) {
			out[i] = f.results[i]
			out[i].Token = m.Token
		}
	}
	return out, nil
}

func (f *fakeSender) messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []Message
	for _, c := range f.calls {
		all = append(all, c...)
	}
	return all
}

type nHarness struct {
	t      *testing.T
	reg    *Registry
	sender *fakeSender
	n      *Notifier
	now    time.Time
	tasks  map[int64]TaskInfo
	appr   func(string) bool

	mu   sync.Mutex
	logs []string
	ids  int
}

func devKey(seed byte) []byte {
	k := make([]byte, KeySize)
	for i := range k {
		k[i] = seed + byte(i)
	}
	return k
}

func newNHarness(t *testing.T) *nHarness {
	t.Helper()
	reg, err := OpenRegistry(filepath.Join(t.TempDir(), "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &nHarness{
		t:      t,
		reg:    reg,
		sender: &fakeSender{},
		now:    time.Unix(1790000000, 0),
		tasks: map[int64]TaskInfo{
			1: {Title: "Root task", Root: true},
			2: {Title: "Child task", Root: false},
			3: {Title: "System task", Root: true, System: true},
			7: {Title: "", Root: true},
		},
	}
	h.n = NewNotifier(Deps{
		Registry: reg,
		Sender:   h.sender,
		Approved: func(id string) bool { return h.appr == nil || h.appr(id) },
		Task: func(id int64) (TaskInfo, bool) {
			info, ok := h.tasks[id]
			return info, ok
		},
		Now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.now
		},
		NewID: func() string {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.ids++
			return fmt.Sprintf("ev-%d", h.ids)
		},
		Logf: func(f string, a ...any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.logs = append(h.logs, fmt.Sprintf(f, a...))
		},
	})
	return h
}

func (h *nHarness) advance(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

func (h *nHarness) add(id string, seed byte, kinds []string) Registration {
	h.t.Helper()
	if kinds == nil {
		kinds = AllKinds
	}
	r := Registration{
		DeviceID: id, Provider: ProviderExpo, Token: "ExponentPushToken[" + id + "tok]",
		Platform: PlatformAndroid, Key: devKey(seed), Kinds: kinds, RegisteredAt: h.now,
	}
	if err := h.reg.Put(r); err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *nHarness) open(r Registration, m Message) Event {
	h.t.Helper()
	ev, err := Open(r.Key, r.DeviceID, m.Data)
	if err != nil {
		h.t.Fatalf("open: %v", err)
	}
	return ev
}

func TestNotifyRulesTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		kind string
		task int64
		want int
	}{
		{"permission child", KindPermission, 2, 1},
		{"needs_continue child", KindNeedsContinue, 2, 1},
		{"done root", KindDone, 1, 1},
		{"done child", KindDone, 2, 0},
		{"error child", KindError, 2, 0},
		{"error root", KindError, 1, 1},
		{"permission system", KindPermission, 3, 0},
		{"needs_continue system", KindNeedsContinue, 3, 0},
		{"done system", KindDone, 3, 0},
		{"error system", KindError, 3, 0},
		{"unknown task", KindPermission, 99, 0},
		{"unknown kind", "bogus", 1, 0},
		{"test kind", KindTest, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newNHarness(t)
			h.add("dev-a", 1, nil)
			got := h.n.Notify(context.Background(), Input{Kind: tc.kind, Key: "k", TaskID: tc.task})
			if got != tc.want || len(h.sender.messages()) != tc.want {
				t.Fatalf("Notify = %d, sent %d, want %d", got, len(h.sender.messages()), tc.want)
			}
		})
	}
}

func TestNotifyPermissionDedupePerRequest(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	ctx := context.Background()
	in := Input{Kind: KindPermission, Key: "req-1", TaskID: 1}
	if h.n.Notify(ctx, in) != 1 || h.n.Notify(ctx, in) != 0 {
		t.Fatal("same key must deliver once")
	}
	in.Key = "req-2"
	if h.n.Notify(ctx, in) != 1 {
		t.Fatal("different key must deliver")
	}
	h.add("dev-b", 2, nil)
	in.Key = "req-1"
	if h.n.Notify(ctx, in) != 1 {
		t.Fatal("same key for a new device must deliver to that device only")
	}
	last := h.sender.calls[len(h.sender.calls)-1]
	if len(last) != 1 || last[0].Token != "ExponentPushToken[dev-btok]" {
		t.Fatalf("last call = %+v", last)
	}
}

func TestNotifyKindsFilter(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, []string{KindPermission})
	if got := h.n.Notify(context.Background(), Input{Kind: KindDone, Key: "k", TaskID: 1}); got != 0 {
		t.Fatalf("done delivered to permission-only device: %d", got)
	}
	if got := h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: "k", TaskID: 1}); got != 1 {
		t.Fatalf("permission = %d", got)
	}
}

func TestNotifyUnapprovedDeviceSkipped(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	b := h.add("dev-b", 2, nil)
	h.appr = func(id string) bool { return id == "dev-b" }
	if got := h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: "k", TaskID: 1}); got != 1 {
		t.Fatalf("Notify = %d", got)
	}
	msgs := h.sender.messages()
	if len(msgs) != 1 || msgs[0].Token != b.Token {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestNotifyRateLimitDoneError(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	ctx := context.Background()
	delivered := 0
	for i := 0; i < 7; i++ {
		delivered += h.n.Notify(ctx, Input{Kind: KindDone, Key: fmt.Sprintf("run%d:done", i), TaskID: 1})
	}
	if delivered != 6 {
		t.Fatalf("delivered = %d, want 6", delivered)
	}
	// Errors share the same window.
	if h.n.Notify(ctx, Input{Kind: KindError, Key: "r:error", TaskID: 1}) != 0 {
		t.Fatal("error should share the done limit")
	}
	for i := 0; i < 10; i++ {
		if h.n.Notify(ctx, Input{Kind: KindPermission, Key: fmt.Sprintf("p%d", i), TaskID: 1}) != 1 {
			t.Fatalf("permission %d dropped", i)
		}
	}
	h.advance(61 * time.Second)
	if h.n.Notify(ctx, Input{Kind: KindDone, Key: "late:done", TaskID: 1}) != 1 {
		t.Fatal("done after window should deliver")
	}
}

func TestNotifyMultiDeviceSealedSeparately(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	regs := []Registration{h.add("dev-a", 1, nil), h.add("dev-b", 50, nil), h.add("dev-c", 100, nil)}
	if got := h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: "k", TaskID: 1, Summary: "x"}); got != 3 {
		t.Fatalf("Notify = %d", got)
	}
	if len(h.sender.calls) != 1 || len(h.sender.calls[0]) != 3 {
		t.Fatalf("calls = %d", len(h.sender.calls))
	}
	msgs := h.sender.calls[0]
	seen := map[string]bool{}
	for i, r := range regs {
		if msgs[i].Token != r.Token {
			t.Fatalf("msg %d token = %s", i, msgs[i].Token)
		}
		h.open(r, msgs[i])
		other := regs[(i+1)%3]
		if _, err := Open(other.Key, r.DeviceID, msgs[i].Data); !errors.Is(err, ErrAuth) {
			t.Fatalf("other key opened blob %d: %v", i, err)
		}
		if seen[msgs[i].Data.C] {
			t.Fatal("blobs are identical")
		}
		seen[msgs[i].Data.C] = true
	}
}

func TestNotifyBodyContent(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	r := h.add("dev-a", 1, nil)
	ctx := context.Background()
	cases := []struct {
		in    Input
		title string
		body  string
	}{
		{Input{Kind: KindPermission, Key: "1", TaskID: 1, Summary: "run rm"}, "Root task", "Needs approval: run rm"},
		{Input{Kind: KindPermission, Key: "2", TaskID: 1}, "Root task", "A run needs your approval"},
		{Input{Kind: KindNeedsContinue, Key: "3", TaskID: 1}, "Root task", "Reached the turn cap. Open to continue."},
		{Input{Kind: KindDone, Key: "4", TaskID: 1}, "Root task", "Run finished."},
		{Input{Kind: KindError, Key: "5", TaskID: 1, Err: "boom"}, "Root task", "Run failed: boom"},
		{Input{Kind: KindError, Key: "6", TaskID: 1}, "Root task", "Run failed"},
		{Input{Kind: KindError, Key: "autopause:1:1", TaskID: 1, Summary: "Automation paused: 3 failed runs"}, "Root task", "Automation paused: 3 failed runs"},
		{Input{Kind: KindPermission, Key: "7", TaskID: 7}, "Task 7", "A run needs your approval"},
	}
	for i, tc := range cases {
		tc.in.ChatID, tc.in.RunID = 9, "run-x"
		if h.n.Notify(ctx, tc.in) != 1 {
			t.Fatalf("case %d not delivered", i)
		}
		msgs := h.sender.messages()
		ev := h.open(r, msgs[len(msgs)-1])
		if ev.Title != tc.title || ev.Body != tc.body || ev.Kind != tc.in.Kind || ev.TaskID != tc.in.TaskID ||
			ev.ChatID != 9 || ev.RunID != "run-x" || ev.At != h.now.Unix() || ev.ID == "" {
			t.Fatalf("case %d event = %+v", i, ev)
		}
	}
}

func TestNotifyDeviceNotRegisteredRemovesRegistration(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.add("dev-b", 2, nil)
	h.sender.results = []Result{{ErrCode: "DeviceNotRegistered"}, {OK: true}}
	if got := h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: "k", TaskID: 1}); got != 2 {
		t.Fatalf("Notify = %d", got)
	}
	if _, ok := h.reg.Get("dev-a"); ok {
		t.Fatal("dev-a should be removed")
	}
	if _, ok := h.reg.Get("dev-b"); !ok {
		t.Fatal("dev-b should stay")
	}
}

func TestNotifyInvalidCredentialsKeepsRegistration(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.sender.results = []Result{{ErrCode: "InvalidCredentials"}}
	h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: "k", TaskID: 1})
	if _, ok := h.reg.Get("dev-a"); !ok {
		t.Fatal("registration must be kept")
	}
}

func TestNotifySenderErrorKeepsRegistrations(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.add("dev-b", 2, nil)
	h.sender.err = errors.New("network down")
	if got := h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: "k", TaskID: 1}); got != 2 {
		t.Fatalf("Notify = %d, want 2", got)
	}
	if len(h.reg.List()) != 2 {
		t.Fatal("registrations changed")
	}
}

func TestNotifyNoRegistrationsNoSend(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	if got := h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: "k", TaskID: 1}); got != 0 {
		t.Fatalf("Notify = %d", got)
	}
	if len(h.sender.calls) != 0 {
		t.Fatal("sender called")
	}
}

func TestNotifyLogsAreSecretFree(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.tasks[1] = TaskInfo{Title: "Secret Title Zebra", Root: true}
	bad := Registration{
		DeviceID: "bad-device-1", Provider: ProviderExpo, Token: "ExponentPushToken[badtok]",
		Platform: PlatformAndroid, Key: []byte{1, 2, 3, 4, 5}, Kinds: AllKinds, RegisteredAt: h.now,
	}
	if err := h.reg.Put(bad); err != nil {
		t.Fatal(err)
	}
	regs := []Registration{
		h.add("dev-gone", 1, nil), h.add("dev-rej", 20, nil), h.add("dev-ok", 40, nil),
	}
	// List order: bad-device-1, dev-gone, dev-ok, dev-rej.
	h.sender.results = []Result{{ErrCode: "DeviceNotRegistered"}, {OK: true}, {ErrCode: "MismatchSenderId"}}
	ctx := context.Background()
	h.n.Notify(ctx, Input{Kind: KindPermission, Key: "k1", TaskID: 1, Summary: "summary-text-quokka"})
	h.sender.results = nil
	h.sender.err = errors.New("post failed for ExponentPushToken[dev-oktok]")
	h.n.Notify(ctx, Input{Kind: KindError, Key: "k2", TaskID: 1, Err: "run-error-narwhal"})

	if len(h.logs) == 0 {
		t.Fatal("expected some logs")
	}
	secrets := []string{"Secret Title Zebra", "summary-text-quokka", "run-error-narwhal", "badtok"}
	for _, r := range append(regs, bad) {
		secrets = append(secrets, r.Token, hex.EncodeToString(r.Key), base64.StdEncoding.EncodeToString(r.Key))
	}
	for _, line := range h.logs {
		for _, s := range secrets {
			if strings.Contains(line, s) {
				t.Fatalf("log line %q contains %q", line, s)
			}
		}
	}
}

func TestNotifyConcurrent(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	h.add("dev-b", 2, nil)
	const workers, per = 8, 5
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: fmt.Sprintf("w%d-%d", w, i), TaskID: 1})
				// Repeat the same key: must not be delivered twice.
				h.n.Notify(context.Background(), Input{Kind: KindPermission, Key: fmt.Sprintf("w%d-%d", w, i), TaskID: 1})
			}
		}()
	}
	wg.Wait()
	if got, want := len(h.sender.messages()), workers*per*2; got != want {
		t.Fatalf("messages = %d, want %d", got, want)
	}
}

func TestNotifyStalledSealedNoPlaintext(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	r := h.add("dev-a", 1, nil)
	// A stalled seat is not a run kind: a child task is notified too.
	if got := h.n.Notify(context.Background(), Input{Kind: KindStalled, Key: "stall:2:100", TaskID: 2, Minutes: 15}); got != 1 {
		t.Fatalf("Notify = %d, want 1", got)
	}
	msgs := h.sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages, want 1", len(msgs))
	}
	raw, err := json.Marshal(msgs[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"Child task", "Tree stalled", "idle for"} {
		if strings.Contains(string(raw), plain) {
			t.Fatalf("sealed data leaks %q: %s", plain, raw)
		}
	}
	ev := h.open(r, msgs[0])
	if ev.Kind != KindStalled || ev.TaskID != 2 || ev.Title != "Child task" || ev.Body != "Tree stalled: Child task has been idle for 15 min" {
		t.Fatalf("opened event = %+v", ev)
	}
}

func TestStalledDedupeByEpisodeKey(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	h.add("dev-a", 1, nil)
	ctx := context.Background()
	in := Input{Kind: KindStalled, Key: "stall:1:100", TaskID: 1, Minutes: 15}
	if h.n.Notify(ctx, in) != 1 || h.n.Notify(ctx, in) != 0 {
		t.Fatal("same episode key must deliver once")
	}
	in.Key = "stall:1:200"
	if h.n.Notify(ctx, in) != 1 {
		t.Fatal("a new episode (new since) must deliver again")
	}
	// Stalled alerts are not subject to the done/error rate limit.
	for i := 0; i < 10; i++ {
		in.Key = fmt.Sprintf("stall:1:%d", 1000+i)
		if h.n.Notify(ctx, in) != 1 {
			t.Fatalf("stalled alert %d was rate limited", i)
		}
	}
}

// An automation auto-pause with no task to open is still pushed (a pause needs
// a person): it is addressed to the automation, carries its id and title, and
// is not dropped by the done/error rate window.
func TestNotifyAutomationPauseWithoutTask(t *testing.T) {
	t.Parallel()
	h := newNHarness(t)
	reg := h.add("dev-a", 1, nil)
	ctx := context.Background()
	for i := 0; i < 6; i++ { // fill the done/error window
		if h.n.Notify(ctx, Input{Kind: KindDone, Key: fmt.Sprintf("run%d:done", i), TaskID: 1}) != 1 {
			t.Fatalf("done %d dropped", i)
		}
	}
	in := Input{Kind: KindError, Key: "autopause:3:0:1000", AutomationID: 3, Title: "Automation paused: nightly", Summary: "Automation paused: the template is no longer valid"}
	if h.n.Notify(ctx, in) != 1 {
		t.Fatal("the pause was not delivered (task-less, or dropped by the rate window)")
	}
	msgs := h.sender.messages()
	ev := h.open(reg, msgs[len(msgs)-1])
	if ev.TaskID != 0 || ev.AutomationID != 3 || ev.Title != "Automation paused: nightly" || ev.Body != "Automation paused: the template is no longer valid" || ev.Kind != KindError {
		t.Fatalf("event = %+v", ev)
	}
	if h.n.Notify(ctx, in) != 0 {
		t.Fatal("the same pause was delivered twice")
	}
	// Only an error-kind alert may skip the task lookup: an unknown task still delivers nothing.
	if h.n.Notify(ctx, Input{Kind: KindDone, Key: "x", TaskID: 0, AutomationID: 3}) != 0 {
		t.Fatal("a done alert for task 0 was delivered")
	}
}
