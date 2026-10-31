package wsapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func rpcResult(t *testing.T, h handlerFunc, rc *requestContext, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return h(context.Background(), rc, raw)
}

func TestFileStatAndReadRange_OverTheWire(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	data := make([]byte, 600<<10)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	copy(data, "%PDF-1.7\n")
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "doc.pdf"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	sendRequest(t, ws, "stat", "file.stat", map[string]any{"taskId": task.ID, "path": "doc.pdf"})
	resp := readEnvelopeFor(t, ws, "stat", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("file.stat: %v", resp.Error.Message)
	}
	var st fileStatResult
	if err := json.Unmarshal(resp.Result, &st); err != nil {
		t.Fatal(err)
	}
	if st.Kind != "pdf" || st.Mime != "application/pdf" || st.Ext != ".pdf" || st.Mismatch || !st.Previewable || st.Size != int64(len(data)) {
		t.Fatalf("file.stat = %+v", st)
	}

	var got []byte
	var mtime DateTime = st.Mtime
	for i := 0; ; i++ {
		sendRequest(t, ws, "rr", "file.readRange", map[string]any{
			"taskId": task.ID, "path": "doc.pdf", "offset": len(got), "length": 1 << 30, "ifMtime": mtime,
		})
		resp = readEnvelopeFor(t, ws, "rr", 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("file.readRange: %v", resp.Error.Message)
		}
		var rr fileReadRangeResult
		if err := json.Unmarshal(resp.Result, &rr); err != nil {
			t.Fatal(err)
		}
		if rr.Length != len(rr.Data) || rr.Offset != int64(len(got)) {
			t.Fatalf("chunk %d: offset=%d length=%d data=%d", i, rr.Offset, rr.Length, len(rr.Data))
		}
		got = append(got, rr.Data...)
		if rr.EOF {
			break
		}
		if i > 10 {
			t.Fatal("too many chunks")
		}
	}
	if !bytes.Equal(got, data) {
		t.Fatal("reassembled bytes differ over the wire")
	}

	// Typed errors keep their codes through the wire.
	sendRequest(t, ws, "neg", "file.readRange", map[string]any{"taskId": task.ID, "path": "doc.pdf", "offset": -1, "length": 5})
	if resp = readEnvelopeFor(t, ws, "neg", 5*time.Second); resp.Error == nil || resp.Error.Code != "invalid_params" {
		t.Errorf("negative offset: error = %+v, want invalid_params", resp.Error)
	}
	sendRequest(t, ws, "miss", "file.stat", map[string]any{"taskId": task.ID, "path": "gone.pdf"})
	if resp = readEnvelopeFor(t, ws, "miss", 5*time.Second); resp.Error == nil || resp.Error.Code != "not_found" {
		t.Errorf("missing: error = %+v, want not_found", resp.Error)
	}
	stale := time.Now().Add(-time.Hour)
	sendRequest(t, ws, "stale", "file.readRange", map[string]any{"taskId": task.ID, "path": "doc.pdf", "offset": 0, "length": 5, "ifMtime": DateTime(stale)})
	if resp = readEnvelopeFor(t, ws, "stale", 5*time.Second); resp.Error == nil || resp.Error.Code != "conflict" {
		t.Errorf("stale ifMtime: error = %+v, want conflict", resp.Error)
	}
	sendRequest(t, ws, "esc", "file.stat", map[string]any{"taskId": task.ID, "path": "../outside.pdf"})
	if resp = readEnvelopeFor(t, ws, "esc", 5*time.Second); resp.Error == nil {
		t.Error("../ escape over the wire: want refusal")
	}
}

func TestFileReadRange_ChunkCeilingPerTransport(t *testing.T) {
	t.Parallel()
	wm, _ := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	data := bytes.Repeat([]byte{0xAB}, 5<<20)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "big.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	h := handleFileReadRange(wm)
	for name, c := range map[string]struct {
		rc   *requestContext
		want int
	}{
		"direct":       {&requestContext{conn: &conn{maxMessage: DirectMaxMessage}}, FileMaxChunkDirect},
		"relay":        {&requestContext{conn: &conn{maxMessage: RelayMaxMessage}}, FileMaxChunkRelay},
		"unknown conn": {&requestContext{}, FileMaxChunkRelay},
	} {
		out, err := rpcResult(t, h, c.rc, map[string]any{"taskId": task.ID, "path": "big.bin", "offset": 0, "length": 1 << 30})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rr := out.(fileReadRangeResult); rr.Length != c.want || rr.EOF {
			t.Errorf("%s: length=%d eof=%v, want %d bytes", name, rr.Length, rr.EOF, c.want)
		}
	}
}

func TestDaemonInfoFilesCapability(t *testing.T) {
	t.Parallel()
	h := handleDaemonInfo(newDaemonInfo(time.Now()), nil)
	for name, c := range map[string]struct {
		rc   *requestContext
		want int
	}{
		"direct": {&requestContext{conn: &conn{maxMessage: DirectMaxMessage}}, 4 << 20},
		"relay":  {&requestContext{conn: &conn{maxMessage: RelayMaxMessage}}, 512 << 10},
	} {
		out, err := h(context.Background(), c.rc, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(out)
		var got struct {
			Files map[string]any `json:"files"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		f := got.Files
		if f == nil {
			t.Fatalf("%s: daemon.info has no files object: %s", name, raw)
		}
		if f["maxReadBytes"] != float64(50<<20) || f["maxChunkBytes"] != float64(c.want) {
			t.Errorf("%s: files = %v, want maxReadBytes 50 MiB maxChunkBytes %d", name, f, c.want)
		}
		if kinds, _ := f["previewKinds"].([]any); len(kinds) != 1 || kinds[0] != "pdf" {
			t.Errorf("%s: previewKinds = %v, want [pdf]", name, f["previewKinds"])
		}
	}
}

// TestFileRangeRelayChunkFitsFrame: the largest relay response (a full
// 512 KiB chunk, base64 in its JSON envelope) must fit one relay message,
// which RelayMaxMessage (1<<20 minus the AEAD/frame overhead) bounds.
func TestFileRangeRelayChunkFitsFrame(t *testing.T) {
	t.Parallel()
	chunk := make([]byte, FileMaxChunkRelay)
	if _, err := rand.Read(chunk); err != nil {
		t.Fatal(err)
	}
	res, err := json.Marshal(fileReadRangeResult{
		Offset: 1 << 40, Length: len(chunk), EOF: true, Data: chunk, Mtime: DateTime(time.Now()),
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := json.Marshal(envelope{ID: strings.Repeat("i", 64), Result: res})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) > RelayMaxMessage || len(env) >= 1<<20 {
		t.Fatalf("max relay chunk envelope is %d bytes, relay limit %d (frame 1<<20)", len(env), RelayMaxMessage)
	}
	if len(env) < 600<<10 {
		t.Fatalf("envelope %d bytes is suspiciously small for a 512 KiB chunk", len(env))
	}
	if got := fileChunkLimit(RelayMaxMessage); got != FileMaxChunkRelay {
		t.Errorf("relay chunk limit = %d", got)
	}
}

type fileLogBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *fileLogBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *fileLogBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// TestFileMethods_NotPluginReachableAndNoContentInLogs: both methods exist
// in the handler map, are on the plugin never-list and in no capability
// group; a canary file's bytes and absolute path never reach the log or an
// error text, only the result of a successful read.
func TestFileMethods_NotPluginReachableAndNoContentInLogs(t *testing.T) {
	// Not parallel: it swaps the slog default.
	e := newProcEnv(t)
	for _, m := range []string{"file.stat", "file.readRange"} {
		if _, ok := e.api.hs[m]; !ok {
			t.Errorf("%s is not in the handler map", m)
		}
		if !pluginNever(m) {
			t.Errorf("%s is not on the plugin never-list", m)
		}
		for capName, ms := range PluginCapabilityTable {
			for _, pm := range ms {
				if pm.Name == m {
					t.Errorf("%s is listed under plugin capability %q", m, capName)
				}
			}
		}
	}

	buf := &fileLogBuf{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const canary = "CANARY-9f3b1c-do-not-log"
	if err := os.WriteFile(filepath.Join(*e.task.WorktreePath, "secret.bin"), []byte(canary), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := &requestContext{conn: &conn{maxMessage: RelayMaxMessage}}
	var errTexts []string
	for _, call := range []struct {
		h handlerFunc
		p map[string]any
	}{
		{handleFileStat(e.wm), map[string]any{"taskId": e.task.ID, "path": "secret.bin"}},
		{handleFileReadRange(e.wm), map[string]any{"taskId": e.task.ID, "path": "secret.bin", "offset": 0, "length": 1000}},
		{handleFileReadRange(e.wm), map[string]any{"taskId": e.task.ID, "path": "secret.bin", "offset": -1, "length": 1}},
		{handleFileReadRange(e.wm), map[string]any{"taskId": e.task.ID, "path": "../secret.bin", "offset": 0, "length": 1}},
		{handleFileReadRange(e.wm), map[string]any{"taskId": e.task.ID, "path": "secret.bin", "offset": 0, "length": 1, "ifMtime": DateTime(time.Unix(1, 0))}},
		{handleFileStat(e.wm), map[string]any{"taskId": e.task.ID, "path": "nope.bin"}},
	} {
		out, err := rpcResult(t, call.h, rc, call.p)
		if err != nil {
			errTexts = append(errTexts, err.Error())
			continue
		}
		if rr, ok := out.(fileReadRangeResult); ok && string(rr.Data) != canary {
			t.Errorf("success read returned %q, want the canary", rr.Data)
		}
	}
	if len(errTexts) != 4 {
		t.Fatalf("expected 4 refusals, got %d: %v", len(errTexts), errTexts)
	}
	wt := *e.task.WorktreePath
	for _, s := range append(errTexts, buf.String()) {
		if strings.Contains(s, canary) || strings.Contains(s, wt) {
			t.Errorf("canary bytes or an absolute worktree path leaked into: %q", s)
		}
	}
}
