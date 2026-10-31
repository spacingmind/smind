package failure

import (
	"fmt"
	"strings"
	"time"
)

// The Action.Kind values (the wire's action.kind). A client renders Label for
// a kind it does not know.
const (
	ActionSignIn        = "sign_in"
	ActionWaitUntil     = "wait_until"
	ActionSwitchProfile = "switch_profile"
	ActionPickModel     = "pick_model"
	ActionAutoRetry     = "auto_retry"
	ActionRetryNow      = "retry_now"
	ActionContinue      = "continue"
)

// Action is the concrete next step for a failed run (additive, omitempty on
// the wire). The daemon builds it, so web, mobile, MCP and the CLI share one
// vocabulary.
type Action struct {
	Kind string `json:"kind"`
	// Target is what the control acts on: a settings deep link
	// ("settings:providers?provider=glm"), "profile:<id>", or an RFC 3339 time.
	Target string `json:"target,omitempty"`
	Label  string `json:"label"`
	// Models are the models the plan allows, when known (pick_model).
	Models []string `json:"models,omitempty"`
	// Attempt/Max/NextAt describe a pending automatic retry (auto_retry).
	Attempt int        `json:"attempt,omitempty"`
	Max     int        `json:"max,omitempty"`
	NextAt  *time.Time `json:"nextAt,omitempty"`
}

// ActionContext is what NextAction needs; every field is optional.
type ActionContext struct {
	Provider string
	// ProviderLabel names the provider/account in labels; falls back to Provider.
	ProviderLabel string
	Model         string
	// AllowedModels are the models the plan includes, when known.
	AllowedModels []string
	Window        string
	ResetAt       *time.Time
	// FallbackProfileID/Name: the launching profile's fallback (ADR-0041 F2).
	FallbackProfileID   int64
	FallbackProfileName string
	// Attempt/Max/NextAt: a transient retry already queued (Attempt is 1-based).
	Attempt int
	Max     int
	NextAt  *time.Time
	Now     time.Time
}

func (c ActionContext) who() string {
	if c.ProviderLabel != "" {
		return c.ProviderLabel
	}
	if c.Provider != "" {
		return c.Provider
	}
	return "this provider"
}

// NextAction is the next step for a failure of kind; nil for "" and for a kind
// this daemon does not know.
func NextAction(kind string, c ActionContext) *Action {
	switch kind {
	case KindAuthExpired:
		target := "settings:providers"
		if c.Provider != "" {
			target += "?provider=" + c.Provider
		}
		return &Action{Kind: ActionSignIn, Target: target, Label: "Sign in again to " + c.who()}
	case KindModelNotInPlan:
		model := "the chosen model"
		if c.Model != "" {
			model = c.Model
		}
		label := "Pick another model: " + model + " is not in this plan"
		if len(c.AllowedModels) > 0 {
			label = "Pick another model (" + model + " is not in this plan): " + strings.Join(c.AllowedModels, ", ")
		}
		return &Action{Kind: ActionPickModel, Label: label, Models: c.AllowedModels}
	case KindTransient429:
		if c.NextAt != nil && c.Attempt > 0 && c.Max > 0 {
			return &Action{Kind: ActionAutoRetry, Label: fmt.Sprintf("Retrying automatically (%d of %d)", c.Attempt, c.Max), Attempt: c.Attempt, Max: c.Max, NextAt: c.NextAt}
		}
		return &Action{Kind: ActionRetryNow, Label: c.who() + " throttled briefly: retry"}
	case KindPlanWindowExhausted, KindRateLimited:
		return planAction(kind, c)
	case KindCLIExited:
		return &Action{Kind: ActionContinue, Label: "Continue the run"}
	}
	return nil
}

func planAction(kind string, c ActionContext) *Action {
	wait := ""
	if c.ResetAt != nil && c.ResetAt.After(c.Now) {
		wait = "resets " + c.ResetAt.In(c.Now.Location()).Format("15:04")
		if c.Window != "" {
			wait = c.Window + " window " + wait
		}
	}
	if c.FallbackProfileID != 0 {
		label := "Switch to the fallback profile"
		if c.FallbackProfileName != "" {
			label = "Switch to " + c.FallbackProfileName
		}
		if wait != "" {
			label += " or wait (" + wait + ")"
		}
		return &Action{Kind: ActionSwitchProfile, Target: fmt.Sprintf("profile:%d", c.FallbackProfileID), Label: label}
	}
	if c.ResetAt != nil && c.ResetAt.After(c.Now) {
		return &Action{Kind: ActionWaitUntil, Target: c.ResetAt.UTC().Format(time.RFC3339), Label: "Wait: " + wait}
	}
	if kind == KindRateLimited {
		return &Action{Kind: ActionRetryNow, Label: "Retry"}
	}
	return &Action{Kind: ActionRetryNow, Label: "Reset time unknown: retry later"}
}

// Guidance is the one-line routing rule for a failed run of kind, written for
// a Lead reading MCP or CLI output: what to do next and what NOT to do. ""
// for an empty or unknown kind.
func Guidance(kind string) string {
	switch kind {
	case KindAuthExpired:
		return "A human must sign in again (the action names where). Do not retry and do not fall back: neither can work until the login is fixed."
	case KindModelNotInPlan:
		return "The account's plan does not include the model. Re-send with a different model (the action may list the allowed ones). Do not retry the same model and do not fall back to another account."
	case KindPlanWindowExhausted:
		return "The plan's usage window is used up. Wait until resetAt, or start the work on the fallbackProfileId profile if there is one. Do not retry immediately."
	case KindTransient429:
		return "A short throttle, not a plan limit. A profile or orchestrator run is retried by the daemon on its own (twice, 30 s then 2 min); otherwise retry after a short wait. Do not switch profile and do not treat it as exhausted quota."
	case KindRateLimited:
		return "A rate limit of unknown kind (no window, no reset time). Retry after a short wait. Do not assume the plan is exhausted."
	case KindCLIExited:
		return "The agent CLI ended without a result; its session is stored. Send a follow-up prompt such as \"continue\"."
	}
	return ""
}

// ExitCode is `smind task wait`'s exit code for an error run of kind: 6 for a
// rate limit (wait or retry later), 7 when a human must fix setup (sign in, or
// choose another model), 1 otherwise.
func ExitCode(kind string) int {
	switch {
	case IsLimitKind(kind):
		return 6
	case IsHumanFix(kind):
		return 7
	}
	return 1
}
