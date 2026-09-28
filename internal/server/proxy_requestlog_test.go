package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// waitForRequestLog polls s until a row matching pred appears, or fails
// the test after a generous timeout -- the async writer means a row isn't
// necessarily there the instant the HTTP response finishes.
func waitForRequestLog(t *testing.T, s *store.Store, pred func(store.RequestLog) bool) store.RequestLog {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, err := s.ListRequestLogs(store.RequestLogFilter{})
		if err != nil {
			t.Fatalf("ListRequestLogs() error = %v", err)
		}
		for _, r := range rows {
			if pred(r) {
				return r
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no matching request_log row within timeout; rows = %+v", rows)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func int64Val(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

// TestProxy_RequestLog_AnthropicNonStream covers the M1 scenario
// "Anthropic non-stream: all four token fields recorded, outcome=ok" plus
// the byte-for-byte requirement for a non-stream response.
func TestProxy_RequestLog_AnthropicNonStream(t *testing.T) {
	t.Parallel()

	const respBody = `{"id":"msg_1","type":"message","content":[{"type":"text","text":"hi"}],"model":"claude-3","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":3,"cache_read_input_tokens":4}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, respBody)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	reqBody := `{"model":"claude-3","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if w.Body.String() != respBody {
		t.Fatalf("client body = %q, want byte-identical %q", w.Body.String(), respBody)
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.Outcome != store.RequestLogOutcomeOK {
		t.Errorf("Outcome = %q, want ok", row.Outcome)
	}
	if row.Status != http.StatusOK {
		t.Errorf("Status = %d, want 200", row.Status)
	}
	if int64Val(row.InputTokens) != 10 || int64Val(row.OutputTokens) != 20 ||
		int64Val(row.CacheWriteTokens) != 3 || int64Val(row.CacheReadTokens) != 4 {
		t.Errorf("tokens = in=%v out=%v cw=%v cr=%v, want 10/20/3/4",
			row.InputTokens, row.OutputTokens, row.CacheWriteTokens, row.CacheReadTokens)
	}
	if row.Model == nil || *row.Model != "claude-3" {
		t.Errorf("Model = %v, want claude-3", row.Model)
	}
	if row.Stream == nil || *row.Stream {
		t.Errorf("Stream = %v, want false", row.Stream)
	}
	if row.Error != nil {
		t.Errorf("Error = %v, want nil for an ok outcome", row.Error)
	}
}

// sseFrame renders one SSE event, matching what Anthropic and OpenAI both
// actually send: an "event:" line (Anthropic only) followed by "data:"
// and a blank line.
func sseFrame(event, dataJSON string) string {
	var b strings.Builder
	if event != "" {
		b.WriteString("event: " + event + "\n")
	}
	b.WriteString("data: " + dataJSON + "\n\n")
	return b.String()
}

// TestProxy_RequestLog_AnthropicStream covers the M1 scenario "Anthropic
// stream: message_start (input 100, cache_read 40), then two
// message_delta events (output 5, then 12), records 100/40/12."
func TestProxy_RequestLog_AnthropicStream(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	body.WriteString(sseFrame("message_start", `{"type":"message_start","message":{"id":"msg_1","model":"claude-3","usage":{"input_tokens":100,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":40}}}`))
	body.WriteString(sseFrame("content_block_delta", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`))
	body.WriteString(sseFrame("message_delta", `{"type":"message_delta","delta":{"stop_reason":null},"usage":{"output_tokens":5}}`))
	body.WriteString(sseFrame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`))
	full := body.String()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, full)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3","stream":true}`))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if w.Body.String() != full {
		t.Fatalf("client stream body mismatch (want byte-identical passthrough)")
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.Outcome != store.RequestLogOutcomeOK {
		t.Errorf("Outcome = %q, want ok", row.Outcome)
	}
	if int64Val(row.InputTokens) != 100 || int64Val(row.CacheReadTokens) != 40 || int64Val(row.OutputTokens) != 12 {
		t.Errorf("tokens = in=%v cr=%v out=%v, want 100/40/12", row.InputTokens, row.CacheReadTokens, row.OutputTokens)
	}
}

// TestProxy_RequestLog_AnthropicStream_CompatibleUpstreamInputInDelta
// covers "Compatible stream with input tokens only in message_delta:
// input is recorded" -- a non-Anthropic upstream speaking the same wire
// format but only reporting input_tokens in message_delta, never
// message_start.
func TestProxy_RequestLog_AnthropicStream_CompatibleUpstreamInputInDelta(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	body.WriteString(sseFrame("message_start", `{"type":"message_start","message":{"id":"msg_1","model":"glm-x","usage":{"output_tokens":0}}}`))
	body.WriteString(sseFrame("message_delta", `{"type":"message_delta","delta":{},"usage":{"output_tokens":7,"input_tokens":55}}`))
	full := body.String()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, full)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-glm")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"glm-x","stream":true}`))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if int64Val(row.InputTokens) != 55 {
		t.Errorf("InputTokens = %v, want 55 (from message_delta)", row.InputTokens)
	}
	if int64Val(row.OutputTokens) != 7 {
		t.Errorf("OutputTokens = %v, want 7", row.OutputTokens)
	}
}

// TestProxy_RequestLog_OpenAINonStream covers "OpenAI, non-stream: prompt,
// completion, cached, and reasoning tokens recorded."
func TestProxy_RequestLog_OpenAINonStream(t *testing.T) {
	t.Parallel()

	const respBody = `{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":30,"completion_tokens":8,"prompt_tokens_details":{"cached_tokens":6},"completion_tokens_details":{"reasoning_tokens":2}}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, respBody)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withOpenAIHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerOpenAI, "o1", "sk-openai-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	w := httptest.NewRecorder()
	p.handleOpenAI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if w.Body.String() != respBody {
		t.Fatalf("client body mismatch, want byte-identical")
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if int64Val(row.InputTokens) != 30 || int64Val(row.OutputTokens) != 8 ||
		int64Val(row.CacheReadTokens) != 6 || int64Val(row.ReasoningTokens) != 2 {
		t.Errorf("tokens = in=%v out=%v cr=%v reasoning=%v, want 30/8/6/2",
			row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.ReasoningTokens)
	}
}

// TestProxy_RequestLog_OpenAIStream_ClientIncludeUsageUnchanged covers
// "OpenAI stream with the client's own include_usage: request body
// forwarded unchanged."
func TestProxy_RequestLog_OpenAIStream_ClientIncludeUsageUnchanged(t *testing.T) {
	t.Parallel()

	const reqBody = `{"model":"gpt-5","stream":true,"stream_options":{"include_usage":true}}`
	var gotReqBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotReqBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sseFrame("", `{"choices":[{"delta":{"content":"hi"}}]}`)+"data: [DONE]\n\n")
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withOpenAIHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerOpenAI, "o1", "sk-openai-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	w := httptest.NewRecorder()
	p.handleOpenAI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if gotReqBody != reqBody {
		t.Errorf("upstream received body = %q, want byte-identical %q", gotReqBody, reqBody)
	}
}

// TestProxy_RequestLog_OpenAIStream_InjectsIncludeUsage covers "OpenAI
// stream without it: the upstream receives it injected, and the client
// still gets every content chunk in order."
func TestProxy_RequestLog_OpenAIStream_InjectsIncludeUsage(t *testing.T) {
	t.Parallel()

	var gotReqBody string
	respBody := sseFrame("", `{"choices":[{"delta":{"content":"one"}}]}`) +
		sseFrame("", `{"choices":[{"delta":{"content":"two"}}]}`) +
		sseFrame("", `{"choices":[{"delta":{}}],"usage":{"prompt_tokens":9,"completion_tokens":3}}`) +
		"data: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotReqBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, respBody)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withOpenAIHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerOpenAI, "o1", "sk-openai-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","stream":true}`))
	w := httptest.NewRecorder()
	p.handleOpenAI(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if w.Body.String() != respBody {
		t.Fatalf("client stream body mismatch, want every chunk passed through in order")
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(gotReqBody), &sent); err != nil {
		t.Fatalf("upstream body not valid JSON: %v; body = %s", err, gotReqBody)
	}
	opts, _ := sent["stream_options"].(map[string]any)
	if opts == nil || opts["include_usage"] != true {
		t.Fatalf("upstream stream_options = %+v, want include_usage injected true", sent["stream_options"])
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if int64Val(row.InputTokens) != 9 || int64Val(row.OutputTokens) != 3 {
		t.Errorf("tokens = in=%v out=%v, want 9/3", row.InputTokens, row.OutputTokens)
	}
}

// TestProxy_RequestLog_UpstreamErrorStatus covers "Upstream 429:
// upstream_error, and the client gets the upstream body."
func TestProxy_RequestLog_UpstreamErrorStatus(t *testing.T) {
	t.Parallel()

	const errBody = `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, errBody)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if w.Body.String() != errBody {
		t.Fatalf("client body = %q, want the upstream error body verbatim", w.Body.String())
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.Outcome != store.RequestLogOutcomeUpstreamError {
		t.Errorf("Outcome = %q, want upstream_error", row.Outcome)
	}
	if row.UpstreamStatus == nil || *row.UpstreamStatus != http.StatusTooManyRequests {
		t.Errorf("UpstreamStatus = %v, want 429", row.UpstreamStatus)
	}
	if row.AccountID == nil {
		t.Errorf("AccountID = nil, want set (an account was chosen and dialed)")
	}
}

// TestProxy_RequestLog_NoAccountsIsRouteError covers "No accounts: 503,
// route_error, account_id NULL."
func TestProxy_RequestLog_NoAccountsIsRouteError(t *testing.T) {
	t.Parallel()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s)
	t.Cleanup(p.Close)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.Outcome != store.RequestLogOutcomeRouteError {
		t.Errorf("Outcome = %q, want route_error", row.Outcome)
	}
	if row.AccountID != nil {
		t.Errorf("AccountID = %v, want nil", row.AccountID)
	}
	if row.Status != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want 503", row.Status)
	}
}

// TestProxy_RequestLog_MidStreamAbortRecordsPartialTokens covers "Mid-
// stream upstream break: aborted with partial tokens, and
// panic(http.ErrAbortHandler) is unchanged."
func TestProxy_RequestLog_MidStreamAbortRecordsPartialTokens(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sseFrame("message_start", `{"type":"message_start","message":{"id":"msg_1","model":"claude-3","usage":{"input_tokens":100,"output_tokens":0}}}`))
		w.(http.Flusher).Flush()

		// Simulate the upstream connection dying mid-stream, after the
		// client has already learned input_tokens via message_start.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Log("upstream ResponseWriter does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Logf("Hijack() error = %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	proxySrv := httptest.NewServer(http.HandlerFunc(p.handleAnthropic))
	defer proxySrv.Close()

	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/messages", strings.NewReader(`{"model":"claude-3","stream":true}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body) // drain; the truncation itself is TestProxy_UpstreamStreamBreakAbortsDownstream's concern

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.Outcome != store.RequestLogOutcomeAborted {
		t.Errorf("Outcome = %q, want aborted", row.Outcome)
	}
	if int64Val(row.InputTokens) != 100 {
		t.Errorf("InputTokens = %v, want 100 (from message_start, seen before the break)", row.InputTokens)
	}
	if row.Error == nil || *row.Error == "" {
		t.Errorf("Error = %v, want a non-empty short message", row.Error)
	}
}

// TestProxy_RequestLog_ClientDisconnectRecordsClientCancelled covers
// "Client disconnect: client_cancelled." The upstream keeps writing
// chunks on a short tick, stopping as soon as a write fails -- rather
// than gating a single second write behind a fixed delay after the
// client disconnects, which would race the OS's TCP teardown -- so the
// break is detected on whichever tick first lands after the client is
// actually gone.
func TestProxy_RequestLog_ClientDisconnectRecordsClientCancelled(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i := 0; i < 200; i++ {
			if _, err := io.WriteString(w, sseFrame("", `{"chunk":true}`)); err != nil {
				return
			}
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	proxySrv := httptest.NewServer(http.HandlerFunc(p.handleAnthropic))
	defer proxySrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxySrv.URL+"/v1/messages", strings.NewReader(`{"model":"claude-3","stream":true}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("read first chunk: %v", err)
	}
	cancel()
	_ = resp.Body.Close()

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.Outcome != store.RequestLogOutcomeClientCancelled {
		t.Errorf("Outcome = %q, want client_cancelled", row.Outcome)
	}
}

// TestProxy_RequestLog_OverResponseParseCap covers "Over the parse cap:
// tokens NULL, response intact."
func TestProxy_RequestLog_OverResponseParseCap(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat("x", responseBodyParseCap+1024)
	respBody := `{"id":"msg_1","padding":"` + padding + `","usage":{"input_tokens":10,"output_tokens":20}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, respBody)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body len = %d", w.Code, w.Body.Len())
	}
	if w.Body.String() != respBody {
		t.Fatalf("client body was altered by capped parsing (len got=%d want=%d)", w.Body.Len(), len(respBody))
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.InputTokens != nil || row.OutputTokens != nil {
		t.Errorf("tokens = in=%v out=%v, want nil (over parse cap)", row.InputTokens, row.OutputTokens)
	}
	if row.Outcome != store.RequestLogOutcomeOK {
		t.Errorf("Outcome = %q, want ok (the response itself is fine, only parsing was capped)", row.Outcome)
	}
}

// TestProxy_RequestLog_WriterFailureLeavesResponseUnaffected covers
// "Writer queue full / closed DB: the response is unaffected and the
// counter increments," at the HTTP layer: an always-erroring
// requestLogStore must never change what the client gets back.
func TestProxy_RequestLog_WriterFailureLeavesResponseUnaffected(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	writer := newRequestLogWriter(erroringStore{}, 4)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)), withRequestLogWriter(writer))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` {
		t.Fatalf("status = %d, body = %q, want 200 {\"ok\":true} regardless of the logging failure", w.Code, w.Body.String())
	}

	waitUntil(t, 2*time.Second, func() bool { return writer.Dropped() == 1 })
}

// TestProxy_RequestLog_NoSensitiveData covers "The raw row contains
// neither the API key nor any prompt text."
func TestProxy_RequestLog_NoSensitiveData(t *testing.T) {
	t.Parallel()

	const secretKey = "sk-ant-super-secret-value"
	const promptText = "the secret launch codes are 1234"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"msg_1","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", secretKey)

	reqBody := `{"model":"claude-3","messages":[{"role":"user","content":"` + promptText + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("x-api-key", "incoming-client-key")
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	if strings.Contains(string(raw), secretKey) {
		t.Errorf("row contains the account's API key: %s", raw)
	}
	if strings.Contains(string(raw), "incoming-client-key") {
		t.Errorf("row contains the caller's raw credential: %s", raw)
	}
	if strings.Contains(string(raw), promptText) {
		t.Errorf("row contains prompt text: %s", raw)
	}
}

// TestProxy_ForcesIdentityAcceptEncoding covers the review bug: a client
// sending "Accept-Encoding: gzip, deflate, br" must not make the upstream
// compress its response (usage parsing needs plaintext, and our uTLS
// RoundTripper has no transparent decompression). The upstream fake
// asserts it received identity and returns plaintext, and usage is
// recorded as usual.
func TestProxy_ForcesIdentityAcceptEncoding(t *testing.T) {
	t.Parallel()

	var gotEncoding string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":20}}`)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if gotEncoding != "identity" {
		t.Errorf("upstream Accept-Encoding = %q, want identity", gotEncoding)
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if int64Val(row.InputTokens) != 10 || int64Val(row.OutputTokens) != 20 {
		t.Errorf("tokens = in=%v out=%v, want 10/20 (plaintext response parsed)", row.InputTokens, row.OutputTokens)
	}
}

// TestProxy_GzippedUpstreamPassesBytesThrough covers the misbehaving
// upstream that ignores the identity request and gzips anyway: the client
// still gets the exact upstream bytes and Content-Encoding header (byte
// passthrough is unchanged), and token fields are NULL rather than parsed
// garbage -- with no crash.
func TestProxy_GzippedUpstreamPassesBytesThrough(t *testing.T) {
	t.Parallel()

	var gzBody bytes.Buffer
	zw := gzip.NewWriter(&gzBody)
	if _, err := zw.Write([]byte(`{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":20}}`)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	gzBytes := gzBody.Bytes()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gzBytes)
	}))
	defer upstream.Close()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(testHTTPClient(t, upstream)))
	t.Cleanup(p.Close)
	addAPIKeyAccount(t, reg, providerAnthropic, "a1", "sk-ant-real")

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	w := httptest.NewRecorder()
	p.handleAnthropic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("client Content-Encoding = %q, want gzip (passthrough)", w.Header().Get("Content-Encoding"))
	}
	if !bytes.Equal(w.Body.Bytes(), gzBytes) {
		t.Errorf("client body != upstream gzip bytes (len got=%d want=%d)", w.Body.Len(), len(gzBytes))
	}

	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome != "" })
	if row.InputTokens != nil || row.OutputTokens != nil {
		t.Errorf("tokens = in=%v out=%v, want nil (compressed body is not parsed)", row.InputTokens, row.OutputTokens)
	}
	if row.Outcome != store.RequestLogOutcomeOK {
		t.Errorf("Outcome = %q, want ok (the response itself is fine)", row.Outcome)
	}
}
