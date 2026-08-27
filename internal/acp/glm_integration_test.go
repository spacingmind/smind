package acp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestGLMIntegration is a bonus, opt-in check that this package's Client
// actually works against the real glm-acp-agent (via npx) rather than only
// the offline fake agent. It requires npx/Node.js, network access to
// Z.AI's API, and a configured Z.AI API key (see glm-acp-agent --setup),
// none of which this repo's main test suite can assume, so it's skipped
// unless SMIND_ACP_GLM_INTEGRATION=1 is set.
func TestGLMIntegration(t *testing.T) {
	if os.Getenv("SMIND_ACP_GLM_INTEGRATION") != "1" {
		t.Skip("set SMIND_ACP_GLM_INTEGRATION=1 to run against the real glm-acp-agent (requires npx + network + a configured Z.AI API key)")
	}

	c, err := New(GLMCommand())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	sessionID, err := c.NewSession(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	updates := make(chan SessionUpdate)
	go func() {
		for range updates {
		}
	}()

	stopReason, err := c.Prompt(ctx, sessionID, `Reply with exactly the word "pong" and nothing else.`, updates)
	if err != nil {
		// The handshake and session setup above already prove this
		// package's ACP client works against the real agent; a 429 here
		// is Z.AI's account-level rate limit, not a client bug, so it's
		// reported rather than failed.
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) && containsRateLimit(fmt.Sprintf("%s %v", rpcErr.Message, rpcErr.Data)) {
			t.Skipf("real GLM agent rate-limited (account quota, not a client bug): %v", err)
		}
		t.Fatalf("Prompt() error = %v", err)
	}
	if stopReason == "" {
		t.Fatal("Prompt() returned empty stopReason")
	}
	t.Logf("real GLM agent stopReason = %q", stopReason)
}

func containsRateLimit(s string) bool {
	for _, needle := range []string{"429", "usage limit", "rate limit"} {
		if strings.Contains(strings.ToLower(s), needle) {
			return true
		}
	}
	return false
}
