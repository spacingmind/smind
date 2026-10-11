package quota

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openaiFixture reads a testdata fixture written by openaiWriteFixture.
func openaiFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

var openaiNow = time.Date(2025, 10, 9, 12, 0, 0, 0, time.UTC)

// openaiTestSource returns the source pointed at a server serving handler,
// plus the recorded requests.
func openaiTestSource(t *testing.T, handler http.HandlerFunc) (*openaiSource, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	src := newOpenAISource(srv.Client())
	src.url = srv.URL
	src.now = func() time.Time { return openaiNow }
	return src, &got
}

func openaiJSONHandler(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

func openaiFindWindow(t *testing.T, r Report, id string) Window {
	t.Helper()
	for _, w := range r.Windows {
		if w.ID == id {
			return w
		}
	}
	t.Fatalf("window %q not found in %+v", id, r.Windows)
	return Window{}
}

func openaiFloat(f float64) *float64 { return &f }

func openaiTime(t time.Time) *time.Time { return &t }

// openaiTestJWT builds an unsecured JWT with the given JSON claims payload.
func openaiTestJWT(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + enc([]byte(claims)) + "." + enc([]byte("sig"))
}

func TestOpenai_full(t *testing.T) {
	t.Parallel()

	src, reqs := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_full.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if r.Version != ReportVersion || r.Status != StatusAvailable {
		t.Fatalf("status = %v/%v, want available v%d", r.Status, r.Version, ReportVersion)
	}
	if !r.FetchedAt.Equal(openaiNow) {
		t.Fatalf("FetchedAt = %v, want %v", r.FetchedAt, openaiNow)
	}
	if r.PlanLabel != "plus" {
		t.Errorf("PlanLabel = %q, want plus", r.PlanLabel)
	}

	fh := openaiFindWindow(t, r, "five_hour")
	if !fh.Summary || fh.Scope != nil {
		t.Errorf("five_hour Summary=%v Scope=%v, want summary account window", fh.Summary, fh.Scope)
	}
	if fh.UsedPct == nil || *fh.UsedPct != 31.5 {
		t.Errorf("five_hour UsedPct = %v, want 31.5", fh.UsedPct)
	}
	if fh.RemainingPct == nil || *fh.RemainingPct != 68.5 {
		t.Errorf("five_hour RemainingPct = %v, want 68.5", fh.RemainingPct)
	}
	if fh.Tone != ToneOK {
		t.Errorf("five_hour Tone = %v, want ok", fh.Tone)
	}
	if fh.Label != "Session" || fh.ShortLabel != "5h" {
		t.Errorf("five_hour labels = %q/%q, want Session/5h", fh.Label, fh.ShortLabel)
	}
	if fh.DurationSeconds == nil || *fh.DurationSeconds != 18000 {
		t.Errorf("five_hour DurationSeconds = %v, want 18000", fh.DurationSeconds)
	}
	wantReset := time.Unix(1760000100, 0).UTC()
	if fh.ResetsAt == nil || !fh.ResetsAt.Equal(wantReset) {
		t.Errorf("five_hour ResetsAt = %v, want %v", fh.ResetsAt, wantReset)
	}

	wk := openaiFindWindow(t, r, "weekly")
	if !wk.Summary || wk.Scope != nil {
		t.Errorf("weekly Summary=%v Scope=%v, want summary account window", wk.Summary, wk.Scope)
	}
	if wk.UsedPct == nil || *wk.UsedPct != 12 {
		t.Errorf("weekly UsedPct = %v, want 12", wk.UsedPct)
	}

	if len(r.Windows) != 2 {
		t.Errorf("windows = %d, want 2", len(r.Windows))
	}
	if len(r.Balances) != 1 {
		t.Fatalf("balances = %d, want 1", len(r.Balances))
	}
	b := r.Balances[0]
	if b.ID != "credits" || b.Label != "Credits" || b.Unit != "credits" {
		t.Errorf("balance = %+v, want credits/Credits/credits", b)
	}
	if b.Remaining == nil || *b.Remaining != 1200 {
		t.Errorf("balance Remaining = %v, want 1200", b.Remaining)
	}
	if len(*reqs) != 1 {
		t.Errorf("requests = %d, want 1", len(*reqs))
	}
}

func TestOpenai_unknown_duration(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_unknown_duration.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}

	p := openaiFindWindow(t, r, "unknown_primary")
	if p.Label != "Primary limit" || p.ShortLabel != "" {
		t.Errorf("unknown_primary labels = %q/%q, want Primary limit/empty", p.Label, p.ShortLabel)
	}
	if p.DurationSeconds != nil {
		t.Errorf("unknown_primary DurationSeconds = %v, want nil", p.DurationSeconds)
	}
	s := openaiFindWindow(t, r, "unknown_secondary")
	if s.Label != "Secondary limit" || s.ShortLabel != "" {
		t.Errorf("unknown_secondary labels = %q/%q, want Secondary limit/empty", s.Label, s.ShortLabel)
	}
}

func TestOpenai_other_duration(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_other_duration.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}

	w := openaiFindWindow(t, r, "86400s")
	if w.Label != "1-day" || w.ShortLabel != "1d" {
		t.Errorf("86400s labels = %q/%q, want 1-day/1d", w.Label, w.ShortLabel)
	}
	if w.DurationSeconds == nil || *w.DurationSeconds != 86400 {
		t.Errorf("86400s DurationSeconds = %v, want 86400", w.DurationSeconds)
	}
}

func TestOpenai_epoch_reset(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_epoch_reset.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}

	wantNum := time.Unix(1760000000, 0).UTC()
	if got := openaiFindWindow(t, r, "five_hour"); got.ResetsAt == nil || !got.ResetsAt.Equal(wantNum) {
		t.Errorf("numeric reset_at = %v, want %v", got.ResetsAt, wantNum)
	}
	wantStr := time.Unix(1760000123, 0).UTC()
	if got := openaiFindWindow(t, r, "weekly"); got.ResetsAt == nil || !got.ResetsAt.Equal(wantStr) {
		t.Errorf("string reset_at = %v, want %v", got.ResetsAt, wantStr)
	}
}

func TestOpenai_additional_limits(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_additional_limits.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}

	// named (limit_name only)
	w := openaiFindWindow(t, r, "limit:Cloud%20mode:five_hour")
	if w.Scope == nil || w.Scope.Kind != ScopeFeature || w.Scope.Name != "Cloud mode" {
		t.Errorf("Cloud mode scope = %+v, want feature/Cloud mode", w.Scope)
	}
	if w.Label != "Cloud mode · Session" || w.ShortLabel != "Cloud mode 5h" {
		t.Errorf("Cloud mode labels = %q/%q", w.Label, w.ShortLabel)
	}
	if w.Summary {
		t.Errorf("Cloud mode window is summary, want not")
	}

	// unnamed entry (neither metered_feature nor limit_name) is skipped
	for _, win := range r.Windows {
		if strings.HasPrefix(win.ID, "limit:") && !strings.HasPrefix(win.ID, "limit:Cloud") && !strings.HasPrefix(win.ID, "limit:image") {
			t.Errorf("unnamed limit produced window %q", win.ID)
		}
	}

	// metered_feature with a space and slash is percent-escaped (JS
	// encodeURIComponent semantics: space %20, slash %2F)
	w = openaiFindWindow(t, r, "limit:image%20gen%2Fadvanced:five_hour")
	if w.Scope == nil || w.Scope.Name != "Images" {
		t.Errorf("Images scope = %+v, want name Images", w.Scope)
	}

	if len(r.Windows) != 3 { // account primary + cloud mode + images
		t.Errorf("windows = %d (%+v), want 3", len(r.Windows), r.Windows)
	}
}

func TestOpenai_code_review(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_code_review.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}

	w := openaiFindWindow(t, r, "code_review:weekly")
	if w.Scope == nil || w.Scope.Kind != ScopeFeature || w.Scope.ID != "code_review" || w.Scope.Name != "Code review" {
		t.Fatalf("code review scope = %+v", w.Scope)
	}
	if w.Label != "Code review · Weekly" || w.ShortLabel != "Code review wk" {
		t.Errorf("code review labels = %q/%q", w.Label, w.ShortLabel)
	}
}

func TestOpenai_credits_string(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_credits_string.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}

	if len(r.Balances) != 1 {
		t.Fatalf("balances = %d, want 1", len(r.Balances))
	}
	if got := r.Balances[0].Remaining; got == nil || *got != 12.5 {
		t.Errorf("credits Remaining = %v, want 12.5", got)
	}
}

func TestOpenai_no_rate_limit(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_no_rate_limit.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err == nil {
		t.Fatalf("fetch() error = nil, want malformed")
	}
	var fe *FetchError
	if !errors.As(err, &fe) || fe.msg != errMalformed {
		t.Errorf("err = %v, want malformed FetchError", err)
	}
	if r.Status != StatusError || r.Error != errMalformed {
		t.Errorf("report = %v/%q, want error/malformed", r.Status, r.Error)
	}
}

func TestOpenai_one_bad_entry(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_one_bad_entry.json")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}

	openaiFindWindow(t, r, "five_hour")             // good primary survives
	openaiFindWindow(t, r, "limit:Whole:five_hour") // good additional survives
	for _, w := range r.Windows {                   // bad secondary and bad additional dropped
		if w.ID == "unknown_secondary" || w.ID == "limit:Broken:five_hour" {
			t.Errorf("malformed entry produced window %q", w.ID)
		}
	}
}

func TestOpenai_html(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_html.html")))
	r, err := src.fetch(context.Background(), "test-token-abc")
	var fe *FetchError
	if !errors.As(err, &fe) || fe.msg != errMalformed {
		t.Fatalf("err = %v, want malformed", err)
	}
	if r.Status != StatusError || r.Error != errMalformed {
		t.Errorf("report = %v/%q, want error/malformed", r.Status, r.Error)
	}
}

func TestOpenai_truncated_json(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_truncated.json")))
	_, err := src.fetch(context.Background(), "test-token-abc")
	var fe *FetchError
	if !errors.As(err, &fe) || fe.msg != errMalformed {
		t.Fatalf("err = %v, want malformed", err)
	}
}

func TestOpenai_401(t *testing.T) {
	t.Parallel()

	src, reqs := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v, want nil", err)
	}
	if r.Status != StatusUnavailable || r.Problem == nil {
		t.Fatalf("report = %+v, want unavailable with problem", r)
	}
	if r.Problem.Kind != ProblemRejected || r.Problem.Status != 401 || r.Problem.Detail != detailRejected {
		t.Errorf("problem = %+v, want rejected/401", r.Problem)
	}
	if len(*reqs) != 1 {
		t.Errorf("requests = %d, want exactly 1 (no retry)", len(*reqs))
	}
}

func TestOpenai_403(t *testing.T) {
	t.Parallel()

	src, reqs := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v, want nil", err)
	}
	if r.Status != StatusUnavailable || r.Problem == nil || r.Problem.Kind != ProblemRejected || r.Problem.Status != 403 {
		t.Fatalf("report = %+v, want unavailable rejected 403", r)
	}
	if len(*reqs) != 1 {
		t.Errorf("requests = %d, want exactly 1 (no retry)", len(*reqs))
	}
}

func TestOpenai_429(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		retryAfter string
		want       time.Duration
	}{
		{name: "seconds", retryAfter: "30", want: 30 * time.Second},
		{name: "http-date", retryAfter: openaiNow.Add(2 * time.Minute).Format(http.TimeFormat), want: 2 * time.Minute},
		{name: "absent", retryAfter: "", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src, _ := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(http.StatusTooManyRequests)
			})
			r, err := src.fetch(context.Background(), "test-token-abc")
			var fe *FetchError
			if !errors.As(err, &fe) {
				t.Fatalf("err = %v, want *FetchError", err)
			}
			if fe.Status != 429 {
				t.Errorf("status = %d, want 429", fe.Status)
			}
			if fe.RetryAfter != tt.want {
				t.Errorf("RetryAfter = %v, want %v", fe.RetryAfter, tt.want)
			}
			if r.Status != StatusError || r.Error != errRateLimited {
				t.Errorf("report = %v/%q, want error/rate limited", r.Status, r.Error)
			}
		})
	}
}

func TestOpenai_5xx_timeout_network(t *testing.T) {
	t.Parallel()

	t.Run("503", func(t *testing.T) {
		t.Parallel()
		src, _ := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		r, err := src.fetch(context.Background(), "test-token-abc")
		var fe *FetchError
		if !errors.As(err, &fe) || fe.Status != 503 || fe.msg != "http 503" {
			t.Fatalf("err = %v, want http 503", err)
		}
		if r.Status != StatusError {
			t.Errorf("status = %v, want error", r.Status)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		src, _ := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		})
		src.client.Timeout = 100 * time.Millisecond
		_, err := src.fetch(context.Background(), "test-token-abc")
		var fe *FetchError
		if !errors.As(err, &fe) || fe.msg != errTimeout {
			t.Fatalf("err = %v, want timeout", err)
		}
	})

	t.Run("network", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close() // port now closed
		src := newOpenAISource(&http.Client{})
		src.url = url
		src.now = func() time.Time { return openaiNow }
		_, err := src.fetch(context.Background(), "test-token-abc")
		var fe *FetchError
		if !errors.As(err, &fe) || fe.msg != errNetwork {
			t.Fatalf("err = %v, want network error", err)
		}
	})
}

func TestOpenai_redirect_not_followed(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target was followed")
	}))
	t.Cleanup(target.Close)

	src, _ := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	})
	_, err := src.fetch(context.Background(), "test-token-abc")
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Status != 302 || fe.msg != "http 302" {
		t.Fatalf("err = %v, want http 302", err)
	}
}

func TestOpenai_oversize_body(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"pad":"`))
		w.Write([]byte(strings.Repeat("x", maxBodyBytes+16)))
		w.Write([]byte(`"}`))
	})
	_, err := src.fetch(context.Background(), "test-token-abc")
	var fe *FetchError
	if !errors.As(err, &fe) || fe.msg != errTooLarge {
		t.Fatalf("err = %v, want too large", err)
	}
}

func openaiReportWithAccountID(t *testing.T) Report {
	t.Helper()

	id := "acct-7f3d2b"
	// auth-claim id preferred; a top-level id is the fallback (second check)
	claim := openaiTestJWT(`{"sub":"u1","` + openaiAuthClaim + `":{"chatgpt_account_id":"` + id + `"},"email":"someone@example.com"}`)
	src, reqs := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_full.json")))
	r, err := src.fetch(context.Background(), claim)
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].Header.Get("ChatGPT-Account-Id") != id {
		t.Fatalf("ChatGPT-Account-Id = %q, want %q", (*reqs)[0].Header.Get("ChatGPT-Account-Id"), id)
	}
	return r
}

func TestOpenai_account_id_header(t *testing.T) {
	t.Parallel()

	t.Run("auth claim", func(t *testing.T) {
		t.Parallel()
		r := openaiReportWithAccountID(t)
		// The id must never appear in the stored report.
		enc, err := EncodeReport(r)
		if err != nil {
			t.Fatalf("EncodeReport: %v", err)
		}
		if strings.Contains(enc, "acct-7f3d2b") {
			t.Errorf("stored report contains the account id: %s", enc)
		}
	})

	t.Run("top-level claim fallback", func(t *testing.T) {
		t.Parallel()
		src, reqs := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_full.json")))
		if _, err := src.fetch(context.Background(), openaiTestJWT(`{"chatgpt_account_id":"acct-top"}`)); err != nil {
			t.Fatalf("fetch() error = %v", err)
		}
		if got := (*reqs)[0].Header.Get("ChatGPT-Account-Id"); got != "acct-top" {
			t.Errorf("ChatGPT-Account-Id = %q, want acct-top", got)
		}
	})

	t.Run("absent claim omits header", func(t *testing.T) {
		t.Parallel()
		src, reqs := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_full.json")))
		if _, err := src.fetch(context.Background(), openaiTestJWT(`{"sub":"u1"}`)); err != nil {
			t.Fatalf("fetch() error = %v", err)
		}
		if got := (*reqs)[0].Header.Get("ChatGPT-Account-Id"); got != "" {
			t.Errorf("ChatGPT-Account-Id = %q, want empty", got)
		}
	})

	t.Run("garbage token omits header", func(t *testing.T) {
		t.Parallel()
		src, reqs := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_full.json")))
		if _, err := src.fetch(context.Background(), "not-a-jwt"); err != nil {
			t.Fatalf("fetch() error = %v", err)
		}
		if got := (*reqs)[0].Header.Get("ChatGPT-Account-Id"); got != "" {
			t.Errorf("ChatGPT-Account-Id = %q, want empty", got)
		}
	})
}

func TestOpenai_email_dropped(t *testing.T) {
	t.Parallel()

	body := openaiFixture(t, "openai_email.json")
	src, _ := openaiTestSource(t, openaiJSONHandler(body))
	r, err := src.fetch(context.Background(), "test-token-abc")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if r.PlanLabel != "plus" {
		t.Errorf("PlanLabel = %q, want plus", r.PlanLabel)
	}
	enc, err := EncodeReport(r)
	if err != nil {
		t.Fatalf("EncodeReport: %v", err)
	}
	if strings.Contains(enc, "someone@example.com") {
		t.Errorf("stored report contains the response email: %s", enc)
	}
}

func TestOpenai_request_shape(t *testing.T) {
	t.Parallel()

	src, reqs := openaiTestSource(t, openaiJSONHandler(openaiFixture(t, "openai_full.json")))
	if _, err := src.fetch(context.Background(), "test-token-abc"); err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(*reqs))
	}
	req := (*reqs)[0]
	if req.Method != http.MethodGet {
		t.Errorf("method = %q, want GET", req.Method)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer test-token-abc" {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != openaiUserAgent {
		t.Errorf("User-Agent = %q", got)
	}
	if req.URL.RawQuery != "" {
		t.Errorf("query = %q, want none", req.URL.RawQuery)
	}
}

func TestOpenai_token_not_leaked(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("secret-body"))
	})
	r, err := src.fetch(context.Background(), "test-token-abc")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want *FetchError", err)
	}
	if strings.Contains(fe.Error(), "test-token-abc") || strings.Contains(fe.Error(), "secret-body") {
		t.Errorf("error message leaks: %s", fe.Error())
	}
	enc, err := EncodeReport(r)
	if err != nil {
		t.Fatalf("EncodeReport: %v", err)
	}
	if strings.Contains(enc, "test-token-abc") || strings.Contains(enc, "secret-body") {
		t.Errorf("stored report leaks: %s", enc)
	}
}

func TestOpenai_ctx_cancel(t *testing.T) {
	t.Parallel()

	src, _ := openaiTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := src.fetch(ctx, "test-token-abc")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if r.Version != 0 || r.Status != "" || r.FetchedAt != (time.Time{}) {
		t.Errorf("report = %+v, want zero", r)
	}
}

func TestOpenai_duplicate_durations_get_unique_ids(t *testing.T) {
	t.Parallel()

	body := `{"rate_limit":{"primary_window":{"used_percent":1,"limit_window_seconds":604800},"secondary_window":{"used_percent":2,"limit_window_seconds":604800}}}`
	r, ok := parseOpenAIUsage([]byte(body), time.Unix(1_700_000_000, 0))
	if !ok || len(r.Windows) != 2 {
		t.Fatalf("parse ok=%v windows=%d, want 2 windows", ok, len(r.Windows))
	}
	if r.Windows[0].ID == r.Windows[1].ID {
		t.Fatalf("window ids collide: %q", r.Windows[0].ID)
	}
	if r.Windows[0].ID != "weekly" {
		t.Errorf("first id = %q, want weekly (stable)", r.Windows[0].ID)
	}
}

func TestOpenai_absurd_numbers_are_ignored(t *testing.T) {
	t.Parallel()

	body := `{"rate_limit":{"primary_window":{"used_percent":5,"reset_at":1e300,"limit_window_seconds":1e300}}}`
	r, ok := parseOpenAIUsage([]byte(body), time.Unix(1_700_000_000, 0))
	if !ok || len(r.Windows) != 1 {
		t.Fatalf("parse ok=%v windows=%d, want 1", ok, len(r.Windows))
	}
	w := r.Windows[0]
	if w.ResetsAt != nil || w.DurationSeconds != nil || w.ID != "unknown_primary" {
		t.Errorf("window = %+v, want no reset/duration and id unknown_primary", w)
	}
}
