package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/quota"
)

type fakeInstanceFetcher struct{ calls atomic.Int32 }

func (f *fakeInstanceFetcher) Fetch(context.Context, string) (quota.Report, error) {
	f.calls.Add(1)
	used := 100.0
	resets := time.Now().Add(2 * time.Hour)
	return quota.Report{
		Version: 1,
		Status:  quota.StatusAvailable,
		Windows: []quota.Window{{ID: "five_hour", Label: "5 hours", UsedPct: &used, ResetsAt: &resets}},
	}, nil
}

type fakeUsageInstances []ProviderUsageInstance

func (f fakeUsageInstances) UsageInstances() []ProviderUsageInstance { return f }

type fakeInstCreds struct {
	url, key string
	err      error
}

func (c fakeInstCreds) Creds(string) (string, string, error) { return c.url, c.key, c.err }

func providerUsageCall(t *testing.T, h handlerFunc, runScoped bool, params map[string]any) (providerUsageResult, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := h(context.Background(), &requestContext{runScoped: runScoped}, raw)
	if err != nil {
		return providerUsageResult{}, err
	}
	return out.(providerUsageResult), nil
}

func mustProviderUsage(t *testing.T, h handlerFunc, params map[string]any) providerUsageResult {
	t.Helper()
	res, err := providerUsageCall(t, h, false, params)
	if err != nil {
		t.Fatalf("provider.usage error = %v", err)
	}
	return res
}

var oneInstance = fakeUsageInstances{{ID: "zai", Label: "Z.AI"}}

func TestProviderUsageRPC_Modes(t *testing.T) {
	f := &fakeInstanceFetcher{}
	pu := quota.NewProviderUsage(f)
	t.Cleanup(pu.Close)
	h := handleProviderUsage(pu, oneInstance)

	res := mustProviderUsage(t, h, nil)
	if !res.Enabled || len(res.Reports) != 1 {
		t.Fatalf("cached = %+v", res)
	}
	r := res.Reports[0]
	if r.Report != nil || f.calls.Load() != 0 {
		t.Fatalf("cached on cold: report=%v calls=%d", r.Report, f.calls.Load())
	}
	if r.InstanceID != "zai" || r.Key != "instance-zai" || r.Provider != "zai" || r.DisplayName != "Z.AI" || r.CredentialType != "api_key" || r.AccountID != 0 {
		t.Fatalf("row = %+v", r)
	}

	res = mustProviderUsage(t, h, map[string]any{"mode": "touch"})
	if res.Reports[0].Report == nil || f.calls.Load() != 1 {
		t.Fatalf("touch: report=%v calls=%d", res.Reports[0].Report, f.calls.Load())
	}
	mustProviderUsage(t, h, map[string]any{"mode": "touch"})
	if f.calls.Load() != 1 {
		t.Fatalf("second touch calls = %d, want 1", f.calls.Load())
	}

	res = mustProviderUsage(t, h, map[string]any{"mode": "refresh"})
	r = res.Reports[0]
	if f.calls.Load() != 1 || !r.Stale || r.RefreshBlocked == nil || *r.RefreshBlocked != "floor" {
		t.Fatalf("refresh within floor: calls=%d stale=%v blocked=%v", f.calls.Load(), r.Stale, r.RefreshBlocked)
	}
}

func TestProviderUsageRPC_RefusedForAgents(t *testing.T) {
	f := &fakeInstanceFetcher{}
	pu := quota.NewProviderUsage(f)
	t.Cleanup(pu.Close)
	h := handleProviderUsage(pu, oneInstance)
	for _, mode := range []string{"cached", "touch", "refresh"} {
		_, err := providerUsageCall(t, h, true, map[string]any{"mode": mode})
		var ce *rpcCodeError
		if !errors.As(err, &ce) || ce.code != codeForbidden {
			t.Fatalf("mode %s: err = %v, want forbidden", mode, err)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", f.calls.Load())
	}
	for _, m := range RunScopedMethods {
		if m == "provider.usage" {
			t.Fatal("provider.usage must not be run-scoped")
		}
	}
}

func TestProviderUsageRPC_UnknownInstanceAndMode(t *testing.T) {
	f := &fakeInstanceFetcher{}
	pu := quota.NewProviderUsage(f)
	t.Cleanup(pu.Close)
	h := handleProviderUsage(pu, oneInstance)

	_, err := providerUsageCall(t, h, false, map[string]any{"instanceIds": []string{"nope"}})
	if err == nil || err.Error() != `provider.usage: unknown instance "nope"` {
		t.Fatalf("unknown instance err = %v", err)
	}
	_, err = providerUsageCall(t, h, false, map[string]any{"mode": "bogus"})
	if err == nil || err.Error() != `provider.usage: unknown mode "bogus"` {
		t.Fatalf("unknown mode err = %v", err)
	}
	res := mustProviderUsage(t, h, map[string]any{"instanceIds": []string{"zai", "zai"}})
	if len(res.Reports) != 1 {
		t.Fatalf("deduped reports = %d, want 1", len(res.Reports))
	}
	if f.calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", f.calls.Load())
	}
}

func TestProviderUsageRPC_DisabledMakesNoRequest(t *testing.T) {
	f := &fakeInstanceFetcher{}
	pu := quota.NewProviderUsage(f, quota.WithProviderUsageEnabled(false))
	t.Cleanup(pu.Close)
	h := handleProviderUsage(pu, oneInstance)
	for _, mode := range []string{"cached", "touch", "refresh"} {
		res := mustProviderUsage(t, h, map[string]any{"mode": mode})
		if res.Enabled || len(res.Reports) != 0 {
			t.Fatalf("mode %s: %+v", mode, res)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", f.calls.Load())
	}
}

const zaiRPCBody = `{"code":200,"success":true,"data":{"level":"pro","limits":[
 {"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":42,"nextResetTime":1778753640304},
 {"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":7,"nextResetTime":1779300000000}]}}`

func TestProviderUsageRPC_SentinelKeyNeverLeaks(t *testing.T) {
	const sentinel = "zai-sentinel-key-789"

	captureLog := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		var buf bytes.Buffer
		prev := log.Writer()
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(prev) })
		return &buf
	}

	t.Run("fetch", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(zaiRPCBody))
		}))
		t.Cleanup(srv.Close)
		buf := captureLog(t)
		pu := quota.NewProviderUsage(quota.NewZaiInstanceFetcher(fakeInstCreds{url: srv.URL, key: sentinel}, srv.Client()))
		t.Cleanup(pu.Close)
		h := handleProviderUsage(pu, oneInstance)

		var all []byte
		for _, mode := range []string{"touch", "refresh"} {
			res := mustProviderUsage(t, h, map[string]any{"mode": mode})
			b, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, b...)
		}
		if !strings.Contains(string(all), `"report":{`) {
			t.Fatalf("expected a report in %s", all)
		}
		if strings.Contains(string(all), sentinel) {
			t.Fatalf("sentinel leaked in result: %s", all)
		}
		pu.Close() // waits for the fetch goroutine's trailing log line
		if strings.Contains(buf.String(), sentinel) {
			t.Fatalf("sentinel leaked in log: %s", buf.String())
		}
	})

	t.Run("creds error", func(t *testing.T) {
		buf := captureLog(t)
		creds := fakeInstCreds{err: errors.New("lookup failed for key " + sentinel)}
		pu := quota.NewProviderUsage(quota.NewZaiInstanceFetcher(creds, nil))
		t.Cleanup(pu.Close)
		h := handleProviderUsage(pu, oneInstance)

		res := mustProviderUsage(t, h, map[string]any{"mode": "touch"})
		r := res.Reports[0]
		if r.LastError == nil || *r.LastError != "error" {
			t.Fatalf("lastError = %v, want \"error\"", r.LastError)
		}
		b, _ := json.Marshal(res)
		if strings.Contains(string(b), sentinel) {
			t.Fatalf("sentinel leaked in result: %s", b)
		}
		pu.Close()
		if strings.Contains(buf.String(), sentinel) {
			t.Fatalf("sentinel leaked in log: %s", buf.String())
		}
	})
}

func TestProviderUsageRPC_SingleFlightCounts(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(zaiRPCBody))
	}))
	t.Cleanup(srv.Close)
	pu := quota.NewProviderUsage(quota.NewZaiInstanceFetcher(fakeInstCreds{url: srv.URL, key: "k"}, srv.Client()))
	t.Cleanup(pu.Close)
	h := handleProviderUsage(pu, oneInstance)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]any{"mode": "touch"})
			if _, err := h(context.Background(), &requestContext{}, raw); err != nil {
				t.Errorf("touch error = %v", err)
			}
		}()
	}
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Fatalf("server hits = %d, want 1", n)
	}
}
