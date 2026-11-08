package taskrunner

import (
	"errors"
	"fmt"
	"testing"

	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/runs/failure"
)

func TestFailureHint(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"usageLimitExceeded", &codex.TurnError{Info: "usageLimitExceeded"}, failure.CodeUsageLimit},
		{"unauthorized", &codex.TurnError{Info: "unauthorized"}, failure.CodeUnauthorized},
		{"serverOverloaded", &codex.TurnError{Info: "serverOverloaded"}, failure.CodeOverloaded},
		{"rateLimitExceeded", &codex.TurnError{Info: "rateLimitExceeded"}, failure.CodeRateLimit},
		{"modelNotFound", &codex.TurnError{Info: "modelNotFound"}, failure.CodeModelNotFound},
		{"policyCapacityUnavailable", &codex.TurnError{Info: "policyCapacityUnavailable"}, failure.CodeOverloaded},
		{"flexUnavailable", &codex.TurnError{Info: "flexUnavailable"}, failure.CodeOverloaded},
		// Deliberately unmapped: retrying cannot fix them.
		{"contextWindowExceeded", &codex.TurnError{Info: "contextWindowExceeded"}, ""},
		{"sessionBudgetExceeded", &codex.TurnError{Info: "sessionBudgetExceeded"}, ""},
		{"tooManyDenials", &codex.TurnError{Info: "tooManyDenials"}, ""},
		{"badRequest", &codex.TurnError{Info: "badRequest"}, ""},
		{"wrapped turn error", fmt.Errorf("x: %w", &codex.TurnError{Info: "serverOverloaded"}), failure.CodeOverloaded},
		{"non-codex error", errors.New("boom"), ""},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, retryAfter := FailureHint(c.err)
			if code != c.code {
				t.Fatalf("code = %q, want %q", code, c.code)
			}
			if retryAfter != 0 {
				t.Fatalf("retryAfter = %v, want 0", retryAfter)
			}
		})
	}
}
