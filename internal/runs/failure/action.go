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
