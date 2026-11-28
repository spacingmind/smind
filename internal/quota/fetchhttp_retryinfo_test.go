package quota

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const retryInfo90 = `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"90s"}]}}`

func get429(t *testing.T, header, body string) (time.Duration, int32) {
	t.Helper()
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if header != "" {
			w.Header().Set("Retry-After", header)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()
	client := newUsageClient(&http.Client{})
	_, _, err := getBody(context.Background(), client, ts.URL+"/usage", testAuthHeader(), time.Now)
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want FetchError", err)
	}
	return fe.RetryAfter, hits.Load()
}

func Test_usage_429_retryinfo_body_sets_the_wait(t *testing.T) {
	if got, _ := get429(t, "", retryInfo90); got != 90*time.Second {
		t.Fatalf("RetryAfter = %v, want 90s", got)
	}
}

func Test_usage_429_retry_after_beats_retryinfo(t *testing.T) {
	if got, _ := get429(t, "30", retryInfo90); got != 30*time.Second {
		t.Fatalf("RetryAfter = %v, want 30s", got)
	}
}

func Test_usage_429_body_read_is_capped_and_one_call(t *testing.T) {
	// Valid JSON whose RetryInfo sits after 12 KiB of padding: only a read
	// past the 8 KiB cap would find it.
	big := `{"pad":"` + strings.Repeat("x", 12<<10) + `","error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"90s"}]}}`
	got, hits := get429(t, "", big)
	if got != 0 {
		t.Errorf("RetryAfter = %v, want 0 (RetryInfo is past the read cap)", got)
	}
	if hits != 1 {
		t.Errorf("big body: %d requests, want 1", hits)
	}
	got, hits = get429(t, "", retryInfo90)
	if got != 90*time.Second || hits != 1 {
		t.Errorf("small body: RetryAfter=%v requests=%d, want 90s/1", got, hits)
	}
}
