package quota

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "Bearer test-token-abc"

func testAuthHeader() http.Header {
	return http.Header{"Authorization": []string{testToken}}
}

func assertNoLeak(t *testing.T, ts *httptest.Server, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "test-token-abc") {
		t.Errorf("error %q contains the Authorization value", msg)
	}
	if strings.Contains(msg, ts.URL) || strings.Contains(msg, strings.TrimPrefix(ts.URL, "http://")) {
		t.Errorf("error %q contains the server host", msg)
	}
}

func TestGetBodyStatusMapping(t *testing.T) {
	t.Parallel()

	mib := 1 << 20
	tests := []struct {
		name       string
		status     int
		header     http.Header
		body       []byte
		wantBody   []byte
		rejected   int
		wantErrMsg string
		wantStatus int
		wantRetry  time.Duration
	}{
		{
			name:     "200 returns body",
			status:   200,
			body:     []byte(`{"ok":true}`),
			wantBody: []byte(`{"ok":true}`),
		},
		{
			name:     "401 rejected",
			status:   401,
			rejected: 401,
		},
		{
			name:     "403 rejected",
			status:   403,
			rejected: 403,
		},
		{
			name:       "429 rate limited with retry-after",
			status:     429,
			header:     http.Header{"Retry-After": []string{"120"}},
			wantErrMsg: "rate limited",
			wantStatus: 429,
			wantRetry:  2 * time.Minute,
		},
		{
			name:       "500",
			status:     500,
			wantErrMsg: "http 500",
			wantStatus: 500,
		},
		{
			name:       "503",
			status:     503,
			wantErrMsg: "http 503",
			wantStatus: 503,
		},
		{
			name:     "1 MiB exactly is fine",
			status:   200,
			body:     make([]byte, mib),
			wantBody: make([]byte, mib),
		},
		{
			name:       "1 MiB plus one is too large",
			status:     200,
			body:       make([]byte, mib+1),
			wantErrMsg: "response too large",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, vs := range tt.header {
					for _, v := range vs {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write(tt.body)
			}))
			defer ts.Close()

			client := newUsageClient(&http.Client{})
			body, rejected, err := getBody(context.Background(), client, ts.URL+"/usage", testAuthHeader(), time.Now)
			assertNoLeak(t, ts, err)

			if tt.rejected != 0 {
				if rejected != tt.rejected || body != nil || err != nil {
					t.Fatalf("got body=%v rejected=%d err=%v, want rejected=%d nil nil", body, rejected, err, tt.rejected)
				}
				return
			}
			if tt.wantErrMsg != "" {
				var fe *FetchError
				if !errors.As(err, &fe) {
					t.Fatalf("err = %v, want *FetchError", err)
				}
				if fe.Error() != tt.wantErrMsg {
					t.Errorf("msg = %q, want %q", fe.Error(), tt.wantErrMsg)
				}
				if fe.Status != tt.wantStatus {
					t.Errorf("Status = %d, want %d", fe.Status, tt.wantStatus)
				}
				if fe.RetryAfter != tt.wantRetry {
					t.Errorf("RetryAfter = %v, want %v", fe.RetryAfter, tt.wantRetry)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(body) != string(tt.wantBody) {
				t.Errorf("body length = %d, want %d", len(body), len(tt.wantBody))
			}
		})
	}
}

func TestGetBodyRedirectNotFollowed(t *testing.T) {
	t.Parallel()

	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer target.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer ts.Close()

	client := newUsageClient(&http.Client{})
	_, _, err := getBody(context.Background(), client, ts.URL+"/usage", testAuthHeader(), time.Now)
	assertNoLeak(t, ts, err)
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Error() != "http 302" {
		t.Fatalf("err = %v, want FetchError \"http 302\"", err)
	}
	if fe.Status != 302 {
		t.Errorf("Status = %d, want 302", fe.Status)
	}
	if hits != 0 {
		t.Errorf("redirect target saw %d requests, want 0", hits)
	}
}

func TestGetBodyTimeout(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte("{}"))
	}))
	defer ts.Close()

	client := newUsageClient(&http.Client{})
	client.Timeout = 50 * time.Millisecond
	_, _, err := getBody(context.Background(), client, ts.URL+"/usage", testAuthHeader(), time.Now)
	assertNoLeak(t, ts, err)
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Error() != "timeout" {
		t.Fatalf("err = %v, want FetchError \"timeout\"", err)
	}
}

func TestGetBodyClosedConnection(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("recorder cannot hijack")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer ts.Close()

	client := newUsageClient(&http.Client{})
	_, _, err := getBody(context.Background(), client, ts.URL+"/usage", testAuthHeader(), time.Now)
	assertNoLeak(t, ts, err)
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Error() != "network error" {
		t.Fatalf("err = %v, want FetchError \"network error\"", err)
	}
}

func TestGetBodyCancelledContext(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()

	client := newUsageClient(&http.Client{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, _, err := getBody(ctx, client, ts.URL+"/usage", testAuthHeader(), time.Now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	assertNoLeak(t, ts, err)
}
