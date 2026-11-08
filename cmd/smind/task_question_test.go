package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// questionTestEnvelope is wireEnvelope plus the error a failing call carries.
type questionTestEnvelope struct {
	ID     string            `json:"id,omitempty"`
	Result json.RawMessage   `json:"result,omitempty"`
	Error  map[string]string `json:"error,omitempty"`
}

// fakeQuestionDaemon serves run.logs with the given status and events and
// records every run.answerQuestion call's params; a non-empty answerErr
// makes that call fail with it.
func fakeQuestionDaemon(t *testing.T, status string, events []map[string]any, answerErr string) (answers func() []map[string]any) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)

	var mu sync.Mutex
	var got []map[string]any
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var req wireEnvelope
			if json.Unmarshal(data, &req) != nil {
				return
			}
			out := questionTestEnvelope{ID: req.ID}
			switch req.Method {
			case "run.logs":
				out.Result, _ = json.Marshal(map[string]any{"status": status, "events": events})
			case "run.answerQuestion":
				var p map[string]any
				_ = json.Unmarshal(req.Params, &p)
				mu.Lock()
				got = append(got, p)
				mu.Unlock()
				if answerErr != "" {
					out.Error = map[string]string{"message": answerErr}
				} else {
					out.Result = json.RawMessage(`{}`)
				}
			default:
				out.Result = json.RawMessage(`{}`)
			}
			raw, _ := json.Marshal(out)
			if ws.WriteMessage(websocket.TextMessage, raw) != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	writeTestConfig(t, home, srv.URL)
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), got...)
	}
}

func questionEvent(requestID string, items ...map[string]any) map[string]any {
	return map[string]any{"type": "question_request", "requestId": requestID,
		"question": map[string]any{"source": "claude", "questions": items}}
}

func TestSanitizeTerminalText(t *testing.T) {
	in := "a\x1b[31mred\x1b[0m\r\nb\tc\x07\u0085d"
	want := "a?[31mred?[0m?\nb\tc??d"
	if got := sanitizeTerminalText(in); got != want {
		t.Fatalf("sanitizeTerminalText = %q, want %q", got, want)
	}
	if got := sanitizeTerminalText("héllo wörld"); got != "héllo wörld" {
		t.Fatalf("plain text changed: %q", got)
	}
}

func TestRenderQuestionRequest(t *testing.T) {
	q := questionRequest{Questions: []questionItem{
		{ID: "q0", Header: "Color", Text: "Pick one", Kind: "single", Options: []questionOption{{ID: "o0", Label: "Red", Description: "warm"}, {ID: "o1", Label: "Blue"}}},
		{ID: "q1", Text: "Pick several", Kind: "multi", AllowOther: true, Options: []questionOption{{ID: "a", Label: "A"}}},
		{ID: "q2", Header: "Name", Text: "Free text?", Kind: "text"},
	}, Truncated: true}
	out := renderQuestionRequest("run-1", "req-1", q)
	for _, want := range []string{
		"[question] Color: Pick one (request req-1)\n",
		"  1) Red - warm\n",
		"  2) Blue\n",
		"[question] Pick several (request req-1)\n",
		"  1) A\n",
		"  *) other: free text\n",
		"[question] Name: Free text? (request req-1)\n",
		"  (text was shortened)\n",
		"  -> answer:  smind task answer run-1 req-1\n",
		"  -> decline: smind task answer run-1 req-1 --decline [--note \"...\"]\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "*) other") != 1 {
		t.Errorf("other marker should appear only for the allowOther question:\n%s", out)
	}
	hostile := renderQuestionRequest("r", "q", questionRequest{Questions: []questionItem{{ID: "q0", Header: "h\x1b[2J", Text: "t\x1b]0;x\x07", Options: []questionOption{{Label: "l\x1b", Description: "d\r"}}}}})
	if strings.ContainsAny(hostile, "\x1b\x07\r") {
		t.Errorf("control characters leaked: %q", hostile)
	}
}

func TestTaskPermissions_ListsPendingQuestions(t *testing.T) {
	events := []map[string]any{
		questionEvent("req-1", map[string]any{"id": "q0", "text": "Resolved?", "kind": "text"}),
		{"type": "question_resolved", "requestId": "req-1", "reason": "human"},
		questionEvent("req-2", map[string]any{"id": "q0", "header": "Color", "text": "Pick", "kind": "single",
			"options": []map[string]any{{"id": "o0", "label": "Red"}}}),
	}
	fakeQuestionDaemon(t, "running", events, "")
	var code int
	out := captureStdout(t, func() { code = run([]string{"task", "permissions", "run-1"}) })
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "Color: Pick (request req-2)") || strings.Contains(out, "Resolved?") {
		t.Fatalf("stdout = %q", out)
	}

	fakeQuestionDaemon(t, "done", events, "")
	out = captureStdout(t, func() { code = run([]string{"task", "permissions", "run-1"}) })
	if code != 0 || !strings.Contains(out, "no pending permission requests or questions") {
		t.Fatalf("terminal run: code=%d stdout=%q", code, out)
	}
}
