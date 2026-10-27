package push

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Notification kinds (ADR-0040 §4). KindTest is only ever sent by the test
// push RPC, never through Notify.
const (
	KindPermission    = "permission"
	KindNeedsContinue = "needs_continue"
	KindDone          = "done"
	KindError         = "error"
	KindStalled       = "stalled"
	KindTest          = "test"
)

// AllKinds is every kind a device can subscribe to, in canonical order.
var AllKinds = []string{KindPermission, KindNeedsContinue, KindDone, KindError, KindStalled}

const (
	// QueueSize bounds the async queue Enqueue feeds.
	QueueSize = 256

	dedupeCap      = 512
	rateLimitCount = 6
	rateLimitSpan  = 60 * time.Second
	errDeviceGone  = "DeviceNotRegistered"
)

// Input is one daemon event the notifier may turn into pushes.
type Input struct {
	Kind    string // permission | needs_continue | done | error | stalled
	Key     string // dedupe key: permission = the requestId; run events = runId + ":" + kind; stalled = "stall:<taskId>:<since unix>"
	TaskID  int64
	ChatID  int64
	RunID   string
	Summary string // permission summary; for kind error, a body that replaces "Run failed"
	Err     string // run error text (kind error)
	Minutes int    // idle minutes (kind stalled)
}

// TaskInfo is what the notifier needs to know about a task.
type TaskInfo struct {
	Title  string
	Root   bool // no parent task
	System bool
}

// Deps are the notifier's collaborators.
type Deps struct {
	Registry *Registry
	Sender   Sender
	Approved func(deviceID string) bool          // nil = every registered device counts as approved
	Task     func(taskID int64) (TaskInfo, bool) // required
	Now      func() time.Time                    // default time.Now
	NewID    func() string                       // event id; default: 16 random bytes as hex
	Logf     func(format string, args ...any)    // default no-op
}

// Notifier applies the ADR-0040 §4 rules to daemon events and delivers sealed
// pushes. It is safe for concurrent use. It never logs tokens, keys or any
// text derived from tasks or runs.
type Notifier struct {
	d Deps

	mu      sync.Mutex
	seen    map[string]struct{}
	order   []string               // FIFO of seen keys, oldest first
	history map[string][]time.Time // per device: done/error push times

	queue   chan Input
	ctx     context.Context // cancelled by Close; passed to Notify by the worker
	cancel  context.CancelFunc
	dropped atomic.Uint64
	closed  atomic.Bool

	lifeMu  sync.Mutex
	started bool
	done    chan struct{} // closed when the worker exits
}

// NewNotifier returns a Notifier using d, filling defaults for Now, NewID and Logf.
func NewNotifier(d Deps) *Notifier {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = randomID
	}
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Notifier{
		d:       d,
		seen:    make(map[string]struct{}),
		history: make(map[string][]time.Time),
		queue:   make(chan Input, QueueSize),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
}

// Start launches the single worker goroutine that drains the queue. It is
// idempotent and does nothing after Close.
func (n *Notifier) Start() {
	n.lifeMu.Lock()
	defer n.lifeMu.Unlock()
	if n.started || n.closed.Load() {
		return
	}
	n.started = true
	go n.run()
}

func (n *Notifier) run() {
	defer close(n.done)
	for {
		select {
		case <-n.ctx.Done():
			return
		case in := <-n.queue:
			n.safeNotify(in)
		}
	}
}

// safeNotify keeps a panic in one delivery from killing the daemon.
func (n *Notifier) safeNotify(in Input) {
	defer func() {
		if recover() != nil {
			n.d.Logf("push: notifier panic recovered")
		}
	}()
	n.Notify(n.ctx, in)
}

// Enqueue hands in to the worker without ever blocking. It returns false when
// the queue is full or Close was called.
func (n *Notifier) Enqueue(in Input) bool {
	if n.closed.Load() {
		return false
	}
	select {
	case n.queue <- in:
		return true
	default:
		c := n.dropped.Add(1)
		if isPowerOfTen(c) {
			n.d.Logf("push: queue full, dropped %d events so far", c)
		}
		return false
	}
}

func isPowerOfTen(c uint64) bool {
	for c >= 10 && c%10 == 0 {
		c /= 10
	}
	return c == 1
}

// Close stops accepting events, aborts an in-flight Send through its context
// and waits for the worker to exit. It is idempotent and safe without Start.
func (n *Notifier) Close() {
	n.lifeMu.Lock()
	n.closed.Store(true)
	started := n.started
	n.lifeMu.Unlock()
	n.cancel()
	if started {
		<-n.done
	}
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func idPrefix(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// redactTokens blanks every token of msgs in text so a provider or transport
// error that echoes a request body cannot leak them into the log.
func redactTokens(text string, msgs []Message) string {
	for _, m := range msgs {
		if m.Token != "" {
			text = strings.ReplaceAll(text, m.Token, "[redacted]")
		}
	}
	return text
}

func isRunKind(kind string) bool { return kind == KindDone || kind == KindError }

// Notify applies the rules to in and delivers one batch through the Sender.
// It blocks for the Send and returns how many messages were handed to it.
func (n *Notifier) Notify(ctx context.Context, in Input) int {
	if !slices.Contains(AllKinds, in.Kind) {
		return 0
	}
	info, ok := n.d.Task(in.TaskID)
	if !ok || info.System {
		return 0
	}
	if isRunKind(in.Kind) && !info.Root {
		return 0
	}

	var msgs []Message
	var devices []string // device id per msgs entry
	for _, reg := range n.d.Registry.List() {
		if n.d.Approved != nil && !n.d.Approved(reg.DeviceID) {
			continue
		}
		if !slices.Contains(reg.Kinds, in.Kind) {
			continue
		}
		dkey := reg.DeviceID + "\x00" + in.Key
		now := n.d.Now()
		if !n.admit(reg.DeviceID, dkey, in.Kind, now) {
			continue
		}
		blob, err := Seal(reg.Key, reg.DeviceID, n.event(in, info, now))
		if err != nil {
			n.d.Logf("push: seal failed for device %s", idPrefix(reg.DeviceID))
			n.release(reg.DeviceID, dkey, in.Kind)
			continue
		}
		msgs = append(msgs, Message{Token: reg.Token, Platform: reg.Platform, Data: blob})
		devices = append(devices, reg.DeviceID)
	}
	if len(msgs) == 0 {
		return 0
	}

	results, err := n.d.Sender.Send(ctx, msgs)
	if err != nil {
		n.d.Logf("push: send failed: %s", redactTokens(err.Error(), msgs))
	}
	removed := 0
	for i, res := range results {
		if i >= len(msgs) {
			break
		}
		if res.OK {
			continue
		}
		if res.ErrCode == errDeviceGone {
			if c, rerr := n.d.Registry.RemoveToken(msgs[i].Token); rerr == nil {
				removed += c
			}
			continue
		}
		n.d.Logf("push: provider rejected a message (code %s, device %s)", res.ErrCode, idPrefix(devices[i]))
	}
	if removed > 0 {
		n.d.Logf("push: removed %d unregistered device(s)", removed)
	}
	return len(msgs)
}

// admit atomically checks dedupe and the done/error rate limit for one device
// and, when the message may go out, records both decisions.
func (n *Notifier) admit(deviceID, dkey, kind string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, dup := n.seen[dkey]; dup {
		return false
	}
	if isRunKind(kind) {
		h := n.history[deviceID]
		cut := 0
		for cut < len(h) && now.Sub(h[cut]) >= rateLimitSpan {
			cut++
		}
		h = h[cut:]
		if len(h) >= rateLimitCount {
			n.history[deviceID] = h
			return false
		}
		n.history[deviceID] = append(h, now)
	}
	n.seen[dkey] = struct{}{}
	n.order = append(n.order, dkey)
	if len(n.order) > dedupeCap {
		delete(n.seen, n.order[0])
		n.order = n.order[1:]
	}
	return true
}

// release undoes admit's bookkeeping when sealing failed, so the failure does
// not burn a dedupe slot or a rate-limit slot.
func (n *Notifier) release(deviceID, dkey, kind string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.seen, dkey)
	if i := slices.Index(n.order, dkey); i >= 0 {
		n.order = slices.Delete(n.order, i, i+1)
	}
	if isRunKind(kind) {
		if h := n.history[deviceID]; len(h) > 0 {
			n.history[deviceID] = h[:len(h)-1]
		}
	}
}

func (n *Notifier) event(in Input, info TaskInfo, now time.Time) Event {
	title := info.Title
	if title == "" {
		title = "Task " + strconv.FormatInt(in.TaskID, 10)
	}
	var body string
	switch in.Kind {
	case KindPermission:
		if in.Summary == "" {
			body = "A run needs your approval"
		} else {
			body = "Needs approval: " + in.Summary
		}
	case KindNeedsContinue:
		body = "Reached the turn cap. Open to continue."
	case KindDone:
		body = "Run finished."
	case KindError:
		body = "Run failed"
		if in.Err != "" {
			body += ": " + in.Err
		}
		if in.Summary != "" { // a daemon-authored alert on the failed task (an automation auto-pause)
			body = in.Summary
		}
	case KindStalled:
		body = "Tree stalled: " + title + " has been idle for " + strconv.Itoa(in.Minutes) + " min"
	}
	return Event{
		ID:     n.d.NewID(),
		Kind:   in.Kind,
		TaskID: in.TaskID,
		RunID:  in.RunID,
		ChatID: in.ChatID,
		Title:  title,
		Body:   body,
		At:     now.Unix(),
	}
}

// TestResult is the outcome of one device of a test push.
type TestResult struct {
	DeviceID string `json:"deviceId"`
	Sent     bool   `json:"sent"`
	Error    string `json:"error,omitempty"`
}

// Test seals a kind "test" event for each listed device and sends ONE batch.
// It is an operator action, so it bypasses the kinds filter, dedupe and the
// rate limit, but not the approved check. Results come back in the order of
// deviceIDs; Error is one of not_registered, not_approved, seal_failed,
// send_failed or the provider's error code (DeviceNotRegistered also removes
// the registration, as in Notify). Nothing secret is logged.
func (n *Notifier) Test(ctx context.Context, deviceIDs []string) []TestResult {
	out := make([]TestResult, len(deviceIDs))
	var msgs []Message
	var slot []int // out index per msgs entry
	now := n.d.Now()
	for i, id := range deviceIDs {
		out[i].DeviceID = id
		reg, ok := n.d.Registry.Get(id)
		if !ok {
			out[i].Error = "not_registered"
			continue
		}
		if n.d.Approved != nil && !n.d.Approved(id) {
			out[i].Error = "not_approved"
			continue
		}
		blob, err := Seal(reg.Key, reg.DeviceID, Event{
			ID:    n.d.NewID(),
			Kind:  KindTest,
			Title: "smind",
			Body:  "Test notification",
			At:    now.Unix(),
		})
		if err != nil {
			n.d.Logf("push: seal failed for device %s", idPrefix(id))
			out[i].Error = "seal_failed"
			continue
		}
		msgs = append(msgs, Message{Token: reg.Token, Platform: reg.Platform, Data: blob})
		slot = append(slot, i)
	}
	if len(msgs) == 0 {
		return out
	}

	results, err := n.d.Sender.Send(ctx, msgs)
	if err != nil {
		n.d.Logf("push: test send failed: %s", redactTokens(err.Error(), msgs))
		results = nil
	}
	removed := 0
	for j, i := range slot {
		if j >= len(results) {
			out[i].Error = "send_failed"
			continue
		}
		res := results[j]
		if res.OK {
			out[i].Sent = true
			continue
		}
		out[i].Error = res.ErrCode
		if out[i].Error == "" {
			out[i].Error = "send_failed"
		}
		if res.ErrCode == errDeviceGone {
			if c, rerr := n.d.Registry.RemoveToken(msgs[j].Token); rerr == nil {
				removed += c
			}
			continue
		}
		n.d.Logf("push: provider rejected a test message (code %s, device %s)", res.ErrCode, idPrefix(deviceIDs[i]))
	}
	if removed > 0 {
		n.d.Logf("push: removed %d unregistered device(s)", removed)
	}
	return out
}
