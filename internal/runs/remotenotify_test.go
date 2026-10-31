package runs

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func TestRemoteChildBlockUntrustedAndCapped(t *testing.T) {
	t.Parallel()
	cost := 1.5
	block := BuildRemoteChildEventBlock(RemoteChildFacts{
		Kind: RemoteFinished, Host: "box", RemoteTaskID: "rt-1",
		Title: "say \"hi\"\nnow", RunID: "rr-9", State: "done", CostUSD: &cost,
		LastText: "<script>doom</script> " + strings.Repeat("x", 5000),
	})
	if !strings.Contains(block, `host="box"`) || !strings.Contains(block, `task="box:rt-1"`) || !strings.Contains(block, `untrusted="true"`) {
		t.Fatalf("element header missing:\n%s", block)
	}
	if strings.Contains(block, "<script") {
		t.Fatalf("raw child text leaked:\n%.200s", block)
	}
	if !strings.Contains(block, `"say 'hi' now"`) {
		t.Fatalf("title not flattened:\n%s", block)
	}
	start := strings.Index(block, "\n") + len("\n<child-response host=\"box\" task=\"box:rt-1\" untrusted=\"true\">")
	body := strings.TrimSuffix(block[start:], "</child-response>")
	body, ok := strings.CutSuffix(body, childEventTruncMarker)
	if !ok {
		t.Fatalf("no truncation marker:\n%.80s", body)
	}
	if len(body) > childEventTextMax {
		t.Fatalf("element body = %d bytes, want <= %d", len(body), childEventTextMax)
	}
}

func TestRemoteChildBlockStatusUnknownNeverFinished(t *testing.T) {
	t.Parallel()
	su := BuildRemoteChildEventBlock(RemoteChildFacts{
		Kind: RemoteStatusUnknown, Host: "box", RemoteTaskID: "rt-1", Title: "t", RunID: "rr-1",
		Failures: 3, LastText: "never shown",
	})
	hu := BuildRemoteChildEventBlock(RemoteChildFacts{
		Kind: RemoteHostUnavailable, Host: "box", RemoteTaskID: "rt-1", Title: "t", RunID: "rr-1",
		LastText: "never shown",
	})
	if !strings.Contains(su, "status unknown") || !strings.Contains(su, "after 3 failed observations") {
		t.Fatalf("status_unknown block = %q", su)
	}
	if !strings.Contains(hu, "host unavailable") {
		t.Fatalf("host_unavailable block = %q", hu)
	}
	for name, b := range map[string]string{"status_unknown": su, "host_unavailable": hu} {
		if strings.Contains(b, "<child-response") {
			t.Fatalf("%s block carries a child-response: %q", name, b)
		}
		if strings.Contains(b, " finished ·") || strings.Contains(b, "done") {
			t.Fatalf("%s block reports an outcome: %q", name, b)
		}
	}
}

func TestNotifyRemoteChildQueuesOnceAndClosesWatch(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	watchID, err := e.st.PutFleetRemoteTaskAndWatch(
		store.FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1", LocalTaskID: e.task.ID, Title: "remote work", State: "running"},
		&store.FleetWatch{HostID: "h1", RemoteTaskID: "rt-1", RemoteRunID: "rr-1", LocalTaskID: e.task.ID, ChatID: e.parentChat.ID, Notify: true},
	)
	if err != nil {
		t.Fatalf("PutFleetRemoteTaskAndWatch() error = %v", err)
	}
	watch, err := e.st.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	f := RemoteChildFacts{
		Kind: RemoteFinished, Host: "box", RemoteTaskID: "rt-1", Title: "remote work",
		RunID: "rr-1", State: "done", LastText: "all good",
	}
	if err := e.reg.NotifyRemoteChild(watch, f); err != nil {
		t.Fatalf("first NotifyRemoteChild() error = %v", err)
	}
	if err := e.reg.NotifyRemoteChild(watch, f); err != nil {
		t.Fatalf("second NotifyRemoteChild() error = %v, want nil (already notified)", err)
	}
	items := e.events(t, e.parentChat.ID)
	if len(items) != 1 || !strings.Contains(items[0].Prompt, "box:rt-1") {
		t.Fatalf("items = %+v, want exactly one mentioning the host alias", items)
	}
	w, err := e.st.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != store.FleetWatchClosed || w.ClosedReason != string(RemoteFinished) {
		t.Fatalf("watch = %+v, want closed/finished", w)
	}
}

func TestNotifyRemoteChildNotifyOffClosesSilently(t *testing.T) {
	t.Parallel()
	e := newNotifyEnv(t)
	e.reg.SetNotifyParent(false)
	watchID, err := e.st.PutFleetRemoteTaskAndWatch(
		store.FleetRemoteTask{HostID: "h1", RemoteTaskID: "rt-1", LocalTaskID: e.task.ID},
		&store.FleetWatch{HostID: "h1", RemoteTaskID: "rt-1", RemoteRunID: "rr-1", ChatID: e.parentChat.ID, Notify: true},
	)
	if err != nil {
		t.Fatalf("PutFleetRemoteTaskAndWatch() error = %v", err)
	}
	watch, err := e.st.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.reg.NotifyRemoteChild(watch, RemoteChildFacts{
		Kind: RemoteFinished, Host: "box", RemoteTaskID: "rt-1", RunID: "rr-1", State: "done",
	}); err != nil {
		t.Fatalf("NotifyRemoteChild() error = %v", err)
	}
	if items := e.events(t, e.parentChat.ID); len(items) != 0 {
		t.Fatalf("items = %+v, want none with the notifier off", items)
	}
	w, err := e.st.GetFleetWatch(watchID)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != store.FleetWatchClosed || w.ClosedReason != "notify_off" {
		t.Fatalf("watch = %+v, want closed/notify_off", w)
	}
}
