package store

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestFleetAudit_AppendListFilterSince(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	day := func(n int) time.Time {
		return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * 24 * time.Hour)
	}
	add := func(host string, ts time.Time) int64 {
		t.Helper()
		id, err := s.AppendFleetAudit(FleetAuditRow{
			TS: ts, Direction: "out", HostID: host, Method: "task.run", Outcome: "ok",
			TaskID: "rt-1", RunID: "run-1", Code: "200", LatencyMs: 42, Bytes: 512,
			Summary: "ran a task",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a1 := add("alice", day(0))
	a2 := add("alice", day(1))
	b1 := add("bob", day(2))

	// All hosts, newest first.
	rows, err := s.ListFleetAudit("", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].ID != b1 || rows[1].ID != a2 || rows[2].ID != a1 {
		t.Fatalf("rows = %d [%d %d %d], want newest first", len(rows), b1, a2, a1)
	}
	got := rows[1]
	if got.Direction != "out" || got.HostID != "alice" || got.Method != "task.run" || got.Outcome != "ok" ||
		got.TaskID != "rt-1" || got.RunID != "run-1" || got.Code != "200" || got.LatencyMs != 42 || got.Bytes != 512 ||
		got.Summary != "ran a task" {
		t.Fatalf("row = %+v", got)
	}
	if !got.TS.Equal(day(1)) {
		t.Fatalf("TS = %v, want %v", got.TS, day(1))
	}

	// One host only.
	rows, err = s.ListFleetAudit("alice", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != a2 || rows[1].ID != a1 {
		t.Fatalf("alice rows = %+v, want [%d %d]", rows, a2, a1)
	}

	// Since filters the lower bound, across hosts.
	rows, err = s.ListFleetAudit("", day(1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != b1 || rows[1].ID != a2 {
		t.Fatalf("since rows = %+v, want [%d %d]", rows, b1, a2)
	}

	// Limit bounds the page.
	if rows, err = s.ListFleetAudit("", time.Time{}, 1); err != nil || len(rows) != 1 || rows[0].ID != b1 {
		t.Fatalf("limited rows = %+v, %v; want only %d", rows, err, b1)
	}

	// A zero TS is stamped now UTC, not left to the driver.
	before := time.Now().UTC()
	idNow, err := s.AppendFleetAudit(FleetAuditRow{Direction: "in", HostID: "alice", Method: "chat.send", Outcome: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListFleetAudit("alice", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].ID != idNow {
		t.Fatalf("newest = %d, want %d", rows[0].ID, idNow)
	}
	if rows[0].TS.Before(before) || rows[0].TS.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("TS = %v, want ~now", rows[0].TS)
	}
}

func TestFleetAudit_TruncatesSummary(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	if _, err := s.AppendFleetAudit(FleetAuditRow{
		Direction: "out", HostID: "alice", Method: "chat.send", Outcome: "ok",
		Trace:   strings.Repeat("t", 300),
		ReqID:   strings.Repeat("r", 300),
		Code:    strings.Repeat("c", 300),
		Summary: strings.Repeat("京", 600),
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListFleetAudit("alice", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if n := utf8.RuneCountInString(r.Summary); n != fleetAuditMaxSummary {
		t.Fatalf("summary = %d runes, want %d", n, fleetAuditMaxSummary)
	}
	if !strings.HasSuffix(r.Summary, "…") || !utf8.ValidString(r.Summary) {
		t.Fatalf("summary not a clean rune-boundary cut: %q", r.Summary[len(r.Summary)-8:])
	}
	for name, v := range map[string]string{"Trace": r.Trace, "ReqID": r.ReqID, "Code": r.Code} {
		if len(v) > fleetAuditMaxShort || !utf8.ValidString(v) {
			t.Fatalf("%s = %d bytes of valid UTF-8 %t, want <= %d", name, len(v), utf8.ValidString(v), fleetAuditMaxShort)
		}
	}

	// A short summary passes through unchanged, with no marker added.
	short := "all good"
	if _, err := s.AppendFleetAudit(FleetAuditRow{
		Direction: "in", HostID: "alice", Method: "chat.send", Outcome: "ok", Summary: short,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListFleetAudit("alice", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Summary != short {
		t.Fatalf("summary = %q, want %q untouched", rows[0].Summary, short)
	}
}

func TestFleetAudit_RejectsBadDirection(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	for _, d := range []string{"", "outbound", "OUT", "inbound"} {
		if _, err := s.AppendFleetAudit(FleetAuditRow{Direction: d, Method: "task.run", Outcome: "ok"}); err == nil {
			t.Fatalf("direction %q accepted, want error", d)
		}
	}
	for _, d := range []string{"out", "in"} {
		if _, err := s.AppendFleetAudit(FleetAuditRow{Direction: d, Method: "task.run", Outcome: "ok"}); err != nil {
			t.Fatalf("direction %q rejected: %v", d, err)
		}
	}
	for _, r := range []FleetAuditRow{
		{Direction: "out", Outcome: "ok"},
		{Direction: "out", Method: "task.run"},
	} {
		if _, err := s.AppendFleetAudit(r); err == nil {
			t.Fatalf("row %+v accepted, want error for the missing field", r)
		}
	}
	if rows, err := s.ListFleetAudit("", time.Time{}, 0); err != nil || len(rows) != 2 {
		t.Fatalf("rows = %d, %v; want the 2 valid appends", len(rows), err)
	}
}

func TestFleetAudit_SweepOnlyOld(t *testing.T) {
	t.Parallel()

	s := openFleetStore(t)
	now := time.Now().UTC()
	old := FleetAuditRow{Direction: "out", HostID: "alice", Method: "task.run", Outcome: "ok", TS: now.Add(-40 * 24 * time.Hour)}
	if _, err := s.AppendFleetAudit(old); err != nil {
		t.Fatal(err)
	}
	fresh := FleetAuditRow{Direction: "out", HostID: "alice", Method: "task.run", Outcome: "ok", TS: now.Add(-24 * time.Hour)}
	if _, err := s.AppendFleetAudit(fresh); err != nil {
		t.Fatal(err)
	}

	n, err := s.SweepFleetAudit(now.Add(-FleetAuditRetention))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sweep deleted %d rows, want exactly the 40-day-old one", n)
	}
	rows, err := s.ListFleetAudit("", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].TS.Equal(fresh.TS) {
		t.Fatalf("rows after sweep = %+v, want only the 1-day-old row", rows)
	}
	if n, err := s.SweepFleetAudit(now.Add(-FleetAuditRetention)); err != nil || n != 0 {
		t.Fatalf("second sweep deleted %d, %v; want 0", n, err)
	}
}
