package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

type pdmHarness struct {
	api  *API
	path string
}

func newPDMHarness(t *testing.T) *pdmHarness {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	wm, db := newTestWorkspaceManager(t)
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	return &pdmHarness{api: api, path: filepath.Join(home, "config.yaml")}
}

func (h *pdmHarness) set(rc *requestContext, provider, model string) (any, error) {
	raw, _ := json.Marshal(map[string]string{"provider": provider, "model": model})
	return h.api.hs["provider.setDefaultModel"](context.Background(), rc, raw)
}

func (h *pdmHarness) listed(t *testing.T, provider string) string {
	t.Helper()
	res, err := h.api.hs["provider.list"](context.Background(), &requestContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.(providerListResult).Providers {
		if string(p.ID) == provider {
			return p.DefaultModel
		}
	}
	t.Fatalf("provider %q not listed", provider)
	return ""
}

func (h *pdmHarness) file(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func TestProviderSetDefaultModel_SetAndClear(t *testing.T) {
	h := newPDMHarness(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	res, err := h.set(&requestContext{}, "glm", "fake-model-b")
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(providerUpdatedPayload); got.ID != "glm" || got.DefaultModel != "fake-model-b" {
		t.Fatalf("result = %+v", got)
	}
	if got := h.listed(t, "glm"); got != "fake-model-b" {
		t.Fatalf("provider.list defaultModel = %q", got)
	}
	if body := h.file(t); !strings.Contains(body, "fake-model-b") {
		t.Fatalf("config.yaml lacks the model:\n%s", body)
	}
	ev := queued(sub)
	if len(ev) != 1 || ev[0].Topic != TopicProviderUpdated {
		t.Fatalf("events = %+v", ev)
	}
	if p := ev[0].Payload.(providerUpdatedPayload); p.ID != "glm" || p.DefaultModel != "fake-model-b" {
		t.Fatalf("event payload = %+v", p)
	}

	if _, err := h.set(&requestContext{}, "glm", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.listed(t, "glm"); got != "" {
		t.Fatalf("after clear, defaultModel = %q", got)
	}
	if body := h.file(t); strings.Contains(body, "defaultModel") {
		t.Fatalf("defaultModel still in file:\n%s", body)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel("glm") != "" {
		t.Fatal("config still has a glm default")
	}
}

func TestProviderSetDefaultModel_Rejected(t *testing.T) {
	h := newPDMHarness(t)
	if _, err := h.set(&requestContext{}, "glm", "keep"); err != nil {
		t.Fatal(err)
	}
	before := h.file(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	for _, tc := range []struct{ provider, model string }{{"nope", "x"}, {"glm", "--evil"}} {
		if _, err := h.set(&requestContext{}, tc.provider, tc.model); err == nil {
			t.Fatalf("%+v accepted", tc)
		}
	}
	if got := h.listed(t, "glm"); got != "keep" {
		t.Fatalf("live default changed to %q", got)
	}
	if h.file(t) != before {
		t.Fatal("config.yaml changed on a rejected call")
	}
	if ev := queued(sub); len(ev) != 0 {
		t.Fatalf("events on failure: %+v", ev)
	}
}

func TestProviderSetDefaultModel_RunScopedForbidden(t *testing.T) {
	h := newPDMHarness(t)
	sub := subscribeTopics(h.api, TopicProviderUpdated)
	_, err := h.set(&requestContext{runScoped: true}, "glm", "x")
	if err == nil {
		t.Fatal("run-scoped caller allowed")
	}
	if c := codedError(err); c.Code != codeForbidden {
		t.Fatalf("code = %q, want %q", c.Code, codeForbidden)
	}
	if got := h.listed(t, "glm"); got != "" {
		t.Fatalf("default changed to %q", got)
	}
	if h.file(t) != "" {
		t.Fatalf("config.yaml written:\n%s", h.file(t))
	}
	if len(queued(sub)) != 0 {
		t.Fatal("event published")
	}
}
