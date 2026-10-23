package profiles

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func mustCreate(t *testing.T, r *Registry, p store.AgentProfile) store.AgentProfile {
	t.Helper()
	got, err := r.Create(p)
	if err != nil {
		t.Fatalf("Create(%+v) error = %v", p, err)
	}
	return got
}

func TestProfile_OnRateLimitEnum(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	for _, v := range []string{"", "none", "wait"} {
		got, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", OnRateLimit: v})
		if err != nil {
			t.Fatalf("OnRateLimit %q: %v", v, err)
		}
		want := v
		if want == "" {
			want = "none"
		}
		if got.OnRateLimit != want {
			t.Errorf("OnRateLimit %q stored as %q, want %q", v, got.OnRateLimit, want)
		}
	}
	if _, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", OnRateLimit: "retry"}); err == nil {
		t.Fatalf("OnRateLimit retry accepted, want error")
	}
}

func TestProfile_FallbackRequiredForFallbackPolicy(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	if _, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", OnRateLimit: "fallback"}); err == nil {
		t.Fatalf("fallback without fallbackProfileId accepted, want error")
	}
}

func TestProfile_FallbackMustBeOtherProvider(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	same := mustCreate(t, r, store.AgentProfile{Name: "same", Provider: "claude-native"})
	other := mustCreate(t, r, store.AgentProfile{Name: "other", Provider: "codex-native"})

	_, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", OnRateLimit: "fallback", FallbackProfileID: &same.ID})
	if err == nil || !strings.Contains(err.Error(), "fallback profile must use a different provider than the profile it backs up") {
		t.Fatalf("same-provider fallback error = %v", err)
	}
	// "wait" with a same-provider id is also rejected: the id is validated whenever present.
	if _, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", OnRateLimit: "wait", FallbackProfileID: &same.ID}); err == nil {
		t.Fatalf("wait with same-provider fallback accepted, want error")
	}
	for _, pol := range []string{"fallback", "wait", "none"} {
		if _, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", OnRateLimit: pol, FallbackProfileID: &other.ID}); err != nil {
			t.Fatalf("policy %s with other-provider fallback: %v", pol, err)
		}
	}
}

func TestProfile_FallbackMustExistAndNotSelf(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	missing := int64(9999)
	if _, err := r.Create(store.AgentProfile{Name: "p", Provider: "claude-native", OnRateLimit: "fallback", FallbackProfileID: &missing}); err == nil {
		t.Fatalf("missing fallback accepted, want error")
	}
	p := mustCreate(t, r, store.AgentProfile{Name: "p", Provider: "claude-native"})
	p.OnRateLimit, p.FallbackProfileID = "wait", &p.ID
	if _, err := r.Update(p); err == nil {
		t.Fatalf("self fallback accepted, want error")
	}
}

func TestProfile_LegacyProfileDefaultsToNone(t *testing.T) {
	t.Parallel()
	r := newTestRegistry(t)
	p := mustCreate(t, r, store.AgentProfile{Name: "legacy", Provider: "claude-native"})
	got, err := r.Get(p.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.OnRateLimit != "none" || got.FallbackProfileID != nil {
		t.Fatalf("legacy profile = %q / %v, want none / nil", got.OnRateLimit, got.FallbackProfileID)
	}
	got.Notes = "edited"
	if _, err := r.Update(got); err != nil {
		t.Fatalf("Update() of legacy profile error = %v", err)
	}
}
