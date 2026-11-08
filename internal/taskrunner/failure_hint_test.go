package taskrunner

import (
	"errors"
	"fmt"
	"testing"
	"time"

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

// TestFailureHint_NewCodexInfos: the three human-action CodexErrorInfo values
// map to their typed codes (and through Classify to their kinds), never to a
// retryable throttle.
func TestFailureHint_NewCodexInfos(t *testing.T) {
	for info, wantCode := range map[string]string{
		"contextWindowExceeded": failure.CodeContextWindow,
		"sessionBudgetExceeded": failure.CodeSessionBudget,
		"tooManyDenials":        failure.CodeTooManyDenials,
	} {
		t.Run(info, func(t *testing.T) {
			code, retryAfter := FailureHint(&codex.TurnError{Info: info})
			if code != wantCode {
				t.Fatalf("code = %q, want %q", code, wantCode)
			}
			if retryAfter != 0 {
				t.Fatalf("retryAfter = %v, want 0", retryAfter)
			}
			r := failure.Classify(failure.Input{Code: code}, time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC))
			if !failure.IsHumanFix(r.Kind) {
				t.Fatalf("Classify(code %q) = %q, want a human-fix kind", code, r.Kind)
			}
		})
	}
}
