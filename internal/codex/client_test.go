package codex

import (
	"context"
	"testing"
	"time"
)

// TestClient_HandshakeAndStreamingPrompt drives a full initialize ->
// thread/start -> turn/start turn against the fake agent's default
// scenario, proving the async turn/completed correlation (not turn/start's
// own response) is what actually unblocks Prompt, and that both deltas
// arrive on updates in order before it does.
func TestClient_HandshakeAndStreamingPrompt(t *testing.T) {
	t.Parallel()
	c, cwd := newTestClient(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	threadID, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	updates := make(chan Update)
	type promptResult struct {
		stopReason string
		err        error
	}
	resultCh := make(chan promptResult, 1)
	go func() {
		stopReason, err := c.Prompt(ctx, threadID, "hi", updates)
		resultCh <- promptResult{stopReason, err}
	}()

	var texts []string
	for u := range updates {
		texts = append(texts, u.Text)
	}

	want := []string{"Hello, ", "world!"}
	if len(texts) != len(want) {
		t.Fatalf("updates = %v, want %v", texts, want)
	}
	for i, w := range want {
		if texts[i] != w {
			t.Fatalf("updates[%d] = %q, want %q", i, texts[i], w)
		}
	}

	res := <-resultCh
	if res.err != nil {
		t.Fatalf("Prompt() error = %v", res.err)
	}
	if res.stopReason != "completed" {
		t.Fatalf("Prompt() stopReason = %q, want %q", res.stopReason, "completed")
	}
}

// blockingCommandPolicy blocks DecideCommandExecution until release is
// closed, so a test can control exactly when the fake agent's approval call
// resolves -- the same synchronization role internal/acp's tests use a
// dedicated "_test/release" notification for, but here it's the real
// approval round-trip itself doing double duty as both the thing under test
// and the gate.
type blockingCommandPolicy struct {
	release chan struct{}
	got     chan CommandExecutionApprovalRequest
	accept  bool
}

func (p blockingCommandPolicy) DecideCommandExecution(_ context.Context, req CommandExecutionApprovalRequest) (bool, error) {
	p.got <- req
	<-p.release
	return p.accept, nil
}

func (p blockingCommandPolicy) DecideFileChange(_ context.Context, _ FileChangeApprovalRequest) (bool, error) {
	return true, nil
}

// TestClient_CommandExecutionApproval_OrderingAndDecision proves two things
// at once: (1) the real request/summary reaches PermissionPolicy.
// DecideCommandExecution (command, cwd), and (2) no update is delivered
// early -- the fake agent's "decision:" delta and turn/completed
// notification are only sent after the approval call actually resolves, so
// observing them requires this test to have released the policy first, the
// same ordering guarantee internal/acp's release-gate tests prove for its
// own protocol.
func TestClient_CommandExecutionApproval_OrderingAndDecision(t *testing.T) {
	t.Parallel()
	policy := blockingCommandPolicy{
		release: make(chan struct{}),
		got:     make(chan CommandExecutionApprovalRequest, 1),
		accept:  true,
	}
	c, cwd := newTestClient(t, "permission", WithPermissionPolicy(policy))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	threadID, err := c.NewSession(ctx, cwd)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	updates := make(chan Update)
	resultCh := make(chan struct {
		stopReason string
		err        error
	}, 1)
	go func() {
		stopReason, err := c.Prompt(ctx, threadID, "hi", updates)
		resultCh <- struct {
			stopReason string
			err        error
		}{stopReason, err}
	}()

	select {
	case req := <-policy.got:
		if req.Command != "echo hi" || req.Cwd != cwd {
			t.Fatalf("approval request = %+v, want Command=%q Cwd=%q", req, "echo hi", cwd)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for DecideCommandExecution to be called")
	}

	// Prove nothing has been delivered yet, while the policy is still
	// deciding -- exactly what would break if the fake agent (or Client)
	// sent the "decision:" delta before the approval round-trip actually
	// completed.
	select {
	case u := <-updates:
		t.Fatalf("received update %+v before releasing the approval decision", u)
	case <-time.After(200 * time.Millisecond):
	}

	close(policy.release)

	var texts []string
	for u := range updates {
		texts = append(texts, u.Text)
	}
	if len(texts) != 1 || texts[0] != "decision:accept" {
		t.Fatalf("updates after release = %v, want [%q]", texts, "decision:accept")
	}

	res := <-resultCh
	if res.err != nil {
		t.Fatalf("Prompt() error = %v", res.err)
	}
	if res.stopReason != "completed" {
		t.Fatalf("Prompt() stopReason = %q, want %q", res.stopReason, "completed")
	}
}
