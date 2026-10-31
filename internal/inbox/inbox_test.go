package inbox

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/validation"
)

type fx struct {
	svc    *Service
	perms  []runs.PendingPermission
	views  []validation.View
	stall  map[int64]time.Time
	pairs  []HostPair
	issues []HostLinkIssue
	now    time.Time
}

func newFx(t *testing.T) *fx {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f := &fx{now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), stall: map[int64]time.Time{}}
	f.svc = &Service{DB: db,
		Permissions: func() []runs.PendingPermission { return f.perms },
		Validations: func() ([]validation.View, error) { return f.views, nil },
		Stalls:      func() map[int64]time.Time { return f.stall },
		HostPairs:   func() []HostPair { return f.pairs },
		HostLinkIssues: func() []HostLinkIssue {
			return f.issues
		},
		TaskTitle: func(id int64) string { return "task " + string(rune('A'+id)) },
		Now:       func() time.Time { return f.now }}
	return f
}

func verdict(id, task int64, status string, ready, stale bool) validation.View {
	fin := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
	return validation.View{ID: id, TaskID: task, Status: status, MergeReady: ready, Stale: stale, CreatedAt: fin, FinishedAt: &fin,
		Verdict: &validation.Verdict{Status: status, BlockedReason: "no_browser"}}
}

func perm(req string, d *taskrunner.PermissionDetail) runs.PendingPermission {
	return runs.PendingPermission{RunID: "r1", TaskID: 1, ChatID: 1, RequestID: req, Summary: "Run `curl http://secret`", Detail: d}
}

func keysOf(items []Item) string {
	var k []string
	for _, it := range items {
		k = append(k, it.Key)
	}
	return strings.Join(k, ",")
}

func TestInboxList_DerivesPermissionVerdictStall(t *testing.T) {
	f := newFx(t)
	f.perms = []runs.PendingPermission{perm("q1", &taskrunner.PermissionDetail{Kind: "write", Path: "a.go"})}
	f.views = []validation.View{verdict(7, 2, "pass", true, false), verdict(8, 3, "fail", false, false)}
	f.stall[4] = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	items, err := f.svc.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items = %s", keysOf(items))
	}
	if items[0].Kind != KindPermission || items[0].Attention != "high" {
		t.Fatalf("a permission must lead: %+v", items[0])
	}
	got := map[string]Item{}
	for _, it := range items {
		got[it.Key] = it
		if it.State != StateNew {
			t.Errorf("%s state = %s", it.Key, it.State)
		}
	}
	if got["verdict:7"].Attention != "normal" || got["verdict:8"].Attention != "high" || got["stall:4:"+itoa(f.stall[4].Unix())].Kind != KindStall {
		t.Fatalf("items = %+v", got)
	}
	if got["verdict:7"].Links.ValidationID != 7 || got["perm:q1"].Links.PermissionRequestID != "q1" {
		t.Fatalf("links = %+v", got)
	}
	if byKind, _ := f.svc.List(Filter{Kinds: []string{KindStall}}); len(byKind) != 1 {
		t.Fatalf("kind filter = %v", keysOf(byKind))
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestInboxList_StaleVerdictDropsOut(t *testing.T) {
	f := newFx(t)
	f.views = []validation.View{
		verdict(1, 1, "pass", false, true),  // stale pass
		verdict(2, 2, "fail", false, true),  // stale fail
		verdict(3, 3, "pass", false, false), // a pass that is not merge-ready and not stale (e.g. superseded)
		{ID: 4, TaskID: 4, Status: "running"},
	}
	if items, _ := f.svc.List(Filter{}); len(items) != 0 {
		t.Fatalf("stale/not-ready/running validations must not be inbox items: %s", keysOf(items))
	}
	sv := verdict(5, 5, "pass", false, false)
	sv.ScopeViolation = true
	f.views = []validation.View{sv}
	items, _ := f.svc.List(Filter{})
	if len(items) != 1 || !strings.Contains(items[0].Summary, "worktree") || items[0].Attention != "high" {
		t.Fatalf("a scope violation needs a decision: %+v", items)
	}
}

func TestInboxList_RedactsPermissionSummary(t *testing.T) {
	f := newFx(t)
	f.perms = []runs.PendingPermission{
		perm("a", &taskrunner.PermissionDetail{Kind: "execute", Command: "curl -H 'Authorization: Bearer SECRET' https://x"}),
		perm("b", &taskrunner.PermissionDetail{Kind: "write", Path: strings.Repeat("d/", 100) + "f.go"}),
		perm("c", nil),
	}
	items, _ := f.svc.List(Filter{})
	for _, it := range items {
		if strings.Contains(it.Summary, "SECRET") || strings.Contains(it.Summary, "curl") {
			t.Fatalf("command text leaked into the summary: %q", it.Summary)
		}
		if len([]rune(it.Summary)) > MaxSummary {
			t.Fatalf("summary too long: %d", len([]rune(it.Summary)))
		}
	}
	if s := PermissionSummary(&taskrunner.PermissionDetail{Kind: "execute"}); s != "Run a command" {
		t.Fatalf("summary = %q", s)
	}
}

func TestInboxState_GCWhenKeyGone(t *testing.T) {
	f := newFx(t)
	f.views = []validation.View{verdict(1, 1, "fail", false, false)}
	if err := f.svc.Resolve([]string{"verdict:1"}, "local"); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.svc.DB.ListInboxStates(); len(st) != 1 {
		t.Fatalf("states = %v", st)
	}
	f.views = nil // the item no longer derives
	if _, err := f.svc.List(Filter{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.svc.DB.ListInboxStates(); len(st) != 0 {
		t.Fatalf("a state row must be garbage-collected once its key is gone: %v", st)
	}
}

func TestInboxResolve_HidesUntilDerivedKeyChanges(t *testing.T) {
	f := newFx(t)
	f.stall[4] = time.Unix(1000, 0)
	key := "stall:4:1000"
	if err := f.svc.Resolve([]string{key}, "local"); err != nil {
		t.Fatal(err)
	}
	if items, _ := f.svc.List(Filter{}); len(items) != 0 {
		t.Fatalf("resolved items are hidden: %s", keysOf(items))
	}
	if items, _ := f.svc.List(Filter{States: []string{StateResolved}}); len(items) != 1 {
		t.Fatalf("resolved filter = %s", keysOf(items))
	}
	f.stall[4] = time.Unix(2000, 0) // a new stall episode has a new key
	if items, _ := f.svc.List(Filter{}); len(items) != 1 || items[0].Key != "stall:4:2000" {
		t.Fatalf("a new episode must show up: %s", keysOf(items))
	}
}

func TestInboxSnooze_Max24h(t *testing.T) {
	f := newFx(t)
	f.stall[4] = time.Unix(1000, 0)
	key := "stall:4:1000"
	if err := f.svc.Snooze([]string{key}, f.now.Add(25*time.Hour), "local"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("25h snooze err = %v", err)
	}
	if err := f.svc.Snooze([]string{key}, f.now.Add(-time.Minute), "local"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("past snooze err = %v", err)
	}
	if err := f.svc.Snooze([]string{key}, f.now.Add(time.Hour), "local"); err != nil {
		t.Fatal(err)
	}
	if items, _ := f.svc.List(Filter{}); len(items) != 0 {
		t.Fatalf("a snoozed item is hidden: %s", keysOf(items))
	}
	if c, _ := f.svc.Count(); c.Total != 0 || c.Unseen != 0 {
		t.Fatalf("a snoozed item is not counted: %+v", c)
	}
	f.now = f.now.Add(2 * time.Hour) // the snooze expired
	items, _ := f.svc.List(Filter{})
	if len(items) != 1 || items[0].State != StateNew {
		t.Fatalf("an expired snooze reads as new again: %+v", items)
	}
}

func TestInboxSnooze_RefusedForPermission(t *testing.T) {
	f := newFx(t)
	f.perms = []runs.PendingPermission{perm("q1", nil)}
	if err := f.svc.Snooze([]string{"perm:q1"}, f.now.Add(time.Hour), "local"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("snoozing a permission must be refused: %v", err)
	}
	if err := f.svc.Resolve([]string{"perm:q1"}, "local"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("resolving a permission must be refused: %v", err)
	}
	if st, _ := f.svc.DB.ListInboxStates(); len(st) != 0 {
		t.Fatalf("a refused action stored something: %v", st)
	}
}

func TestInboxCount_PermissionAlwaysCounted(t *testing.T) {
	f := newFx(t)
	f.perms = []runs.PendingPermission{perm("q1", nil)}
	f.views = []validation.View{verdict(1, 1, "fail", false, false)}
	if err := f.svc.MarkSeen([]string{"perm:q1", "verdict:1"}, "local"); err != nil {
		t.Fatal(err)
	}
	c, err := f.svc.Count()
	if err != nil {
		t.Fatal(err)
	}
	if c.Total != 2 || c.Unseen != 1 || c.ByKind[KindPermission] != 1 || c.ByKind[KindVerdict] != 1 {
		t.Fatalf("a seen permission still counts as unseen, a seen verdict does not: %+v", c)
	}
}

func TestInboxState_UnknownKeyNotStored(t *testing.T) {
	f := newFx(t)
	if err := f.svc.Resolve([]string{"verdict:99"}, "local"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if err := f.svc.MarkSeen([]string{"verdict:99"}, "local"); err != nil {
		t.Fatalf("seen of an unknown key is a no-op, got %v", err)
	}
	if st, _ := f.svc.DB.ListInboxStates(); len(st) != 0 {
		t.Fatalf("stored states = %v", st)
	}
}

func TestInboxHostPairDerived(t *testing.T) {
	f := newFx(t)
	f.pairs = []HostPair{{HostID: "H1", Alias: "mac", Label: "Mac", Since: f.now.Add(-time.Minute)}}
	items, err := f.svc.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != KindHostPair || items[0].Key != "host_pair:H1" || items[0].Attention != "high" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Title != "mac" || items[0].Links.HostID != "H1" || items[0].TaskID != 0 {
		t.Fatalf("item = %+v", items[0])
	}
	if err := f.svc.Resolve([]string{"host_pair:H1"}, "t"); err != nil {
		t.Fatal(err)
	}
	if items, _ := f.svc.List(Filter{}); len(items) != 0 {
		t.Fatalf("a resolved pair must leave the list: %s", keysOf(items))
	}
}

func TestInboxHostDownOnlyAfterFiveMinutes(t *testing.T) {
	f := newFx(t)
	f.issues = []HostLinkIssue{{HostID: "H1", Alias: "mac", Kind: "down", Since: f.now.Add(-4*time.Minute - 59*time.Second)}}
	if items, _ := f.svc.List(Filter{}); len(items) != 0 {
		t.Fatalf("a link down under five minutes is not an inbox item: %s", keysOf(items))
	}
	f.issues[0].Since = f.now.Add(-5*time.Minute - time.Second)
	items, _ := f.svc.List(Filter{})
	if len(items) != 1 || items[0].Kind != KindHostDown || items[0].Attention != "normal" || items[0].Key != "host_down:H1:"+itoa(f.issues[0].Since.Unix()) {
		t.Fatalf("items = %+v", items)
	}
	before, _ := f.svc.Fingerprint()
	f.now = f.now.Add(time.Minute) // the outage ages; nothing a client renders may change
	after, _ := f.svc.Fingerprint()
	items, _ = f.svc.List(Filter{})
	if len(items) != 1 || items[0].Summary != "The link to this host is down" {
		t.Fatalf("items = %+v", items)
	}
	if before != after {
		t.Fatalf("fingerprint moved with the clock: %s != %s", before, after)
	}
}

func TestInboxHostRefusedHigh(t *testing.T) {
	f := newFx(t)
	f.issues = []HostLinkIssue{{HostID: "H1", Alias: "mac", Kind: "refused", Code: "fleet_version_unsupported",
		Guidance: "update smind on mac", Since: f.now.Add(-time.Minute)}}
	items, err := f.svc.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != KindHostRefused || items[0].Attention != "high" || items[0].Key != "host_refused:H1:fleet_version_unsupported" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Summary != "update smind on mac" {
		t.Fatalf("summary = %q", items[0].Summary)
	}
	f.issues[0].Guidance = ""
	items, _ = f.svc.List(Filter{})
	if len(items) != 1 || items[0].Summary != "The link was refused: fleet_version_unsupported" {
		t.Fatalf("summary without guidance = %+v", items)
	}
}

func TestInboxHostKindsAbsentWithoutSource(t *testing.T) {
	f := newFx(t)
	f.svc.HostPairs = nil
	f.svc.HostLinkIssues = nil
	items, err := f.svc.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %s", keysOf(items))
	}
	if c, _ := f.svc.Count(); c.Total != 0 {
		t.Fatalf("count = %+v", c)
	}
}

func TestInboxFingerprint_ChangesWithVisibleState(t *testing.T) {
	f := newFx(t)
	a, _ := f.svc.Fingerprint()
	f.stall[4] = time.Unix(1000, 0)
	b, _ := f.svc.Fingerprint()
	if a == b {
		t.Fatalf("a new item must change the fingerprint")
	}
	if err := f.svc.MarkSeen([]string{"stall:4:1000"}, "local"); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.svc.Fingerprint(); c == b {
		t.Fatalf("a state change must change the fingerprint")
	}
	if d, _ := f.svc.Fingerprint(); d == "" {
		t.Fatal("empty")
	}
}
