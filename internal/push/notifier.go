package push

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Notification kinds (ADR-0040 §4). KindTest is only ever sent by the test
// push RPC, never through Notify.
const (
	KindPermission    = "permission"
	KindNeedsContinue = "needs_continue"
	KindDone          = "done"
	KindError         = "error"
	KindTest          = "test"
)

// AllKinds is every kind a device can subscribe to, in canonical order.
var AllKinds = []string{KindPermission, KindNeedsContinue, KindDone, KindError}

const (
	dedupeCap      = 512
	rateLimitCount = 6
	rateLimitSpan  = 60 * time.Second
	errDeviceGone  = "DeviceNotRegistered"
)

// Input is one daemon event the notifier may turn into pushes.
type Input struct {
	Kind    string // permission | needs_continue | done | error
	Key     string // dedupe key: permission = the requestId; run events = runId + ":" + kind
	TaskID  int64
	ChatID  int64
	RunID   string
	Summary string // permission summary
	Err     string // run error text (kind error)
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
	return &Notifier{
		d:       d,
		seen:    make(map[string]struct{}),
		history: make(map[string][]time.Time),
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
