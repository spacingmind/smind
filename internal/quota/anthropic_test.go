package quota

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// anthropicToken is an obviously fake token. Nothing the source returns may
// ever contain it.
const anthropicToken = "test-token-abc"

var anthropicNow = time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)

// anthropicFixture reads a testdata file.
func anthropicFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// anthropicSourceFor points a source at an httptest server serving handler.
func anthropicSourceFor(t *testing.T, handler http.HandlerFunc) (*anthropicSource, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	src := newAnthropicSource(server.Client())
	src.url = server.URL
	src.now = func() time.Time { return anthropicNow }
	return src, server
}

// anthropicFetch runs one fetch against the fixture-named test server.
func anthropicFetchFixture(t *testing.T, name string, mutate func(*testing.T, http.Header)) (Report, error) {
	t.Helper()
	src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
		if mutate != nil {
			mutate(t, w.Header())
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicFixture(t, name))
	})
	return src.fetch(context.Background(), anthropicToken)
}

// anthropicWindowIDs lists the window ids in order.
func anthropicWindowIDs(r Report) []string {
	ids := make([]string, len(r.Windows))
	for i, w := range r.Windows {
		ids[i] = w.ID
	}
	return ids
}

func anthropicFindWindow(t *testing.T, r Report, id string) Window {
	t.Helper()
	for _, w := range r.Windows {
		if w.ID == id {
			return w
		}
	}
	t.Fatalf("no window %q in %v", id, anthropicWindowIDs(r))
	return Window{}
}

func TestAnthropicFull(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_full.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if r.Status != StatusAvailable || r.Version != ReportVersion {
		t.Fatalf("status/version = %s/%d", r.Status, r.Version)
	}
	if !r.FetchedAt.Equal(anthropicNow) {
		t.Fatalf("FetchedAt = %v, want %v", r.FetchedAt, anthropicNow)
	}
	if got := anthropicWindowIDs(r); len(got) != 3 || got[0] != "five_hour" || got[1] != "weekly" || got[2] != "weekly_model_opus" {
		t.Fatalf("window ids = %v", got)
	}
	if !anthropicFindWindow(t, r, "five_hour").Summary || !anthropicFindWindow(t, r, "weekly").Summary {
		t.Fatal("five_hour and weekly must be summary windows")
	}
	if anthropicFindWindow(t, r, "weekly_model_opus").Summary {
		t.Fatal("scoped window must not be a summary")
	}
	if opus := anthropicFindWindow(t, r, "weekly_model_opus"); opus.Scope == nil || opus.Scope.Kind != ScopeModel || opus.Scope.Name != "Opus" {
		t.Fatalf("opus scope = %+v", opus.Scope)
	}
	if r.OverageEnabled == nil || !*r.OverageEnabled {
		t.Fatalf("OverageEnabled = %v, want true", r.OverageEnabled)
	}
	if len(r.Details) != 1 || r.Details[0] != (Detail{ID: "extra_usage", Label: "Extra usage", Value: "Enabled"}) {
		t.Fatalf("details = %+v", r.Details)
	}
}

func TestAnthropicLimitsArray(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_limits_array.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	want := []string{"weekly", "weekly_model_fable", "weekly_surface_code", "weekly_model_fable-pro"}
	if got := anthropicWindowIDs(r); !slices.Equal(got, want) {
		t.Fatalf("window ids = %v, want %v (session/weekly_all skipped)", got, want)
	}
	if kind := anthropicFindWindow(t, r, "weekly_surface_code").Scope.Kind; kind != ScopeSurface {
		t.Fatalf("surface scope kind = %s", kind)
	}
	if kind := anthropicFindWindow(t, r, "weekly_model_fable").Scope.Kind; kind != ScopeModel {
		t.Fatalf("model scope kind = %s", kind)
	}
}

func TestAnthropicLegacyAndLimitsTwin(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_legacy_and_limits_twin.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	opusCount := 0
	for _, w := range r.Windows {
		if strings.HasPrefix(w.ID, "weekly_model_opus") {
			opusCount++
		}
	}
	if opusCount != 1 {
		t.Fatalf("got %d Opus windows, want exactly 1: %v", opusCount, anthropicWindowIDs(r))
	}
	opus := anthropicFindWindow(t, r, "weekly_model_opus")
	// limits[] carries the reset, its percent is null so the legacy value wins.
	if opus.UsedPct == nil || *opus.UsedPct != 8 {
		t.Fatalf("UsedPct = %v, want legacy 8 (nil falls back to twin)", opus.UsedPct)
	}
	if opus.ResetsAt == nil || !opus.ResetsAt.Equal(time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ResetsAt = %v, want limits[] 2026-06-09", opus.ResetsAt)
	}
}

func TestAnthropicOmeletteSurfaceVsModelStayApart(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_omelette_surface_vs_model.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := anthropicWindowIDs(r); len(got) != 2 || got[0] != "weekly_model_omelette" || got[1] != "weekly_surface_omelette" {
		t.Fatalf("window ids = %v", got)
	}
	model := anthropicFindWindow(t, r, "weekly_model_omelette")
	surface := anthropicFindWindow(t, r, "weekly_surface_omelette")
	if model.UsedPct == nil || *model.UsedPct != 12 || surface.UsedPct == nil || *surface.UsedPct != 30 {
		t.Fatalf("used = %v / %v, want 12 / 30", model.UsedPct, surface.UsedPct)
	}
	if surface.Scope == nil || surface.Scope.Kind != ScopeSurface {
		t.Fatalf("surface scope = %+v", surface.Scope)
	}
}

func TestAnthropicNullUtilization(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_null_utilization.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	fh := anthropicFindWindow(t, r, "five_hour")
	if fh.UsedPct != nil || fh.RemainingPct != nil || fh.ResetsAt != nil || fh.Tone != ToneDefault {
		t.Fatalf("five_hour = %+v, want nil usage fields and default tone", fh)
	}
}

func TestAnthropicNumericString(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_numeric_string.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	fh := anthropicFindWindow(t, r, "five_hour")
	if fh.UsedPct == nil || *fh.UsedPct != 41.5 {
		t.Fatalf("UsedPct = %v, want 41.5 from numeric string", fh.UsedPct)
	}
}

func TestAnthropicOneBadEntry(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_one_bad_entry.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := anthropicWindowIDs(r); len(got) != 3 || got[2] != "weekly_model_fable" {
		t.Fatalf("window ids = %v, want bad limits entry skipped, siblings kept", got)
	}
}

func TestAnthropicUnknownFields(t *testing.T) {
	t.Parallel()
	r, err := anthropicFetchFixture(t, "anthropic_unknown_fields.json", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	encoded, encErr := EncodeReport(r)
	if encErr != nil {
		t.Fatalf("encode: %v", encErr)
	}
	for _, leaked := range []string{"someone@example.test", "Spy Org", "secret-foo-value", "org-uuid"} {
		if strings.Contains(encoded, leaked) {
			t.Fatalf("report contains unknown field value %q: %s", leaked, encoded)
		}
	}
	if len(r.Windows) != 1 || r.Windows[0].ID != "five_hour" {
		t.Fatalf("windows = %v", anthropicWindowIDs(r))
	}
}

func anthropicExpectMalformed(t *testing.T, name string, body []byte) {
	t.Helper()
	src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	})
	r, err := src.fetch(context.Background(), anthropicToken)
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("%s: err = %v, want *FetchError", name, err)
	}
	if r.Status != StatusError || r.Error != errMalformed {
		t.Fatalf("%s: report = %+v", name, r)
	}
}

func TestAnthropicEmptyObject(t *testing.T) {
	t.Parallel()
	anthropicExpectMalformed(t, "empty", anthropicFixture(t, "anthropic_empty_object.json"))
}

func TestAnthropicTruncatedJSON(t *testing.T) {
	t.Parallel()
	anthropicExpectMalformed(t, "truncated", anthropicFixture(t, "anthropic_truncated.json"))
}

func TestAnthropicHTML(t *testing.T) {
	t.Parallel()
	src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(anthropicFixture(t, "anthropic.html"))
	})
	r, err := src.fetch(context.Background(), anthropicToken)
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("err = %v, want *FetchError", err)
	}
	if r.Status != StatusError || r.Error != errMalformed {
		t.Fatalf("report = %+v", r)
	}
	encoded, _ := EncodeReport(r)
	if strings.Contains(encoded, "oauth required") || strings.Contains(encoded, "Sign in") {
		t.Fatalf("body leaked into report: %s", encoded)
	}
}

func TestAnthropic401And403(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			requests := 0
			src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(status)
			})
			r, err := src.fetch(context.Background(), anthropicToken)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if requests != 1 {
				t.Fatalf("server saw %d requests, want exactly 1", requests)
			}
			if r.Status != StatusUnavailable || r.Problem == nil || r.Problem.Kind != ProblemRejected || r.Problem.Status != status || r.Problem.Detail != detailRejected {
				t.Fatalf("report = %+v, problem = %+v", r, r.Problem)
			}
		})
	}
}

func TestAnthropic429RetryAfter(t *testing.T) {
	t.Parallel()
	httpDate := anthropicNow.Add(3 * time.Minute).UTC().Format(http.TimeFormat)
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"anthropic_429_retry_after_seconds", "120", 120 * time.Second},
		{"anthropic_429_retry_after_http_date", httpDate, 3 * time.Minute},
		{"anthropic_429_retry_after_absent", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.value != "" {
					w.Header().Set("Retry-After", tc.value)
				}
				w.WriteHeader(http.StatusTooManyRequests)
			})
			r, err := src.fetch(context.Background(), anthropicToken)
			var fetchErr *FetchError
			if !errors.As(err, &fetchErr) {
				t.Fatalf("err = %v, want *FetchError", err)
			}
			if fetchErr.RetryAfter != tc.want {
				t.Fatalf("RetryAfter = %v, want %v", fetchErr.RetryAfter, tc.want)
			}
			if r.Status != StatusError || r.Error != errRateLimited {
				t.Fatalf("report = %+v", r)
			}
		})
	}
}

func TestAnthropic5xxTimeoutNetwork(t *testing.T) {
	t.Parallel()
	t.Run("5xx", func(t *testing.T) {
		t.Parallel()
		src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		r, err := src.fetch(context.Background(), anthropicToken)
		var fetchErr *FetchError
		if !errors.As(err, &fetchErr) || fetchErr.Status != 503 || r.Status != StatusError {
			t.Fatalf("5xx: err = %v report = %+v", err, r)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			w.Write(anthropicFixture(t, "anthropic_full.json"))
		})
		// newUsageClient would re-apply the 15 s timeout, so set the client
		// directly with a short one.
		src.client = &http.Client{Timeout: 50 * time.Millisecond}
		r, err := src.fetch(context.Background(), anthropicToken)
		var fetchErr *FetchError
		if !errors.As(err, &fetchErr) || fetchErr.msg != errTimeout || r.Status != StatusError {
			t.Fatalf("timeout: err = %v report = %+v", err, r)
		}
	})
	t.Run("closed connection", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("recorder cannot hijack")
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
		}))
		defer server.Close()
		src := newAnthropicSource(server.Client())
		src.url = server.URL
		src.now = func() time.Time { return anthropicNow }
		r, err := src.fetch(context.Background(), anthropicToken)
		var fetchErr *FetchError
		if !errors.As(err, &fetchErr) || fetchErr.msg != errNetwork || r.Status != StatusError {
			t.Fatalf("closed: err = %v report = %+v", err, r)
		}
	})
}

func TestAnthropicRedirect(t *testing.T) {
	t.Parallel()
	targetHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		w.Write(anthropicFixture(t, "anthropic_full.json"))
	}))
	defer target.Close()
	src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	})
	r, err := src.fetch(context.Background(), anthropicToken)
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("err = %v, want *FetchError", err)
	}
	if fetchErr.Status != http.StatusFound {
		t.Fatalf("status = %d, want the unfollowed 302", fetchErr.Status)
	}
	if targetHits != 0 {
		t.Fatalf("redirect target saw %d requests, want 0", targetHits)
	}
	if r.Status != StatusError {
		t.Fatalf("report = %+v", r)
	}
}

func TestAnthropicOversizeBody(t *testing.T) {
	t.Parallel()
	src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, maxBodyBytes+1024)
		body[0] = ' '
		w.Write(body)
	})
	r, err := src.fetch(context.Background(), anthropicToken)
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) || fetchErr.msg != errTooLarge || r.Status != StatusError {
		t.Fatalf("err = %v report = %+v", err, r)
	}
}

func TestAnthropicRequestShape(t *testing.T) {
	t.Parallel()
	var gotMethod, gotAuth, gotBeta, gotAccept, gotPath, gotQuery string
	src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write(anthropicFixture(t, "anthropic_full.json"))
	})
	if _, err := src.fetch(context.Background(), anthropicToken); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotAuth != "Bearer "+anthropicToken {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotBeta != anthropicOAuthBeta {
		t.Fatalf("anthropic-beta = %q", gotBeta)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotQuery != "" {
		t.Fatalf("query = %q, want none", gotQuery)
	}
	// The source's url is the server root: the request path is exactly what
	// that url carries, with no query appended.
	if gotPath != "/" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestAnthropicTokenNotLeaked(t *testing.T) {
	t.Parallel()
	handlers := map[string]http.HandlerFunc{
		"401":     func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"403":     func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"429":     func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		"5xx":     func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"html":    func(w http.ResponseWriter, r *http.Request) { w.Write(anthropicFixture(t, "anthropic.html")) },
		"garbage": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("not json at all")) },
		"truncated": func(w http.ResponseWriter, r *http.Request) {
			w.Write(anthropicFixture(t, "anthropic_truncated.json"))
		},
		"oversize": func(w http.ResponseWriter, r *http.Request) {
			w.Write(make([]byte, maxBodyBytes+1024))
		},
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src, _ := anthropicSourceFor(t, handler)
			r, err := src.fetch(context.Background(), anthropicToken)
			encoded, _ := EncodeReport(r)
			if strings.Contains(encoded, anthropicToken) {
				t.Fatalf("token leaked into report: %s", encoded)
			}
			if err != nil && strings.Contains(err.Error(), anthropicToken) {
				t.Fatalf("token leaked into error: %v", err)
			}
		})
	}
}

func TestAnthropicCtxCancel(t *testing.T) {
	t.Parallel()
	src, _ := anthropicSourceFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(anthropicFixture(t, "anthropic_full.json"))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := src.fetch(ctx, anthropicToken)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if r.Status != "" || r.Version != 0 || len(r.Windows) != 0 {
		t.Fatalf("report = %+v, want the zero Report", r)
	}
}
