// Package inbox is the Human console's read model (ADR-0045 §5): one list of
// what needs a person, DERIVED on every read from existing state (pending
// permission requests, validator verdicts, stall alerts), plus a small table of
// what the Human did to each item. Nothing here answers, approves, merges or
// edits the things it lists.
package inbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/validation"
)

// Item kinds.
const (
	KindPermission  = "permission"
	KindVerdict     = "verdict"
	KindStall       = "stall"
	KindHostPair    = "host_pair"
	KindHostDown    = "host_down"
	KindHostRefused = "host_refused"
	KindRunFailed   = "run_failed"
)

// Item states.
const (
	StateNew      = "new"
	StateSeen     = "seen"
	StateSnoozed  = "snoozed"
	StateResolved = "resolved"
)

// MaxSnooze is the longest a non-permission item may be snoozed.
const MaxSnooze = 24 * time.Hour

// MaxSummary caps an item's summary.
const MaxSummary = 160

// HostDownAfter is how long a host link must be down before it earns an
// inbox row (ADR-0048 S9): a flap or a brief reconnect must not ping the
// Human.
const HostDownAfter = 5 * time.Minute

// ErrNotAllowed marks an action the rules refuse (a permission cannot be
// snoozed or resolved; a snooze is capped).
var ErrNotAllowed = errors.New("not allowed")

// Item is one inbox row.
type Item struct {
	Key          string     `json:"key"`
	Kind         string     `json:"kind"`
	TaskID       int64      `json:"taskId,omitempty"`
	Title        string     `json:"title"`
	Summary      string     `json:"summary"`
	CreatedAt    time.Time  `json:"createdAt"`
	State        string     `json:"state"`
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`
	Attention    string     `json:"attention"` // high | normal
	Links        Links      `json:"links"`
	// Action is the next step of a run_failed item (failure.Action), omitted
	// for every other kind.
	Action *failure.Action `json:"action,omitempty"`
}

// Links point at what the item is about.
type Links struct {
	TaskID              int64  `json:"taskId,omitempty"`
	RunID               string `json:"runId,omitempty"`
	PermissionRequestID string `json:"permissionRequestId,omitempty"`
	ValidationID        int64  `json:"validationId,omitempty"`
	HostID              string `json:"hostId,omitempty"`
}

// Counts is what the badge reads.
type Counts struct {
	Total  int            `json:"total"`  // visible items: not resolved, not snoozed
	Unseen int            `json:"unseen"` // items in state new, plus every pending permission
	ByKind map[string]int `json:"byKind"`
}

// Filter narrows List.
type Filter struct {
	Kinds  []string
	States []string // default: new, seen (snoozed and resolved are listed only when asked for)
	Limit  int
}

// HostPair is a peer host whose pairing request awaits this human's
// approval (ADR-0048 S9). Every string is peer-supplied.
type HostPair struct {
	HostID string
	Alias  string
	Label  string
	Since  time.Time
}

// HostLinkIssue is a host link that is down or refused. Kind is "down" or
// "refused"; Code is the typed refusal (e.g. "peer_identity_changed",
// "fleet_version_unsupported") and Guidance the peer's update hint. Every
// string is peer-supplied.
type HostLinkIssue struct {
	HostID   string
	Alias    string
	Kind     string
	Code     string
	Guidance string
	Since    time.Time
}

// FailedRun is a task's latest run when it ended in error with a typed
// failure and no run has followed it (run failure classes plan, AC16). Kind is
// the failureKind, Action the daemon-built next step.
type FailedRun struct {
	RunID      string
	TaskID     int64
	Kind       string
	Action     *failure.Action
	FinishedAt time.Time
}

// Service derives the inbox. Every source is injected.
type Service struct {
	DB *store.Store
	// Permissions lists the live pending permission requests.
	Permissions func() []runs.PendingPermission
	// Validations returns the newest validation view of every live task that has one.
	Validations func() ([]validation.View, error)
	// Stalls returns the currently stalled seats (task id -> stalled since).
	Stalls func() map[int64]time.Time
	// HostPairs lists peer hosts awaiting this human's approval. Nil when
	// fleet is off: nothing is derived.
	HostPairs func() []HostPair
	// HostLinkIssues lists host links that are down or refused. Nil when
	// fleet is off: nothing is derived.
	HostLinkIssues func() []HostLinkIssue
	// FailedRuns lists tasks whose latest run failed with a typed class. Nil:
	// nothing is derived.
	FailedRuns func() []FailedRun
	// TaskTitle names a task ("" when unknown).
	TaskTitle func(taskID int64) string
	Now       func() time.Time

	mu        sync.Mutex
	firstSeen map[string]time.Time // permission keys: when this process first saw them
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) title(id int64) string {
	if s.TaskTitle != nil {
		return s.TaskTitle(id)
	}
	return ""
}

// PermissionKey is the key of a pending permission request.
func PermissionKey(requestID string) string { return "perm:" + requestID }

// IsPermissionKey reports whether key names a permission item.
func IsPermissionKey(key string) bool { return strings.HasPrefix(key, "perm:") }

// derive builds every current item (state not yet applied).
func (s *Service) derive() ([]Item, error) {
	now := s.now()
	var items []Item

	if s.Permissions != nil {
		s.mu.Lock()
		if s.firstSeen == nil {
			s.firstSeen = map[string]time.Time{}
		}
		live := map[string]bool{}
		for _, p := range s.Permissions() {
			key := PermissionKey(p.RequestID)
			live[key] = true
			if _, ok := s.firstSeen[key]; !ok {
				s.firstSeen[key] = now
			}
			items = append(items, Item{
				Key: key, Kind: KindPermission, TaskID: p.TaskID, Title: s.title(p.TaskID),
				Summary: PermissionSummary(p.Detail), CreatedAt: s.firstSeen[key], Attention: "high",
				Links: Links{TaskID: p.TaskID, RunID: p.RunID, PermissionRequestID: p.RequestID},
			})
		}
		for k := range s.firstSeen {
			if !live[k] {
				delete(s.firstSeen, k)
			}
		}
		s.mu.Unlock()
	}

	if s.Validations != nil {
		views, err := s.Validations()
		if err != nil {
			return nil, err
		}
		for _, v := range views {
			if it, ok := verdictItem(v, s.title(v.TaskID)); ok {
				items = append(items, it)
			}
		}
	}

	if s.Stalls != nil {
		for taskID, since := range s.Stalls() {
			items = append(items, Item{
				Key: fmt.Sprintf("stall:%d:%d", taskID, since.Unix()), Kind: KindStall, TaskID: taskID, Title: s.title(taskID),
				Summary: "This task tree has gone quiet", CreatedAt: since, Attention: "normal", Links: Links{TaskID: taskID},
			})
		}
	}

	if s.FailedRuns != nil {
		for _, fr := range s.FailedRuns() {
			if it, ok := runFailedItem(fr, s.title(fr.TaskID)); ok {
				items = append(items, it)
			}
		}
	}

	if s.HostPairs != nil {
		for _, p := range s.HostPairs() {
			items = append(items, Item{
				Key: KindHostPair + ":" + p.HostID, Kind: KindHostPair, Title: clip(p.Alias, MaxSummary),
				Summary:   clip("Pairing request: confirm the code on both hosts and choose what it may do", MaxSummary),
				CreatedAt: p.Since, Attention: "high", Links: Links{HostID: p.HostID},
			})
		}
	}

	if s.HostLinkIssues != nil {
		for _, is := range s.HostLinkIssues() {
			switch is.Kind {
			case "down":
				// Constant summary text: the fingerprint hashes summaries,
				// so wording that tracked the clock would churn it every read.
				if now.Sub(is.Since) <= HostDownAfter {
					continue
				}
				items = append(items, Item{
					Key: fmt.Sprintf("host_down:%s:%d", is.HostID, is.Since.Unix()), Kind: KindHostDown, Title: clip(is.Alias, MaxSummary),
					Summary: "The link to this host is down", CreatedAt: is.Since, Attention: "normal", Links: Links{HostID: is.HostID},
				})
			case "refused":
				summary := is.Guidance
				if summary == "" {
					summary = "The link was refused: " + is.Code
				}
				items = append(items, Item{
					Key: KindHostRefused + ":" + is.HostID + ":" + is.Code, Kind: KindHostRefused, Title: clip(is.Alias, MaxSummary),
					Summary: clip(summary, MaxSummary), CreatedAt: is.Since, Attention: "high", Links: Links{HostID: is.HostID},
				})
			}
		}
	}
	return items, nil
}

// runFailedItem is the inbox row of a failed run, or false when the class has
// none: cli_exited keeps the footer's Continue, a transient_429 whose retry is
// still queued is not yet a problem, and an unknown class is not guessed at.
// A human-fix class (auth_expired, model_not_in_plan) is high: only a person
// can fix it. The key and CreatedAt come from the run, so reads are stable.
func runFailedItem(fr FailedRun, title string) (Item, bool) {
	attention := "normal"
	switch fr.Kind {
	case failure.KindAuthExpired, failure.KindModelNotInPlan:
		attention = "high"
	case failure.KindPlanWindowExhausted, failure.KindRateLimited:
	case failure.KindTransient429:
		if fr.Action != nil && fr.Action.Kind == failure.ActionAutoRetry {
			return Item{}, false
		}
	default:
		return Item{}, false
	}
	summary := "The run failed: " + fr.Kind
	if fr.Action != nil && fr.Action.Label != "" {
		summary = fr.Action.Label
	}
	return Item{
		Key: "runfail:" + fr.RunID, Kind: KindRunFailed, TaskID: fr.TaskID, Title: title, Summary: clip(summary, MaxSummary),
		CreatedAt: fr.FinishedAt, Attention: attention, Links: Links{TaskID: fr.TaskID, RunID: fr.RunID}, Action: fr.Action,
	}, true
}

// PermissionSummary builds the redacted line a permission item shows: the tool
// kind and, for a file, its path; never the command text. The full request
// (path, diff, command) stays in the permission card.
func PermissionSummary(d *taskrunner.PermissionDetail) string {
	if d == nil {
		return "A tool needs your permission"
	}
	switch d.Kind {
	case "execute":
		return "Run a command"
	case "write", "edit":
		verb := "Write"
		if d.Kind == "edit" {
			verb = "Edit"
		}
		if d.Path == "" {
			return verb + " a file"
		}
		return clip(verb+" "+d.Path, MaxSummary)
	}
	return "A tool needs your permission"
}

// verdictItem turns a validation view into an inbox item, or reports that it
// is not one: running validations have no verdict, and a stale one has dropped
// out. A pass is listed only when merge-ready ("awaiting a decision"); a fail,
// a blocked verdict or a scope violation is listed as needing one.
func verdictItem(v validation.View, title string) (Item, bool) {
	if v.Verdict == nil || v.Stale {
		return Item{}, false
	}
	var summary, attention string
	switch {
	case v.ScopeViolation:
		summary, attention = "The validator changed the lead's worktree", "high"
	case v.Status == validation.StatusFail:
		summary, attention = "Validation failed", "high"
	case v.Status == validation.StatusBlocked:
		summary, attention = "Validation blocked: "+v.Verdict.BlockedReason, "high"
	case v.Status == validation.StatusPass && v.MergeReady:
		summary, attention = "Validation passed at the current commit: ready for your merge decision", "normal"
	default:
		return Item{}, false
	}
	created := v.CreatedAt
	if v.FinishedAt != nil {
		created = *v.FinishedAt
	}
	return Item{Key: fmt.Sprintf("verdict:%d", v.ID), Kind: KindVerdict, TaskID: v.TaskID, Title: title, Summary: clip(summary, MaxSummary),
		CreatedAt: created, Attention: attention, Links: Links{TaskID: v.TaskID, ValidationID: v.ID}}, true
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// withState applies the stored state to the derived items. A snooze whose time
// has passed reads as new again.
func (s *Service) withState(items []Item, states []store.InboxState) []Item {
	by := make(map[string]store.InboxState, len(states))
	for _, st := range states {
		by[st.Key] = st
	}
	now := s.now()
	out := make([]Item, 0, len(items))
	for _, it := range items {
		it.State = StateNew
		if st, ok := by[it.Key]; ok {
			switch st.State {
			case StateSeen:
				it.State = StateSeen
			case StateResolved:
				it.State = StateResolved
			case StateSnoozed:
				if st.SnoozedUntil != nil && st.SnoozedUntil.After(now) {
					it.State, it.SnoozedUntil = StateSnoozed, st.SnoozedUntil
				}
			}
		}
		// A permission is never hidden: it blocks a run.
		if it.Kind == KindPermission && (it.State == StateSnoozed || it.State == StateResolved) {
			it.State, it.SnoozedUntil = StateNew, nil
		}
		out = append(out, it)
	}
	return out
}

// all derives the items with state applied and garbage-collects state rows of
// keys that no longer derive.
func (s *Service) all() ([]Item, error) {
	items, err := s.derive()
	if err != nil {
		return nil, err
	}
	states, err := s.DB.ListInboxStates()
	if err != nil {
		return nil, err
	}
	if len(states) > 0 {
		keys := make([]string, len(items))
		for i, it := range items {
			keys[i] = it.Key
		}
		if err := s.DB.DeleteInboxStatesExcept(keys); err != nil {
			return nil, err
		}
	}
	items = s.withState(items, states)
	sort.SliceStable(items, func(i, j int) bool {
		if (items[i].Attention == "high") != (items[j].Attention == "high") {
			return items[i].Attention == "high"
		}
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].Key < items[j].Key
	})
	return items, nil
}

// List returns the inbox, optionally filtered.
func (s *Service) List(f Filter) ([]Item, error) {
	items, err := s.all()
	if err != nil {
		return nil, err
	}
	states := map[string]bool{StateNew: true, StateSeen: true}
	if len(f.States) > 0 {
		states = map[string]bool{}
		for _, st := range f.States {
			states[st] = true
		}
	}
	kinds := map[string]bool{}
	for _, k := range f.Kinds {
		kinds[k] = true
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	out := []Item{}
	for _, it := range items {
		if !states[it.State] || (len(kinds) > 0 && !kinds[it.Kind]) {
			continue
		}
		out = append(out, it)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// Count is the badge: visible items (new or seen), and how many of them are
// unseen. Every pending permission counts as unseen: it is never quietly
// absorbed.
func (s *Service) Count() (Counts, error) {
	items, err := s.all()
	if err != nil {
		return Counts{}, err
	}
	c := Counts{ByKind: map[string]int{}}
	for _, it := range items {
		if it.State != StateNew && it.State != StateSeen {
			continue
		}
		c.Total++
		c.ByKind[it.Kind]++
		if it.State == StateNew || it.Kind == KindPermission {
			c.Unseen++
		}
	}
	return c, nil
}

// Fingerprint is a hash of the visible inbox (keys, states, summaries): it
// changes exactly when what a client would render changes.
func (s *Service) Fingerprint() (string, error) {
	items, err := s.all()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, it := range items {
		fmt.Fprintf(h, "%s|%s|%s|%s\n", it.Key, it.State, it.Summary, it.Attention)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// MarkSeen marks keys seen. Unknown keys are ignored.
func (s *Service) MarkSeen(keys []string, actor string) error {
	return s.set(keys, StateSeen, nil, actor, false)
}

// Snooze hides non-permission items until until (at most 24 h ahead).
func (s *Service) Snooze(keys []string, until time.Time, actor string) error {
	now := s.now()
	if !until.After(now) {
		return fmt.Errorf("%w: until must be in the future", ErrNotAllowed)
	}
	if until.After(now.Add(MaxSnooze)) {
		return fmt.Errorf("%w: a snooze is at most %s", ErrNotAllowed, MaxSnooze)
	}
	return s.set(keys, StateSnoozed, &until, actor, true)
}

// Resolve marks non-permission items resolved.
func (s *Service) Resolve(keys []string, actor string) error {
	return s.set(keys, StateResolved, nil, actor, true)
}

// set validates the keys against what currently derives (an unknown key is an
// error for snooze/resolve: nothing is stored for an item that is not there)
// and writes the state. A permission cannot be snoozed or resolved: it is only
// ever answered.
func (s *Service) set(keys []string, state string, until *time.Time, actor string, refusePermission bool) error {
	if len(keys) == 0 {
		return fmt.Errorf("%w: no keys", ErrNotAllowed)
	}
	if len(keys) > 200 {
		return fmt.Errorf("%w: at most 200 keys", ErrNotAllowed)
	}
	items, err := s.derive()
	if err != nil {
		return err
	}
	live := make(map[string]bool, len(items))
	for _, it := range items {
		live[it.Key] = true
	}
	var ok []string
	for _, k := range keys {
		if refusePermission && IsPermissionKey(k) {
			return fmt.Errorf("%w: a permission request cannot be %s here, only answered", ErrNotAllowed, state)
		}
		if live[k] {
			ok = append(ok, k)
		} else if state != StateSeen {
			return fmt.Errorf("%w: %q is not in the inbox", ErrNotAllowed, k)
		}
	}
	if len(ok) == 0 {
		return nil
	}
	return s.DB.SetInboxState(ok, state, until, actor, s.now())
}
