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
	// The picker gate's kinds (ADR-0050 twin, internal/gate): clear a
	// provider cooldown the human has reset, or pick another provider.
	ActionClearCooldown = "clear_cooldown"
	ActionPickProvider  = "pick_provider"
	// The human-action failure kinds' kinds: a fresh chat (the conversation
	// outgrew the context window), the provider's own session budget, and the
	// permission policy. A client renders Label for a kind it does not know.
	ActionNewChat      = "new_chat"
	ActionRaiseBudget  = "raise_budget"
	ActionReviewPolicy = "review_policy"
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
	// Summary is one plain sentence saying what happened (Describe of the
	// failure kind), so a client can render the failure without a private
	// label map.
	Summary string `json:"summary,omitempty"`
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
// this daemon does not know. Every action carries the class's plain sentence
// as Summary.
func NextAction(kind string, c ActionContext) *Action {
	act := nextActionOf(kind, c)
	if act != nil {
		act.Summary = Describe(kind)
	}
	return act
}

func nextActionOf(kind string, c ActionContext) *Action {
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
	case KindContextWindowExceeded:
		return &Action{Kind: ActionNewChat, Target: "chat:new", Label: "Start a fresh chat for this task"}
	case KindSessionBudgetExceeded:
		return &Action{Kind: ActionRaiseBudget, Label: "Raise the session budget in the provider's settings, or start a fresh chat"}
	case KindTooManyDenials:
		return &Action{Kind: ActionReviewPolicy, Target: "settings:permission-policy", Label: "Review the permission policy"}
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
	case KindContextWindowExceeded:
		return "The conversation no longer fits the model's context window. Start a fresh chat for the task; do not retry the same chat and do not fall back to another account."
	case KindSessionBudgetExceeded:
		return "The provider's own session budget for this run is used up (not smind's tree budget). A human raises it in the provider's settings or starts a fresh chat; do not retry and do not fall back."
	case KindTooManyDenials:
		return "The provider stopped the run after too many denied actions. A human should review the permission policy; do not retry the same run and do not widen permissions from inside the run."
	case KindCLIExited:
		return "The agent CLI ended without a result; its session is stored. Send a follow-up prompt such as \"continue\"."
	}
	return ""
}

// Describe is the daemon's one plain human sentence for a failed run of kind
// ("" for an empty or unknown kind): it says what happened, never what to do
// next (that is the Action and the Guidance). One table, so web, mobile, MCP
// and the CLI render the same words.
func Describe(kind string) string {
	switch kind {
	case KindPlanWindowExhausted:
		return "The plan's usage window is used up; it resets later."
	case KindTransient429:
		return "The provider throttled the request for a moment."
	case KindRateLimited:
		return "The provider limited how fast requests can be sent."
	case KindAuthExpired:
		return "The provider no longer accepts the sign-in."
	case KindModelNotInPlan:
		return "The account's plan does not include this model."
	case KindContextWindowExceeded:
		return "The conversation no longer fits the model's context window."
	case KindSessionBudgetExceeded:
		return "The provider's session budget for this run is used up."
	case KindTooManyDenials:
		return "The agent kept asking for actions that were denied, so the provider stopped the run."
	case KindCLIExited:
		return "The agent stopped before finishing; its session is saved, so you can continue where it left off."
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
