package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/quota"
	"github.com/spacingmind/smind/internal/store"
)

type fakeSecrets map[string]string // "instance/name" -> value

func (f fakeSecrets) GetProviderSecret(id, name string) (string, bool, error) {
	v, ok := f[id+"/"+name]
	return v, ok, nil
}
func (f fakeSecrets) ListProviderSecrets(string) (map[string]store.ProviderSecretInfo, error) {
	return nil, nil
}
func (f fakeSecrets) PruneProviderSecrets([]string) (int, error) { return 0, nil }

func zaiSpec(id, base string) providers.Spec {
	return providers.Spec{
		ID: id, Label: "L " + id, Extends: providers.IDClaudeNative, Vendor: providers.VendorZAI,
		Env:       map[string]string{"ANTHROPIC_BASE_URL": base},
		SecretEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "api-key"},
	}
}

func testRegistry(t *testing.T, specs ...providers.Spec) *providers.Registry {
	t.Helper()
	reg := providers.New()
	if err := reg.SetCustom(specs); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestZaiInstances_EligibleOnlyZaiVendorCustom(t *testing.T) {
	generic := zaiSpec("generic-one", "https://api.z.ai/api/anthropic")
	generic.Vendor = providers.VendorGeneric
	reg := testRegistry(t,
		zaiSpec("zz-b", "https://api.z.ai/api/anthropic"),
		generic,
		zaiSpec("evil", "https://evil.example.com"),
		zaiSpec("aa-a", "https://api.z.ai/api/anthropic"),
	)
	src := &zaiInstanceSource{reg: reg, secrets: fakeSecrets{}}
	got := src.UsageInstances()
	if len(got) != 2 || got[0].ID != "aa-a" || got[1].ID != "zz-b" || got[0].Label != "L aa-a" {
		t.Fatalf("instances = %+v", got)
	}
}

func TestZaiInstances_FromRecipeConfig(t *testing.T) {
	rec, ok := providers.RecipeByID("zai-glm")
	if !ok {
		t.Skip("zai-glm recipe not in the catalog")
	}
	entry, _, err := config.BuildRecipeEntry(rec, config.RecipeInput{})
	if err != nil {
		t.Fatal(err)
	}
	reg := testRegistry(t, config.ProviderSpecs([]config.CustomProvider{entry})...)
	secrets := fakeSecrets{entry.ID + "/" + config.RecipeSecretName: "k-1"}
	src := &zaiInstanceSource{reg: reg, secrets: secrets}
	if got := src.UsageInstances(); len(got) != 1 || got[0].ID != entry.ID {
		t.Fatalf("instances = %+v", got)
	}
	url, key, err := src.Creds(entry.ID)
	if err != nil || key != "k-1" || url != "https://api.z.ai/api/monitor/usage/quota/limit" {
		t.Fatalf("creds = %q %q %v", url, key, err)
	}
}

func TestZaiInstances_CredsUseSecretStoreAndAllowlistedURL(t *testing.T) {
	reg := testRegistry(t, zaiSpec("z1", "https://api.z.ai/api/anthropic"))
	src := &zaiInstanceSource{reg: reg, secrets: fakeSecrets{"z1/api-key": "sekret"}}
	url, key, err := src.Creds("z1")
	if err != nil || url != "https://api.z.ai/api/monitor/usage/quota/limit" || key != "sekret" {
		t.Fatalf("creds = %q %q %v", url, key, err)
	}
}

func TestZaiInstances_NoKey(t *testing.T) {
	reg := testRegistry(t, zaiSpec("z1", "https://api.z.ai/api/anthropic"))
	src := &zaiInstanceSource{reg: reg, secrets: fakeSecrets{}}
	if _, _, err := src.Creds("z1"); !errors.Is(err, quota.ErrInstanceNoKey) {
		t.Fatalf("not stored: %v", err)
	}
	src.secrets = fakeSecrets{"z1/api-key": "  "}
	if _, _, err := src.Creds("z1"); !errors.Is(err, quota.ErrInstanceNoKey) {
		t.Fatalf("blank: %v", err)
	}
	if _, _, err := src.Creds("nope"); !errors.Is(err, quota.ErrInstanceUnsupported) {
		t.Fatalf("unknown: %v", err)
	}
}

const fullFiveHour = `{"code":200,"success":true,"data":{"level":"pro","limits":[
{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":%d,"nextResetTime":4102444800000}]}}`

// warmed returns an adapter whose cache holds one fetched report (used pct),
// and the number of requests the server saw.
func warmed(t *testing.T, used int, routing bool, threshold float64) (*providerQuotaAdapter, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, fullFiveHour, used)
	}))
	t.Cleanup(ts.Close)
	reg := testRegistry(t, zaiSpec("z1", "https://api.z.ai/api/anthropic"))
	src := &zaiInstanceSource{
		reg: reg, secrets: fakeSecrets{"z1/api-key": "k"},
		quotaURL: func(string) (string, bool) { return ts.URL, true },
	}
	pu := quota.NewProviderUsage(quota.NewZaiInstanceFetcher(src, nil))
	t.Cleanup(pu.Close)
	res := pu.Touch(context.Background(), "z1")
	if res.Report == nil || hits.Load() != 1 {
		t.Fatalf("warm: report=%v hits=%d lastError=%q", res.Report, hits.Load(), res.LastError)
	}
	return &providerQuotaAdapter{pu: pu, inst: src, routing: routing, thresholdPct: threshold}, &hits
}

func TestProviderQuotaAdapter_CacheOnlyNoRequests(t *testing.T) {
	a, hits := warmed(t, 100, true, 100)
	now := time.Now()
	want := time.UnixMilli(4102444800000)
	for i := 0; i < 20; i++ {
		at, ok := a.ExhaustedUntil("z1", now)
		if !ok || !at.Equal(want) {
			t.Fatalf("ExhaustedUntil = %v %v", at, ok)
		}
		m := a.ExhaustedProviders(now)
		if len(m) != 1 || !m["z1"].Equal(want) {
			t.Fatalf("ExhaustedProviders = %v", m)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
}

func TestProviderQuotaAdapter_RoutingOffIgnoresWindow(t *testing.T) {
	a, _ := warmed(t, 100, false, 100)
	if _, ok := a.ExhaustedUntil("z1", time.Now()); ok {
		t.Fatal("exhausted with routing off")
	}
	if m := a.ExhaustedProviders(time.Now()); len(m) != 0 {
		t.Fatalf("providers = %v", m)
	}
}

func TestProviderQuotaAdapter_ThresholdRespected(t *testing.T) {
	a, _ := warmed(t, 90, true, 100)
	if _, ok := a.ExhaustedUntil("z1", time.Now()); ok {
		t.Fatal("90% exhausted at threshold 100")
	}
	a.thresholdPct = 90
	if _, ok := a.ExhaustedUntil("z1", time.Now()); !ok {
		t.Fatal("90% not exhausted at threshold 90")
	}
}

func TestProviderQuotaAdapter_NonZaiProviderIgnored(t *testing.T) {
	a, _ := warmed(t, 100, true, 100)
	// Put a report in the cache under another id too.
	a.pu.Touch(context.Background(), "claude-native")
	if _, ok := a.ExhaustedUntil("claude-native", time.Now()); ok {
		t.Fatal("non-Z.AI provider reported exhausted")
	}
	if m := a.ExhaustedProviders(time.Now()); len(m) != 1 {
		t.Fatalf("providers = %v", m)
	}
}
