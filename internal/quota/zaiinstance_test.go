package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const zaiInstanceKey = "zai-sentinel-key-456"

type fakeInstanceCreds struct {
	url, key string
	err      error
}

func (c fakeInstanceCreds) Creds(string) (string, string, error) { return c.url, c.key, c.err }

func zaiInstanceServer(t *testing.T, status int) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	t.Helper()
	body := zaiFixture(t)
	var hits atomic.Int32
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &auth
}

func TestZaiInstanceFetcher_Success(t *testing.T) {
	srv, hits, auth := zaiInstanceServer(t, http.StatusOK)
	f := NewZaiInstanceFetcher(fakeInstanceCreds{url: srv.URL, key: zaiInstanceKey}, nil)
	report, err := f.Fetch(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if report.Status != StatusAvailable || len(report.Windows) != 2 {
		t.Fatalf("status=%q windows=%d", report.Status, len(report.Windows))
	}
	if got, _ := auth.Load().(string); got != zaiInstanceKey {
		t.Fatalf("Authorization = %q", got)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestZaiInstanceFetcher_NoKey(t *testing.T) {
	srv, hits, _ := zaiInstanceServer(t, http.StatusOK)
	creds := fakeInstanceCreds{url: srv.URL, err: fmt.Errorf("inst-1: %w", ErrInstanceNoKey)}
	report, err := NewZaiInstanceFetcher(creds, nil).Fetch(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if report.Status != StatusUnavailable || report.Problem == nil ||
		report.Problem.Kind != ProblemNoQuota || report.Problem.Detail != "no API key set" {
		t.Fatalf("report = %+v", report)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestZaiInstanceFetcher_Unsupported(t *testing.T) {
	srv, hits, _ := zaiInstanceServer(t, http.StatusOK)
	creds := fakeInstanceCreds{url: srv.URL, err: ErrInstanceUnsupported}
	report, err := NewZaiInstanceFetcher(creds, nil).Fetch(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if report.Status != StatusUnavailable || report.Problem == nil ||
		report.Problem.Kind != ProblemNoQuota || report.Problem.Detail != detailUnsupport {
		t.Fatalf("report = %+v", report)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestZaiInstanceFetcher_CredsErrorIsFixedVocabulary(t *testing.T) {
	srv, hits, _ := zaiInstanceServer(t, http.StatusOK)
	creds := fakeInstanceCreds{url: srv.URL, err: errors.New("boom " + zaiInstanceKey)}
	report, err := NewZaiInstanceFetcher(creds, nil).Fetch(context.Background(), "inst-1")
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("err = %v, want *FetchError", err)
	}
	if report.Status != StatusError || report.Error != "error" {
		t.Fatalf("report = %+v", report)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(err.Error(), zaiInstanceKey) || strings.Contains(string(raw), zaiInstanceKey) {
		t.Fatal("sentinel leaked")
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestZaiInstanceFetcher_Rejected401(t *testing.T) {
	srv, _, _ := zaiInstanceServer(t, http.StatusUnauthorized)
	f := NewZaiInstanceFetcher(fakeInstanceCreds{url: srv.URL, key: zaiInstanceKey}, nil)
	report, err := f.Fetch(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if report.Status != StatusUnavailable || report.Problem == nil || report.Problem.Kind != ProblemRejected {
		t.Fatalf("report = %+v", report)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), zaiInstanceKey) {
		t.Fatal("sentinel leaked")
	}
}
