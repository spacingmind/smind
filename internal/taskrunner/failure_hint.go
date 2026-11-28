package taskrunner

import (
	"errors"
	"time"

	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/runs/failure"
)

// FailureHint reads the typed error class a harness reported with a failed
// turn, for failure.Classify (Input.Code, Input.RetryAfter). "" = the backend
// gave no structure, so classification falls back to the error text.
//
// Codex: the app-server's CodexErrorInfo (refs/codex
// app-server-protocol/src/protocol/v2/shared.rs). usageLimitExceeded covers
// codex's UsageLimitReached, QuotaExceeded and UsageNotIncluded, and the
// spend-limit names apiKeySpendLimitExceeded, projectSpendLimitExceeded and
// organizationSpendLimitExceeded (Codex rust-v0.172.0; org and project limits
// used to arrive as usageLimitExceeded); unauthorized
// is a failed token refresh; serverOverloaded and rateLimitExceeded are the
// throttles; modelNotFound is the plan not including the model;
// policyCapacityUnavailable and flexUnavailable are capacity temporarily
// unavailable (retryable). contextWindowExceeded, sessionBudgetExceeded and
// tooManyDenials map to their own classes: retrying cannot fix them, a human
// must act (a new session, fewer denials). inferenceHookDenied (a configured
// inference hook vetoed the request) and bioPolicy (the provider's own safety
// refusal) are deliberately unmapped: no class fits them honestly, retrying
// or failing over cannot fix a policy refusal, and the provider's own message
// is what the run shows. Every other class (badRequest,
// internalServerError, ...) is none.
func FailureHint(err error) (code string, retryAfter time.Duration) {
	var te *codex.TurnError
	if errors.As(err, &te) {
		switch te.Info {
		case "usageLimitExceeded", "apiKeySpendLimitExceeded", "projectSpendLimitExceeded", "organizationSpendLimitExceeded":
			return failure.CodeUsageLimit, 0
		case "unauthorized":
			return failure.CodeUnauthorized, 0
		case "serverOverloaded":
			return failure.CodeOverloaded, 0
		case "rateLimitExceeded":
			return failure.CodeRateLimit, 0
		case "modelNotFound":
			return failure.CodeModelNotFound, 0
		case "policyCapacityUnavailable", "flexUnavailable":
			return failure.CodeOverloaded, 0
		case "contextWindowExceeded":
			return failure.CodeContextWindow, 0
		case "sessionBudgetExceeded":
			return failure.CodeSessionBudget, 0
		case "tooManyDenials":
			return failure.CodeTooManyDenials, 0
		}
	}
	return "", 0
}
