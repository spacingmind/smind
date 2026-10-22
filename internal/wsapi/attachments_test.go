package wsapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spacingmind/smind/internal/attach"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/testutil"
)

type attachEnv struct {
	api      *API
	srv      *httptest.Server
	ws       *websocket.Conn
	db       *store.Store
	task     store.Task
	storeDir string
	seq      int
}

// newAttachEnv serves a daemon with an attachment store (a t.TempDir, never
// ~/.smind) and a fake ACP agent that records the prompt blocks it receives.
func newAttachEnv(t *testing.T, scenario string, withStore bool) *attachEnv {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "record-prompt-blocks"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	e := &attachEnv{api: api, db: db, task: task, storeDir: filepath.Join(t.TempDir(), "attachments")}
	if withStore {
		api.SetAttachmentStore(attach.NewStore(e.storeDir))
	}
	e.srv = httptest.NewServer(api.Handler)
	t.Cleanup(e.srv.Close)
	e.ws = dialWS(t, e.srv, "tok")
	return e
}

func (e *attachEnv) call(t *testing.T, method string, params any) envelope {
	t.Helper()
	e.seq++
	id := method + "-" + string(rune('a'+e.seq%26)) + time.Now().Format("150405.000000")
	sendRequest(t, e.ws, id, method, params)
	return readEnvelopeFor(t, e.ws, id, 10*time.Second)
}

func (e *attachEnv) mustCall(t *testing.T, method string, params any, out any) {
	t.Helper()
	env := e.call(t, method, params)
	if env.Error != nil {
		t.Fatalf("%s error = %+v", method, env.Error)
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			t.Fatalf("decode %s: %v", method, err)
		}
	}
}

func (e *attachEnv) setDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(e.storeDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		out = append(out, en.Name())
	}
	return out
}

func (e *attachEnv) runCount(t *testing.T) int {
	t.Helper()
	runs, err := e.db.ListRecentRuns(100)
	if err != nil {
		t.Fatal(err)
	}
	return len(runs)
}

func (e *attachEnv) waitDone(t *testing.T, runID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var logs runLogsResult
		e.mustCall(t, "run.logs", map[string]any{"runId": runID}, &logs)
		if logs.Status == "done" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s still %q", runID, logs.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *attachEnv) promptBlocks(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(*e.task.WorktreePath, "prompt-blocks"))
	if err != nil {
		t.Fatalf("read prompt-blocks: %v", err)
	}
	return string(b)
}

func pngBase64(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func fileReq(name, content string) map[string]any {
	return map[string]any{"kind": "file", "name": name, "mimeType": "text/plain", "data": base64.StdEncoding.EncodeToString([]byte(content))}
}

func (e *attachEnv) start(t *testing.T, extra map[string]any) envelope {
	t.Helper()
	p := map[string]any{"taskId": e.task.ID, "provider": "glm", "prompt": "describe it"}
	for k, v := range extra {
		p[k] = v
	}
	return e.call(t, "run.start", p)
}

func (e *attachEnv) startOK(t *testing.T, extra map[string]any) string {
	t.Helper()
	env := e.start(t, extra)
	if env.Error != nil {
		t.Fatalf("run.start error = %+v", env.Error)
	}
	var res runStartResult
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatal(err)
	}
	return res.RunID
}

type listedRun struct {
	ID          string        `json:"ID"`
	Prompt      string        `json:"Prompt"`
	Attachments []attach.Item `json:"attachments"`
}

func (e *attachEnv) listRuns(t *testing.T) (decoded []listedRun, raw json.RawMessage) {
	t.Helper()
	env := e.call(t, "run.list", map[string]any{})
	if env.Error != nil {
		t.Fatal(env.Error.Message)
	}
	if err := json.Unmarshal(env.Result, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded, env.Result
}

func TestRunStart_AttachmentsAccepted(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	runID := e.startOK(t, map[string]any{"attachments": []any{
		map[string]any{"kind": "image", "name": "shot.png", "mimeType": "image/png", "data": pngBase64(t)},
		fileReq("notes.txt", "hello notes"),
	}})
	e.waitDone(t, runID)

	if dirs := e.setDirs(t); len(dirs) != 1 {
		t.Fatalf("set dirs = %v, want exactly one", dirs)
	}
	got, _ := e.listRuns(t)
	if len(got) != 1 || got[0].Prompt != "describe it" || len(got[0].Attachments) != 2 {
		t.Fatalf("run.list = %+v", got)
	}
	if a := got[0].Attachments[0]; a.Kind != "image" || a.ID != "1" || a.Width != 2 || a.Height != 3 || a.Name != "shot.png" {
		t.Fatalf("image metadata = %+v", a)
	}
	blocks := e.promptBlocks(t)
	if !strings.Contains(blocks, "describe it") || !strings.Contains(blocks, "01-shot.png") || !strings.Contains(blocks, "02-notes.txt") {
		t.Fatalf("agent prompt = %s", blocks)
	}
	row, err := e.db.GetRun(runID)
	if err != nil || row.Prompt != "describe it" {
		t.Fatalf("stored run = %+v, %v", row, err)
	}
}

func TestRunStart_NoAttachmentsUnchanged(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	runID := e.startOK(t, nil)
	e.waitDone(t, runID)
	if !strings.Contains(e.promptBlocks(t), `"text":"describe it"}]`) || strings.Contains(e.promptBlocks(t), "attached") {
		t.Fatalf("agent prompt = %s", e.promptBlocks(t))
	}
	_, raw := e.listRuns(t)
	if strings.Contains(string(raw), `"attachments"`) {
		t.Fatalf("run.list carries an attachments key without attachments: %s", raw)
	}
	if dirs := e.setDirs(t); len(dirs) != 0 {
		t.Fatalf("set dirs = %v", dirs)
	}
	// Empty list == omitted.
	runID = e.startOK(t, map[string]any{"attachments": []any{}})
	e.waitDone(t, runID)
	if dirs := e.setDirs(t); len(dirs) != 0 {
		t.Fatalf("set dirs after empty list = %v", dirs)
	}
}

func TestRunStart_RejectionCreatesNothing(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "hang", true)
	hang := e.startOK(t, nil) // makes the chat busy
	t.Cleanup(func() { e.call(t, "run.stop", map[string]any{"runId": hang}) })
	baseRuns := e.runCount(t)

	cases := map[string]struct {
		extra map[string]any
		code  string
	}{
		"invalid base64": {map[string]any{"attachments": []any{map[string]any{"kind": "file", "name": "a", "data": "!!not base64!!"}}}, "invalid_base64"},
		"mime mismatch":  {map[string]any{"attachments": []any{map[string]any{"kind": "image", "name": "a.png", "mimeType": "image/jpeg", "data": pngBase64(t)}}}, "mime_mismatch"},
		"too many": {map[string]any{"attachments": func() []any {
			var out []any
			for i := 0; i < 11; i++ {
				out = append(out, fileReq("a.txt", "x"))
			}
			return out
		}()}, "too_many_attachments"},
		"busy chat, reject": {map[string]any{"attachments": []any{fileReq("a.txt", "x")}}, ""},
		"bad provider":      {map[string]any{"provider": "nope", "attachments": []any{fileReq("a.txt", "x")}}, ""},
	}
	for name, c := range cases {
		env := e.start(t, c.extra)
		if env.Error == nil {
			t.Fatalf("%s: run.start succeeded", name)
		}
		if c.code != "" && env.Error.Code != c.code {
			t.Fatalf("%s: code = %q, want %q (%s)", name, env.Error.Code, c.code, env.Error.Message)
		}
	}
	if n := e.runCount(t); n != baseRuns {
		t.Fatalf("runs = %d, want %d", n, baseRuns)
	}
	if dirs := e.setDirs(t); len(dirs) != 0 {
		t.Fatalf("store dir not empty after rejections: %v", dirs)
	}
	var q chatQueueListResult
	chat, err := e.db.ListChatsByTask(e.task.ID, false)
	if err != nil || len(chat) == 0 {
		t.Fatalf("chats: %v %v", chat, err)
	}
	e.mustCall(t, "chat.queueList", map[string]any{"chatId": chat[0].ID}, &q)
	if len(q.Items) != 0 {
		t.Fatalf("queue = %+v", q.Items)
	}
}

func TestRunStart_AttachmentsDisabledWithoutStore(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", false)
	env := e.start(t, map[string]any{"attachments": []any{fileReq("a.txt", "x")}})
	if env.Error == nil || !strings.Contains(env.Error.Message, "attachments are not enabled on this daemon") {
		t.Fatalf("error = %+v", env.Error)
	}
	if e.runCount(t) != 0 {
		t.Fatal("a run was created")
	}
}

func TestTaskPrompt_AttachmentsAccepted(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	env := e.call(t, "task.prompt", map[string]any{
		"taskId": e.task.ID, "provider": "glm", "prompt": "via prompt",
		"attachments": []any{fileReq("n.txt", "x")},
	})
	if env.Error != nil {
		t.Fatalf("task.prompt error = %+v", env.Error)
	}
	got, _ := e.listRuns(t)
	if len(got) != 1 || len(got[0].Attachments) != 1 || got[0].Prompt != "via prompt" {
		t.Fatalf("run.list = %+v", got)
	}
	if !strings.Contains(e.promptBlocks(t), "01-n.txt") {
		t.Fatalf("agent prompt = %s", e.promptBlocks(t))
	}
}

func TestRunList_CarriesAttachmentMetadata(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	e.waitDone(t, e.startOK(t, map[string]any{"attachments": []any{fileReq("n.txt", "abc")}}))
	_, raw := e.listRuns(t)
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	items := rows[0]["attachments"].([]any)
	it := items[0].(map[string]any)
	if it["kind"] != "file" || it["name"] != "n.txt" || it["size"] != float64(3) || it["id"] != "1" {
		t.Fatalf("item = %v", it)
	}
	if _, leaked := it["file"]; leaked {
		t.Fatalf("on-disk file name leaked: %v", it)
	}
}

func TestRunLogs_CarriesAttachmentMetadata(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	runID := e.startOK(t, map[string]any{"attachments": []any{fileReq("n.txt", "abc")}})
	e.waitDone(t, runID)
	var logs runLogsResult
	e.mustCall(t, "run.logs", map[string]any{"runId": runID}, &logs)
	if len(logs.Attachments) != 1 || logs.Attachments[0].Name != "n.txt" || logs.Attachments[0].File != "" {
		t.Fatalf("run.logs attachments = %+v", logs.Attachments)
	}
}

func TestChatQueueList_CarriesAttachmentMetadata(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "hang", true)
	hang := e.startOK(t, nil)
	chats, err := e.db.ListChatsByTask(e.task.ID, false)
	if err != nil || len(chats) == 0 {
		t.Fatalf("chats: %v %v", chats, err)
	}
	ec := newEventConn(t, e.ws)
	t.Cleanup(func() {
		sendRequest(t, e.ws, "stop", "run.stop", map[string]any{"runId": hang})
		ec.nextResponse("stop", 5*time.Second)
	})
	ec.subscribe("sub", "chat.queueUpdated")
	sendRequest(t, e.ws, "q", "run.start", map[string]any{
		"taskId": e.task.ID, "chatId": chats[0].ID, "provider": "glm", "prompt": "later", "whenBusy": "queue",
		"attachments": []any{fileReq("q.txt", "queued")},
	})
	if resp := ec.nextResponse("q", 5*time.Second); resp.Error != nil {
		t.Fatalf("queue error = %+v", resp.Error)
	}
	sendRequest(t, e.ws, "ls", "chat.queueList", map[string]any{"chatId": chats[0].ID})
	resp := ec.nextResponse("ls", 5*time.Second)
	var q chatQueueListResult
	if err := json.Unmarshal(resp.Result, &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Items) != 1 || len(q.Items[0].Attachments) != 1 || q.Items[0].Attachments[0].Name != "q.txt" || q.Items[0].Attachments[0].File != "" {
		t.Fatalf("queue list = %+v", q.Items)
	}
	ev := ec.nextEvent(5 * time.Second)
	var snap chatQueueUpdatedPayload
	decodePayload(t, ev, &snap)
	if len(snap.Items) != 1 || len(snap.Items[0].Attachments) != 1 {
		t.Fatalf("queueUpdated snapshot = %+v", snap)
	}
}

func TestRunAttachment_ReturnsBytes(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	runID := e.startOK(t, map[string]any{"attachments": []any{
		map[string]any{"kind": "image", "name": "s.png", "mimeType": "image/png", "data": pngBase64(t)},
		fileReq("n.txt", "the bytes"),
	}})
	e.waitDone(t, runID)
	var res runAttachmentResult
	e.mustCall(t, "run.attachment", map[string]any{"runId": runID, "id": "2"}, &res)
	data, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil || string(data) != "the bytes" || res.Name != "n.txt" || res.MimeType != "text/plain" || res.Size != 9 {
		t.Fatalf("result = %+v (%q, %v)", res, data, err)
	}
	e.mustCall(t, "run.attachment", map[string]any{"runId": runID, "id": "1"}, &res)
	if res.MimeType != "image/png" {
		t.Fatalf("image result = %+v", res)
	}
}

func TestRunAttachment_NotFoundForOtherKinds(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	runID := e.startOK(t, map[string]any{"attachments": []any{
		map[string]any{"kind": "file_ref", "path": "README.md"},
		fileReq("n.txt", "x"),
	}})
	e.waitDone(t, runID)
	for name, p := range map[string]map[string]any{
		"file_ref":    {"runId": runID, "id": "1"},
		"unknown id":  {"runId": runID, "id": "9"},
		"unknown run": {"runId": "nope", "id": "1"},
	} {
		if env := e.call(t, "run.attachment", p); env.Error == nil || env.Error.Code != "not_found" {
			t.Fatalf("%s: error = %+v", name, env.Error)
		}
	}
	off := newAttachEnv(t, "", false)
	if env := off.call(t, "run.attachment", map[string]any{"runId": "x", "id": "1"}); env.Error == nil || env.Error.Code != "not_found" {
		t.Fatalf("store off: error = %+v", env.Error)
	}
}

func rpcPipe(t *testing.T, c *pipeTransport, id, method string, params any) envelope {
	t.Helper()
	data, err := json.Marshal(envelope{ID: id, Method: method, Params: marshalOrNull(params)})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(data); err != nil {
		t.Fatal(err)
	}
	for {
		raw, err := c.Receive()
		if err != nil {
			t.Fatal(err)
		}
		var env envelope
		if json.Unmarshal(raw, &env) == nil && env.ID == id {
			return env
		}
	}
}

func (e *attachEnv) relayClient(t *testing.T) *pipeTransport {
	t.Helper()
	client, server := newPipeTransportPair()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.api.ServeTransport(ctx, server); close(done) }()
	t.Cleanup(func() { client.Close(); cancel(); <-done })
	return client
}

func TestRunAttachment_TooLargeForRelay(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	big := strings.Repeat("x", 1<<20) // base64 ~1.4 MB > 1048551
	runID := e.startOK(t, map[string]any{"attachments": []any{fileReq("big.txt", big), fileReq("small.txt", "ok")}})
	e.waitDone(t, runID)

	relay := e.relayClient(t)
	env := rpcPipe(t, relay, "r1", "run.attachment", map[string]any{"runId": runID, "id": "1"})
	if env.Error == nil || env.Error.Code != "attachment_too_large" {
		t.Fatalf("relay big = %+v", env.Error)
	}
	if env = rpcPipe(t, relay, "r2", "run.attachment", map[string]any{"runId": runID, "id": "2"}); env.Error != nil {
		t.Fatalf("relay small = %+v", env.Error)
	}
	var res runAttachmentResult
	e.mustCall(t, "run.attachment", map[string]any{"runId": runID, "id": "1"}, &res)
	if res.Size != 1<<20 {
		t.Fatalf("direct big size = %d", res.Size)
	}
}

func TestWSReadLimit_Closes1009(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	huge := bytes.Repeat([]byte("a"), DirectMaxMessage+1024)
	_ = e.ws.WriteMessage(websocket.TextMessage, huge)
	_ = e.ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, _, err := e.ws.ReadMessage()
		if err == nil {
			continue
		}
		if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
			t.Fatalf("read error = %v, want close 1009", err)
		}
		return
	}
}

func TestDaemonInfo_ReportsAttachmentLimits(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	var direct map[string]any
	e.mustCall(t, "daemon.info", nil, &direct)
	att, ok := direct["attachments"].(map[string]any)
	if !ok {
		t.Fatalf("daemon.info = %v", direct)
	}
	want := map[string]float64{"maxCount": 10, "maxImageBytes": 5 << 20, "maxFileBytes": 10 << 20, "maxTotalBytes": 10 << 20, "maxMessageBytes": 16 << 20}
	for k, v := range want {
		if att[k] != v {
			t.Errorf("direct %s = %v, want %v", k, att[k], v)
		}
	}
	if imgs, _ := att["images"].([]any); len(imgs) != 4 {
		t.Errorf("images = %v", att["images"])
	}
	if att["github"] != false {
		t.Errorf("github = %v", att["github"])
	}
	env := rpcPipe(t, e.relayClient(t), "i", "daemon.info", nil)
	var viaRelay map[string]any
	_ = json.Unmarshal(env.Result, &viaRelay)
	if m := viaRelay["attachments"].(map[string]any)["maxMessageBytes"]; m != float64(RelayMaxMessage) {
		t.Errorf("relay maxMessageBytes = %v", m)
	}

	off := newAttachEnv(t, "", false)
	var info map[string]any
	off.mustCall(t, "daemon.info", nil, &info)
	if _, has := info["attachments"]; has {
		t.Errorf("daemon without a store reports attachments: %v", info)
	}
}

func TestFileRef_AcceptedNormalised(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	wt := *e.task.WorktreePath
	if err := os.MkdirAll(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "sub", "f.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runID := e.startOK(t, map[string]any{"attachments": []any{
		map[string]any{"kind": "file_ref", "path": "./sub/../sub/f.go"},
		map[string]any{"kind": "file_ref", "path": filepath.Join(wt, "README.md")},
		map[string]any{"kind": "file_ref", "path": "sub"},
	}})
	e.waitDone(t, runID)
	var logs runLogsResult
	e.mustCall(t, "run.logs", map[string]any{"runId": runID}, &logs)
	if len(logs.Attachments) != 3 {
		t.Fatalf("attachments = %+v", logs.Attachments)
	}
	if a := logs.Attachments[0]; a.Kind != "file_ref" || a.Path != "sub/f.go" || a.IsDir {
		t.Fatalf("item 1 = %+v", a)
	}
	if a := logs.Attachments[1]; a.Path != "README.md" {
		t.Fatalf("item 2 = %+v", a)
	}
	if a := logs.Attachments[2]; a.Path != "sub" || !a.IsDir {
		t.Fatalf("item 3 = %+v", a)
	}
	if dirs := e.setDirs(t); len(dirs) != 1 {
		t.Fatalf("set dirs = %v", dirs)
	}
}

func TestFileRef_OutsideWorkspaceRejected(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", true)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Link(t, secret, filepath.Join(*e.task.WorktreePath, "evil"))
	for _, p := range []string{"../outside.txt", "sub/../../x", secret, "evil"} {
		env := e.start(t, map[string]any{"attachments": []any{map[string]any{"kind": "file_ref", "path": p}}})
		if env.Error == nil || env.Error.Code != "outside_workspace" {
			t.Fatalf("%q: error = %+v", p, env.Error)
		}
		if strings.Contains(env.Error.Message, outside) {
			t.Fatalf("%q: error leaks a host path: %s", p, env.Error.Message)
		}
	}
	env := e.start(t, map[string]any{"attachments": []any{map[string]any{"kind": "file_ref", "path": "missing.txt"}}})
	if env.Error == nil || env.Error.Code != "not_found" {
		t.Fatalf("missing: error = %+v", env.Error)
	}
	if e.runCount(t) != 0 || len(e.setDirs(t)) != 0 {
		t.Fatal("a rejection left a run or a set behind")
	}
}

// A file_ref is a path, not content: a file the daemon cannot read is still
// accepted, and its bytes appear nowhere.
func TestFileRef_ContentsNeverRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX permission bits")
	}
	e := newAttachEnv(t, "", true)
	p := filepath.Join(*e.task.WorktreePath, "unreadable.txt")
	if err := os.WriteFile(p, []byte("file-ref-content-sentinel"), 0o000); err != nil {
		t.Fatal(err)
	}
	runID := e.startOK(t, map[string]any{"attachments": []any{map[string]any{"kind": "file_ref", "path": "unreadable.txt"}}})
	e.waitDone(t, runID)
	if blocks := e.promptBlocks(t); !strings.Contains(blocks, "unreadable.txt") || strings.Contains(blocks, "file-ref-content-sentinel") {
		t.Fatalf("agent prompt = %s", blocks)
	}
}
