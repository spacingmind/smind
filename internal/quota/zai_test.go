package quota

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

var zaiTestNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func zaiFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/zai_quota_limit.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseZai_Fixture(t *testing.T) {
	report, ok := parseZaiUsage(zaiFixture(t), zaiTestNow)
	if !ok {
		t.Fatal("ok = false")
	}
	if report.Status != StatusAvailable || report.PlanLabel != "pro" {
		t.Fatalf("status/plan = %v/%q", report.Status, report.PlanLabel)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(report.Windows))
	}
	five, weekly := report.Windows[0], report.Windows[1]
	if five.ID != "five_hour" || five.UsedPct == nil || *five.UsedPct != 42 ||
		five.RemainingPct == nil || *five.RemainingPct != 58 || five.Tone != ToneOK {
		t.Fatalf("five_hour = %+v", five)
	}
	if want := time.UnixMilli(1778753640304).UTC(); five.ResetsAt == nil || !five.ResetsAt.Equal(want) {
		t.Fatalf("five_hour resetsAt = %v, want %v", five.ResetsAt, want)
	}
	if five.DurationSeconds == nil || *five.DurationSeconds != 18000 {
		t.Fatalf("five_hour duration = %v", five.DurationSeconds)
	}
	if weekly.ID != "weekly" || weekly.UsedPct == nil || *weekly.UsedPct != 7 ||
		weekly.DurationSeconds == nil || *weekly.DurationSeconds != 604800 {
		t.Fatalf("weekly = %+v", weekly)
	}
	if len(report.Balances) != 1 {
		t.Fatalf("balances = %d, want 1", len(report.Balances))
	}
	b := report.Balances[0]
	if b.ID != "mcp_monthly" || b.Unit != "requests" || b.Used == nil || *b.Used != 250 ||
		b.Remaining == nil || *b.Remaining != 750 || b.Limit == nil || *b.Limit != 1000 {
		t.Fatalf("balance = %+v", b)
	}
}

func TestParseZai_MissingWeekly(t *testing.T) {
	body := `{"code":200,"success":true,"data":{"level":"lite","limits":[
		{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":10,"nextResetTime":1778753640304},
		{"type":"WHATEVER","unit":9,"number":9}]}}`
	report, ok := parseZaiUsage([]byte(body), zaiTestNow)
	if !ok || len(report.Windows) != 1 || report.Windows[0].ID != "five_hour" {
		t.Fatalf("ok=%v report=%+v", ok, report)
	}
}

func TestParseZai_NoResetTime(t *testing.T) {
	body := `{"code":200,"success":true,"data":{"level":"pro","limits":[
		{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":150}]}}`
	report, ok := parseZaiUsage([]byte(body), zaiTestNow)
	if !ok || len(report.Windows) != 1 {
		t.Fatalf("ok=%v report=%+v", ok, report)
	}
	w := report.Windows[0]
	if w.ResetsAt != nil || w.UsedPct == nil || *w.UsedPct != 100 {
		t.Fatalf("window = %+v", w)
	}
}

func TestParseZai_MalformedAndBusinessError(t *testing.T) {
	for _, body := range []string{"<html>nope</html>", "null", `{"code":500,"success":false}`, "{}"} {
		if _, ok := parseZaiUsage([]byte(body), zaiTestNow); ok {
			t.Errorf("body %q: ok = true, want false", body)
		}
	}
}

func TestParseZai_NoPlan(t *testing.T) {
	report, ok := parseZaiUsage([]byte(`{"code":200,"success":true,"data":{"limits":[]}}`), zaiTestNow)
	if !ok || report.Status != StatusUnavailable || report.Problem == nil ||
		report.Problem.Kind != ProblemNoQuota || report.Problem.Detail != detailNoPlan {
		t.Fatalf("ok=%v report=%+v", ok, report)
	}
}

func newZaiTestSource(srv *httptest.Server) *zaiSource {
	s := newZaiSource(srv.Client(), srv.URL+"/api/monitor/usage/quota/limit")
	s.now = func() time.Time { return zaiTestNow }
	return s
}

func TestZaiSource_Fetch_RawAuthHeaderNoBearer(t *testing.T) {
	const key = "zai-sentinel-key-123"
	var gotAuth string
	body := zaiFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	report, err := newZaiTestSource(srv).fetch(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != key {
		t.Fatalf("Authorization = %q, want %q", gotAuth, key)
	}
	if report.Status != StatusAvailable || len(report.Windows) != 2 {
		t.Fatalf("report = %+v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), key) {
		t.Fatal("api key leaked into report")
	}
}

func TestZaiSource_Fetch_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	report, err := newZaiTestSource(srv).fetch(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusUnavailable || report.Problem == nil ||
		report.Problem.Kind != ProblemRejected || report.Problem.Status != 401 {
		t.Fatalf("report = %+v", report)
	}
}

func TestZaiSource_Fetch_429RetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	report, err := newZaiTestSource(srv).fetch(context.Background(), "k")
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) || fetchErr.RetryAfter != 120*time.Second {
		t.Fatalf("err = %v", err)
	}
	if report.Status != StatusError {
		t.Fatalf("status = %v", report.Status)
	}
}

func TestZaiSource_Fetch_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()

	report, err := newZaiTestSource(srv).fetch(context.Background(), "k")
	if err == nil || report.Error != "malformed response" {
		t.Fatalf("err=%v report=%+v", err, report)
	}
}

func TestZaiSource_HostAllowlist(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.z.ai/api/anthropic", "https://api.z.ai/api/monitor/usage/quota/limit"},
		{"https://open.bigmodel.cn/api/anthropic", "https://open.bigmodel.cn/api/monitor/usage/quota/limit"},
		{"http://api.z.ai", ""},
		{"https://evil.example.com", ""},
		{"https://api.z.ai.evil.com", ""},
		{"", ""},
	}
	for _, c := range cases {
		got, ok := ZaiQuotaURL(c.in)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("ZaiQuotaURL(%q) = %q, %v", c.in, got, ok)
		}
	}
}
