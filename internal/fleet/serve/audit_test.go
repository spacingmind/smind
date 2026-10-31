package serve

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

func TestFleetAuditBothEndsNoPromptBody(t *testing.T) {
	const canary = "sk-CANARY-abcdefghijklmnopqrstuvwxyz0123456789"
	body := canary + " " + strings.Repeat("lorem ipsum ", 170)
	clk := newClock()
	s, au, _ := newTestServer("A", clk)

	raw := rawReq(t, clk, "B", "req-1", map[string]any{"prompt": body})
	if _, err := s.Handle(context.Background(), "B", wire.MethodTaskGet, raw); err != nil {
		t.Fatal(err)
	}
	if len(au.rows) != 1 {
		t.Fatalf("rows %d, want 1", len(au.rows))
	}
	r := au.rows[0]
	if r.Direction != "in" || r.HostID != "B" || r.Method != wire.MethodTaskGet || r.Trace != "tr" || r.ReqID != "req-1" ||
		r.Outcome != "ok" || r.TaskID != "7" || r.Bytes <= 0 {
		t.Fatalf("row %+v", r)
	}
	if !strings.HasPrefix(r.Summary, "sha256=") {
		t.Fatalf("summary %q", r.Summary)
	}
	if n := utf8.RuneCountInString(r.Summary); n > 7+64+1+120 {
		t.Fatalf("summary is %d runes", n)
	}
	if strings.Contains(r.Summary, canary) || strings.Contains(r.Summary, body) {
		t.Fatalf("summary leaks the body: %q", r.Summary)
	}

	// A refused call is audited too.
	p := wire.Prov{Origin: "B", Trace: "tr", Hops: []string{"B", "A"}, ReqID: "req-2", TS: clk.now().Unix()}
	b, _ := json.Marshal(map[string]any{"prov": p, "id": 1})
	if _, err := s.Handle(context.Background(), "B", wire.MethodTaskGet, b); err == nil {
		t.Fatal("want refusal")
	}
	if len(au.rows) != 2 || au.rows[1].Outcome != "refused" || au.rows[1].Code != wire.CodeCycle {
		t.Fatalf("rows %+v", au.rows)
	}
}

func TestFleetSummaryDropsProvAndCutsOnRuneBoundary(t *testing.T) {
	id := func(s string) string { return s }
	a := Summary(json.RawMessage(`{"prov":{"reqId":"1"},"id":3}`), id)
	b := Summary(json.RawMessage(`{"id":3,"prov":{"reqId":"2"}}`), id)
	if a != b || strings.Contains(a, "prov") {
		t.Fatalf("%q vs %q", a, b)
	}
	long := Summary(json.RawMessage(`{"t":"`+strings.Repeat("é", 300)+`"}`), id)
	if !utf8.ValidString(long) || utf8.RuneCountInString(long) != 7+64+1+120 {
		t.Fatalf("runes %d", utf8.RuneCountInString(long))
	}
	if got := Summary(json.RawMessage(`not json`), id); !strings.HasSuffix(got, " not json") {
		t.Fatalf("got %q", got)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func appendRow(t *testing.T, s *store.Store, ts time.Time) {
	t.Helper()
	if _, err := s.AppendFleetAudit(store.FleetAuditRow{TS: ts, Direction: "in", HostID: "B", Method: "fleet.info", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
}

func TestFleetAuditRetention(t *testing.T) {
	s := openStore(t)
	now := time.Now().UTC()
	appendRow(t, s, now.Add(-31*24*time.Hour))
	appendRow(t, s, now)
	n, err := SweepAudit(s, now, store.FleetAuditRetention)
	if err != nil || n != 1 {
		t.Fatalf("deleted %d, err %v", n, err)
	}
	rows, err := s.ListFleetAudit("", time.Time{}, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %d, err %v", len(rows), err)
	}
}

func TestFleetAuditSweeperRuns(t *testing.T) {
	s := openStore(t)
	stop := StartAuditSweeper(s, store.FleetAuditRetention, 10*time.Millisecond)
	appendRow(t, s, time.Now().UTC().Add(-40*24*time.Hour))
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, err := s.ListFleetAudit("", time.Time{}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old row was not swept")
		}
		time.Sleep(5 * time.Millisecond)
	}
	done := make(chan struct{})
	go func() { stop(); stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not return")
	}
}
