package runstats

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func ev(t *testing.T, typ taskrunner.EventType, kv map[string]any) string {
	t.Helper()
	m := map[string]any{"type": int(typ)}
	for k, v := range kv {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// str is a JSON string whose encoded size is exactly n bytes (quotes included).
func str(n int) string { return strings.Repeat("x", n-2) }

type fixedEvent struct {
	data string
	at   time.Duration // offset from the run start
}

// snapshot builds the fixture database with the real schema, copies it with
// VACUUM INTO (as the real measurement does) and returns the copy's path.
func snapshot(t *testing.T) string {
	t.Helper()
	t.Setenv("SMIND_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "smind.db")
	st, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/synthetic/repo", Title: "synthetic", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mk := func(id, provider string, started time.Time, evs []fixedEvent) {
		fin := started.Add(time.Minute)
		if _, err := st.CreateRun(store.Run{ID: id, TaskID: tk.ID, Provider: provider, Prompt: "SECRET-PROMPT", Status: "done", StartedAt: started, FinishedAt: &fin}); err != nil {
			t.Fatal(err)
		}
		for i, e := range evs {
			if _, err := st.AppendRunEvent(id, int64(i), e.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	text := func(at time.Duration) fixedEvent {
		return fixedEvent{ev(t, taskrunner.EventTypeText, map[string]any{"text": "SECRET-TEXT-aaaaaaaaaaaaaaaaaaaaaaa"}), at}
	}
	think := func(at time.Duration) fixedEvent {
		return fixedEvent{ev(t, taskrunner.EventTypeThinking, map[string]any{"text": "SECRET-THINK-bbbbbbbbbbbbbbbbbbbbb"}), at}
	}
	tool := func(id, name string, in, res int) fixedEvent {
		m := map[string]any{"toolCallId": id, "toolName": name, "toolStatus": "running"}
		if in > 0 {
			m["toolInput"] = str(in)
		}
		if res > 0 {
			m["toolResult"] = str(res)
			m["toolStatus"] = "success"
			if id != "c3" { // a result update usually omits the tool name
				delete(m, "toolName")
			}
		}
		return fixedEvent{ev(t, taskrunner.EventTypeToolCall, m), 0}
	}
	var evs []fixedEvent
	evs = append(evs, fixedEvent{ev(t, taskrunner.EventTypeUserMessage, map[string]any{"text": "SECRET-PROMPT"}), 0})
	for i := 0; i < 10; i++ { // burst one
		evs = append(evs, text(time.Duration(i)*10*time.Millisecond))
	}
	for i := 0; i < 5; i++ { // burst two, 2 s later: idle split
		evs = append(evs, text(2*time.Second+time.Duration(i)*10*time.Millisecond))
	}
	for i := 0; i < 3; i++ {
		evs = append(evs, think(2*time.Second+100*time.Millisecond+time.Duration(i)*10*time.Millisecond))
	}
	evs = append(evs,
		tool("c1", "Bash", 100, 0), tool("c1", "Bash", 0, 600),
		tool("c2", "Bash", 100, 0), tool("c2", "Bash", 0, 10000),
		tool("c3", "rm -rf SECRET-TOOL", 0, 512),
		text(5*time.Second),
		fixedEvent{ev(t, taskrunner.EventTypeDone, map[string]any{"stopReason": "end_turn"}), 5 * time.Second},
	)
	start := now.Add(-time.Hour)
	mk("SECRET-RUN-1", "glm", start, evs)
	mk("SECRET-RUN-2", "claude-native", now.Add(-40*24*time.Hour), []fixedEvent{text(0), {ev(t, taskrunner.EventTypeDone, nil), 0}})
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	snap := filepath.Join(t.TempDir(), "smind-copy.db")
	srcDB, err := sql.Open("sqlite", "file:"+src+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srcDB.Exec(`VACUUM INTO ?`, snap); err != nil {
		t.Fatal(err)
	}
	srcDB.Close()
	cp, err := sql.Open("sqlite", snap)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range evs {
		if _, err := cp.Exec(`UPDATE run_events SET created_at = ? WHERE run_id = 'SECRET-RUN-1' AND seq = ?`, start.Add(e.at), i); err != nil {
			t.Fatal(err)
		}
	}
	cp.Close()
	for _, suffix := range []string{"-wal", "-shm"} {
		os.Remove(snap + suffix)
	}
	return snap
}

func compute(t *testing.T, path string) *Report {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rep, err := Compute(context.Background(), db, path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestComputeCountsAndThresholds(t *testing.T) {
	rep := compute(t, snapshot(t))
	if rep.Runs != 2 || rep.Rows != 28 {
		t.Fatalf("runs=%d rows=%d, want 2 and 28", rep.Runs, rep.Rows)
	}
	if rep.ToolCalls != 3 {
		t.Errorf("tool calls = %d, want 3", rep.ToolCalls)
	}
	// payloads: 100, 600, 100, 10000, 512
	if rep.Payload.N != 5 || rep.Payload.Sum != 11312 || rep.Payload.Max != 10000 {
		t.Errorf("payload dist = %+v", rep.Payload)
	}
	var th Threshold
	for _, x := range rep.Thresholds {
		if x.InlineCap == 512 && x.WholeCap == 8192 {
			th = x
		}
	}
	// 100,100,512 inline (<= 512); 600 whole in the excerpt tier; 10000 -> 8192.
	if th.InlineBytes != 712 || th.ExcerptTierBytes != 600+8192 || th.DescriptorBytes != 2*DescriptorBytes || th.PayloadsWhole != 80 {
		t.Errorf("threshold 512/8192 = %+v", th)
	}
	if rep.ByAge["30d+"].Rows != 2 || rep.ByAge["0-7d"].Rows != 26 {
		t.Errorf("age buckets = %+v", rep.ByAge)
	}
	var other bool
	for _, tl := range rep.ByTool {
		other = other || tl.Tool == "other"
	}
	if !other {
		t.Errorf("a free-form tool name must be reported as \"other\": %+v", rep.ByTool)
	}
	for _, tl := range rep.ByTool {
		if tl.Tool == "Bash" && (tl.Calls != 2 || tl.Input.N != 2 || tl.Result.N != 2 || tl.Result.Max != 10000) {
			t.Errorf("a result without a tool name must count for its call's tool: %+v", tl)
		}
	}
	if rep.DB.RowOverheadBytes <= 0 {
		t.Errorf("overhead = %d", rep.DB.RowOverheadBytes)
	}
}

func TestCoalescingModel(t *testing.T) {
	c := compute(t, snapshot(t)).Coalescing
	// run 1: 16 text + 3 thinking chunks, run 2: 1 text chunk.
	if c.ChunkRowsNow != 20 {
		t.Errorf("chunk rows now = %d, want 20", c.ChunkRowsNow)
	}
	// boundary + 4 KiB: text(15), thinking(3), text(1), run 2 text(1).
	if c.RowsBoundaryAnd4K != 4 {
		t.Errorf("boundary rows = %d, want 4", c.RowsBoundaryAnd4K)
	}
	// the 2 s gap splits the 15 text chunks in two: one more row.
	if c.RowsWithIdle != 5 {
		t.Errorf("idle rows = %d, want 5", c.RowsWithIdle)
	}
	if c.BytesWithIdle >= c.ChunkBytesNow {
		t.Errorf("coalescing must shrink the bytes: %d >= %d", c.BytesWithIdle, c.ChunkBytesNow)
	}
}

func TestOutputsCarryNoText(t *testing.T) {
	rep := compute(t, snapshot(t))
	b, _ := json.Marshal(rep)
	var tbl bytes.Buffer
	Render(&tbl, rep)
	for _, out := range []string{string(b), tbl.String()} {
		if strings.Contains(out, "SECRET") {
			t.Fatalf("output leaks text, an id or a free-form name:\n%s", out)
		}
	}
	if !strings.Contains(tbl.String(), "glm") {
		t.Errorf("table lacks the provider:\n%s", tbl.String())
	}
}

func TestOpenRefusesWhatCouldBeLive(t *testing.T) {
	snap := snapshot(t)
	home := os.Getenv("SMIND_HOME")
	live := filepath.Join(home, "smind.db")
	data, _ := os.ReadFile(snap)
	_ = os.WriteFile(live, data, 0o600)
	inHome := filepath.Join(home, "copy.db")
	_ = os.WriteFile(inHome, data, 0o600)
	link := filepath.Join(t.TempDir(), "link.db")
	if err := os.Symlink(live, link); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(t.TempDir(), "hard.db")
	if err := os.Link(live, hard); err != nil {
		t.Fatal(err)
	}
	walDB := filepath.Join(t.TempDir(), "w.db")
	_ = os.WriteFile(walDB, data, 0o600)
	_ = os.WriteFile(walDB+"-wal", []byte("pending"), 0o600)
	for name, p := range map[string]string{
		"empty": "", "query string": snap + "?mode=rwc", "file uri": "file:" + snap, "memory": ":memory:",
		"directory": filepath.Dir(snap), "missing": snap + ".nope",
		"live db": live, "file in home": inHome, "symlink to live": link, "hard link to live": hard, "pending wal": walDB,
	} {
		if db, err := Open(p); err == nil {
			db.Close()
			t.Errorf("%s: Open(%q) succeeded, want a refusal", name, p)
		}
	}
	db, err := Open(snap)
	if err != nil {
		t.Fatalf("a plain snapshot must open: %v", err)
	}
	db.Close()
}

func TestMeasurementNeverWrites(t *testing.T) {
	snap := snapshot(t)
	before := sha256.Sum256(mustRead(t, snap))
	db, err := Open(snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM run_events`); err == nil {
		t.Fatal("the connection accepted a write")
	}
	db.Close()
	compute(t, snap)
	if after := sha256.Sum256(mustRead(t, snap)); after != before {
		t.Fatal("the snapshot changed")
	}
	for _, s := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(snap + s); err == nil {
			t.Errorf("measurement left %s next to the snapshot", s)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
