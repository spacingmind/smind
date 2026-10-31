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
// codex's UsageLimitReached, QuotaExceeded and UsageNotIncluded; unauthorized
// is a failed token refresh; serverOverloaded and rateLimitExceeded are the
// throttles. Every other class (badRequest, internalServerError, ...) is none.
func FailureHint(err error) (code string, retryAfter time.Duration) {
	var te *codex.TurnError
	if errors.As(err, &te) {
		switch te.Info {
		case "usageLimitExceeded":
			return failure.CodeUsageLimit, 0
		case "unauthorized":
			return failure.CodeUnauthorized, 0
		case "serverOverloaded":
			return failure.CodeOverloaded, 0
		case "rateLimitExceeded":
			return failure.CodeRateLimit, 0
		}
	}
	return "", 0
}
