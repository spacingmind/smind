package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// recordingUpstream records "<path>|<x-api-key or bearer>" per request.
type recordingUpstream struct {
	*httptest.Server
	mu      sync.Mutex
	hits    []string
	queries []string
}

func newRecordingUpstream(t *testing.T) *recordingUpstream {
	t.Helper()
	u := &recordingUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key == "" {
			key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		u.mu.Lock()
		u.hits = append(u.hits, r.URL.Path+"|"+key)
		u.queries = append(u.queries, r.URL.RawQuery)
		u.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *recordingUpstream) snapshot() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.hits...)
}

func TestProxy_BaseURLSemantics(t *testing.T) {
	t.Parallel()

	up := newRecordingUpstream(t)
	cases := []struct {
		name, provider, base, path, want string
		metered                          bool
	}{
		{"anthropic base + full path", providerAnthropic, up.URL + "/prefix", "/v1/messages", "/prefix/v1/messages|k", true},
		{"anthropic legacy full endpoint", providerAnthropic, up.URL + "/prefix/v1/messages", "/v1/messages", "/prefix/v1/messages|k", true},
		{"openai base with v1", providerOpenAI, up.URL + "/v1", "/v1/chat/completions", "/v1/chat/completions|k", true},
		{"openai legacy v1 endpoint", providerOpenAI, up.URL + "/v1/chat/completions", "/v1/chat/completions", "/v1/chat/completions|k", true},
		{"openai perplexity-style legacy", providerOpenAI, up.URL + "/chat/completions", "/v1/chat/completions", "/chat/completions|k", true},
		{"count_tokens passthrough unmetered", providerAnthropic, up.URL, "/v1/messages/count_tokens", "/v1/messages/count_tokens|k", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStoreForProxy(t)
			reg, router := newTestRouting(t, s)
			p := newProxy(reg, router, s,
				withAnthropicHTTPClient(&http.Client{}), withOpenAIHTTPClient(&http.Client{}))
			if _, err := reg.AddAPIKeyWithBaseURL(tc.provider, "a", "k", tc.base); err != nil {
				t.Fatal(err)
			}
			before := len(up.snapshot())

			h := p.handleAnthropic
			switch {
			case tc.provider == providerOpenAI:
				h = p.handleOpenAI
			case strings.HasSuffix(tc.path, "/count_tokens"):
				h = p.handleAnthropicCountTokens
			}
			w := httptest.NewRecorder()
			h(w, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"model":"m"}`)))
			if w.Code != 200 {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			hits := up.snapshot()[before:]
			if len(hits) != 1 || hits[0] != tc.want {
				t.Fatalf("upstream hits = %v, want [%s]", hits, tc.want)
			}

			p.Close() // drains the async writer
			rows, err := s.ListRequestLogs(store.RequestLogFilter{})
			if err != nil {
				t.Fatal(err)
			}
			wantRows := 0
			if tc.metered {
				wantRows = 1
			}
			if len(rows) != wantRows {
				t.Fatalf("request_log rows = %d, want %d", len(rows), wantRows)
			}
		})
	}
}

func TestProxy_ModelAwareRouting(t *testing.T) {
	t.Parallel()

	up := newRecordingUpstream(t)
	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(&http.Client{}))
	t.Cleanup(p.Close)

	add := func(label, key string, models []string) int64 {
		a, err := reg.AddWithCredentialModels(providerAnthropic, label, key, up.URL, models)
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	send := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		p.handleAnthropic(w, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
		return w
	}
	lastKey := func() string {
		h := up.snapshot()
		return h[len(h)-1][strings.Index(h[len(h)-1], "|")+1:]
	}

	// Only a glm-* account exists: claude-* has no candidates -> 400, and
	// the glm key is never used.
	add("glm", "k-glm", []string{"glm-*"})
	w := send(`{"model":"claude-opus-4"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not_found_error") {
		t.Fatalf("claude-* with only glm account: status=%d body=%s", w.Code, w.Body.String())
	}
	if len(up.snapshot()) != 0 {
		t.Fatalf("glm key reached upstream for claude-*: %v", up.snapshot())
	}
	row := waitForRequestLog(t, s, func(r store.RequestLog) bool { return r.Outcome == store.RequestLogOutcomeRouteError })
	if row.Status != http.StatusBadRequest || row.AccountID != nil {
		t.Fatalf("route_error row = %+v", row)
	}

	// Explicit match wins over a no-list account.
	add("any", "k-any", nil)
	if w := send(`{"model":"glm-4.6"}`); w.Code != 200 || lastKey() != "k-glm" {
		t.Fatalf("glm-4.6: status=%d key=%q", w.Code, lastKey())
	}
	// No explicit match -> the no-list account; never the glm one.
	for i := 0; i < 3; i++ {
		if w := send(`{"model":"claude-opus-4"}`); w.Code != 200 || lastKey() != "k-any" {
			t.Fatalf("claude-opus-4: status=%d key=%q", w.Code, lastKey())
		}
	}
	// A parseable body with no model only matches no-list accounts.
	for _, body := range []string{`{}`} {
		if w := send(body); w.Code != 200 || lastKey() != "k-any" {
			t.Fatalf("body %q: status=%d key=%q", body, w.Code, lastKey())
		}
	}

	// OpenAI-shaped model_not_found, and count_tokens is unmetered even on
	// a routing failure.
	w = httptest.NewRecorder()
	p.handleOpenAI(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"x"}`)))
	if w.Code != http.StatusServiceUnavailable { // no openai accounts at all
		t.Fatalf("openai no accounts: status=%d", w.Code)
	}
	if _, err := reg.AddWithCredentialModels(providerOpenAI, "o", "ko", up.URL+"/v1", []string{"gpt-*"}); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	p.handleOpenAI(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"x"}`)))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "model_not_found") {
		t.Fatalf("openai unknown model: status=%d body=%s", w.Code, w.Body.String())
	}
}

// When the model can't be determined (oversized or non-JSON body), routing
// fails closed per ADR-0020 §5(b): only no-model-list accounts may serve
// the request; a glm-only setup never receives a claude-* request it can't
// serve. When the model can be recovered from the buffered prefix of an
// oversized body, normal narrowing applies.
func TestProxy_UndeterminableModelFailsClosed(t *testing.T) {
	t.Parallel()

	pad := bytes.Repeat([]byte("x"), requestBodyParseCap+64)
	// model FIRST: recoverable from the buffered prefix even though the
	// body is oversized.
	modelFirst := append(append([]byte(`{"model":"claude-opus-4","pad":"`), pad...), []byte(`"}`)...)
	// model AFTER a >5MiB pad: not recoverable from any bounded prefix.
	modelLast := append(append([]byte(`{"pad":"`), pad...), []byte(`","model":"claude-opus-4"}`)...)

	cases := []struct {
		name     string
		body     []byte
		glmList  bool // add the glm-* listed account
		addCld   bool // add a claude-* listed account
		addNone  bool // add a no-list account
		wantSts  int
		wantHits []string // expected upstream hits (paths|keys), empty for none
	}{
		{
			name:     "oversized, model first, only glm account",
			body:     modelFirst,
			glmList:  true,
			wantSts:  http.StatusBadRequest,
			wantHits: nil,
		},
		{
			name:     "oversized, model first, glm + claude accounts",
			body:     modelFirst,
			glmList:  true,
			addCld:   true,
			wantSts:  http.StatusOK,
			wantHits: []string{"/v1/messages|k-claude"},
		},
		{
			name:     "oversized, model after pad, glm + no-list accounts",
			body:     modelLast,
			glmList:  true,
			addNone:  true,
			wantSts:  http.StatusOK,
			wantHits: []string{"/v1/messages|k-any"},
		},
		{
			name:     "oversized, model after pad, only glm account",
			body:     modelLast,
			glmList:  true,
			wantSts:  http.StatusBadRequest,
			wantHits: nil,
		},
		{
			name:     "non-JSON body, only glm account",
			body:     []byte("not json"),
			glmList:  true,
			wantSts:  http.StatusBadRequest,
			wantHits: nil,
		},
		{
			name:     "non-JSON body, glm + no-list accounts",
			body:     []byte("not json"),
			glmList:  true,
			addNone:  true,
			wantSts:  http.StatusOK,
			wantHits: []string{"/v1/messages|k-any"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := newRecordingUpstream(t)
			s := newTestStoreForProxy(t)
			reg, router := newTestRouting(t, s)
			p := newProxy(reg, router, s, withAnthropicHTTPClient(&http.Client{}))
			t.Cleanup(p.Close)
			if tc.addCld {
				if _, err := reg.AddWithCredentialModels(providerAnthropic, "claude", "k-claude", up.URL, []string{"claude-*"}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.addNone {
				if _, err := reg.AddWithCredentialModels(providerAnthropic, "any", "k-any", up.URL, nil); err != nil {
					t.Fatal(err)
				}
			}
			if tc.glmList {
				if _, err := reg.AddWithCredentialModels(providerAnthropic, "glm", "k-glm", up.URL, []string{"glm-*"}); err != nil {
					t.Fatal(err)
				}
			}

			w := httptest.NewRecorder()
			p.handleAnthropic(w, httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(tc.body)))
			if w.Code != tc.wantSts {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantSts, w.Body.String())
			}
			if tc.wantSts == http.StatusBadRequest && !strings.Contains(w.Body.String(), "not_found_error") {
				t.Fatalf("400 body = %s", w.Body.String())
			}
			if got := up.snapshot(); !reflect.DeepEqual(got, tc.wantHits) {
				t.Fatalf("upstream hits = %v, want %v", got, tc.wantHits)
			}
		})
	}
}

// The full oversized body reaches upstream byte-for-byte: the prefix
// readRequestBody buffered for model extraction plus the live remainder.
func TestProxy_UndeterminableModelForwardsFullBody(t *testing.T) {
	t.Parallel()

	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(&http.Client{}))
	t.Cleanup(p.Close)

	var gotLen int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		atomic.StoreInt64(&gotLen, n)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(up.Close)
	if _, err := reg.AddWithCredentialModels(providerAnthropic, "claude", "k-claude", up.URL, []string{"claude-*"}); err != nil {
		t.Fatal(err)
	}

	body := append(append([]byte(`{"model":"claude-opus-4","pad":"`), bytes.Repeat([]byte("x"), requestBodyParseCap+1024)...), []byte(`"}`)...)
	w := httptest.NewRecorder()
	p.handleAnthropic(w, httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if n := atomic.LoadInt64(&gotLen); n != int64(len(body)) {
		t.Fatalf("upstream received %d bytes, want %d", n, len(body))
	}
}

// The incoming query string (Claude Code sends ?beta=true) reaches upstream,
// for both /v1/messages and count_tokens.
func TestProxy_PreservesQueryString(t *testing.T) {
	t.Parallel()

	up := newRecordingUpstream(t)
	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(&http.Client{}))
	t.Cleanup(p.Close)
	if _, err := reg.AddAPIKeyWithBaseURL(providerAnthropic, "a", "k", up.URL); err != nil {
		t.Fatal(err)
	}
	for path, h := range map[string]http.HandlerFunc{
		"/v1/messages":              p.handleAnthropic,
		"/v1/messages/count_tokens": p.handleAnthropicCountTokens,
	} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodPost, path+"?beta=true", strings.NewReader(`{"model":"m"}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, w.Code)
		}
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.queries) != 2 || up.queries[0] != "beta=true" || up.queries[1] != "beta=true" {
		t.Fatalf("upstream queries = %v, want [beta=true beta=true]", up.queries)
	}
}

// count_tokens is unmetered on routing-failure paths too: no request_log
// row for a 503 (no accounts) or a 400 (model_not_found).
func TestProxy_CountTokensUnmeteredOnRouteFailure(t *testing.T) {
	t.Parallel()

	up := newRecordingUpstream(t)
	s := newTestStoreForProxy(t)
	reg, router := newTestRouting(t, s)
	p := newProxy(reg, router, s, withAnthropicHTTPClient(&http.Client{}))

	send := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		p.handleAnthropicCountTokens(w, httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-opus-4"}`)))
		return w
	}
	if w := send(); w.Code != http.StatusServiceUnavailable { // no accounts
		t.Fatalf("no accounts: status = %d", w.Code)
	}
	if _, err := reg.AddWithCredentialModels(providerAnthropic, "glm", "k-glm", up.URL, []string{"glm-*"}); err != nil {
		t.Fatal(err)
	}
	if w := send(); w.Code != http.StatusBadRequest { // model_not_found
		t.Fatalf("unknown model: status = %d", w.Code)
	}

	p.Close() // drains the async writer
	rows, err := s.ListRequestLogs(store.RequestLogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("request_log rows = %d, want 0: %+v", len(rows), rows)
	}
	if len(up.snapshot()) != 0 {
		t.Fatalf("upstream reached on route failure: %v", up.snapshot())
	}
}
