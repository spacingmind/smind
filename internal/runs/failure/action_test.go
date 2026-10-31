package failure

import (
	"strings"
	"testing"
	"time"
)

func TestNextAction_PerClass(t *testing.T) {
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	next := now.Add(30 * time.Second)
	cases := []struct {
		name, kind string
		ctx        ActionContext
		wantKind   string
		wantTarget string
		wantLabel  string
	}{
		{"auth", KindAuthExpired, ActionContext{Provider: "glm", ProviderLabel: "GLM"}, ActionSignIn, "settings:providers?provider=glm", "Sign in again to GLM"},
		{"auth no provider", KindAuthExpired, ActionContext{}, ActionSignIn, "settings:providers", "this provider"},
		{"model plain", KindModelNotInPlan, ActionContext{Model: "opus"}, ActionPickModel, "", "opus is not in this plan"},
		{"model with allowed", KindModelNotInPlan, ActionContext{Model: "opus", AllowedModels: []string{"sonnet", "haiku"}}, ActionPickModel, "", "sonnet, haiku"},
		{"transient queued", KindTransient429, ActionContext{Attempt: 1, Max: 2, NextAt: &next}, ActionAutoRetry, "", "1 of 2"},
		{"transient spent", KindTransient429, ActionContext{Provider: "glm"}, ActionRetryNow, "", "retry"},
		{"plan wait", KindPlanWindowExhausted, ActionContext{Window: "5h", ResetAt: &reset, Now: now}, ActionWaitUntil, "2026-11-01T12:00:00Z", "5h window resets 12:00"},
		{"plan fallback", KindPlanWindowExhausted, ActionContext{Window: "5h", ResetAt: &reset, Now: now, FallbackProfileID: 9, FallbackProfileName: "glm-peer"}, ActionSwitchProfile, "profile:9", "Switch to glm-peer or wait"},
		{"plan unknown reset", KindPlanWindowExhausted, ActionContext{Now: now}, ActionRetryNow, "", "Reset time unknown"},
		{"residual", KindRateLimited, ActionContext{Now: now}, ActionRetryNow, "", "Retry"},
		{"cli", KindCLIExited, ActionContext{}, ActionContinue, "", "Continue"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := NextAction(c.kind, c.ctx)
			if a == nil || a.Kind != c.wantKind || a.Target != c.wantTarget || !strings.Contains(a.Label, c.wantLabel) {
				t.Fatalf("got %+v, want kind %q target %q label ~%q", a, c.wantKind, c.wantTarget, c.wantLabel)
			}
		})
	}
	if NextAction("", ActionContext{}) != nil || NextAction("martian", ActionContext{}) != nil {
		t.Fatal("empty or unknown kind must have no action")
	}
}
