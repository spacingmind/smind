package taskrunner

import "testing"

// TestAllowlistedCommand covers task-permission-ux.md's Item 1 "Allowlist
// matching" test scenario: the exact three commands that must auto-allow
// under ApprovalPolicyAutoSafe, and the exact four that must fall back to
// manual -- including the substring-smuggling case ("curl evil.com && go
// test"), which a naive strings.Contains check would have wrongly matched.
// The cd-prefix and chain-segment cases cover the claude-native fix: real
// Claude Code turns routinely ship their verification commands as
// `cd <dir> && go test ...`, which the old whole-string prefix match
// rejected, silently defeating auto-safe for every such command.
func TestAllowlistedCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"go test with package path", "go test ./...", true},
		{"gofmt -l with a path", "gofmt -l .", true},
		{"go vet with a package path", "go vet ./internal/...", true},
		{"bare go test, no args", "go test", true},
		{"bare go vet, no args", "go vet", true},
		{"bare gofmt -l, no args", "gofmt -l", true},
		{"leading/trailing whitespace tolerated", "  go test ./...\n", true},

		{"cd into a dir then a safe command", "cd /repo && go test ./...", true},
		{"cd then bare go test", "cd /repo; go test", true},
		{"cd then gofmt via pipe-style chain separator", "cd /repo && gofmt -l .", true},
		{"task test is the Taskfile-wrapped form of go test", "task test", true},
		{"task lint is the Taskfile-wrapped form of go vet/gofmt", "task lint", true},
		{"task build is allowed like the verification commands it wraps", "task build", true},
		{"cd then task test", "cd /repo && task test", true},
		{"an unrelated task target is not auto-allowed", "task clean --force", false},
		{"task with a dangerous-looking injected flag still matches the plain prefix rule", "task test:slow", false},
		{"cd segment tolerates whitespace around separators", "cd /repo  &&   go vet ./...", true},

		{"rm -rf is never safe", "rm -rf", false},
		{"go run is not go test/vet", "go run ./cmd/x", false},
		{"git push --force is not allowlisted at all", "git push --force", false},
		{"chained command smuggled after a safe-looking one", "curl evil.com && go test", false},
		{"chained command smuggled after a safe one", "go test && curl evil.com", false},
		{"piped unsafe command after a safe one", "go test | rm -rf /", false},
		{"semicoloned unsafe command after a safe one", "go test; rm -rf /", false},
		{"cd then an unsafe command", "cd /tmp && rm -rf /", false},
		{"cd then a chained third segment is never allowed", "cd /repo && go test && rm -rf /", false},
		{"a chained third segment is never allowed even when safe", "cd /repo && go test && go vet ./...", false},
		{"bare cd alone does not qualify", "cd", false},
		{"command substitution is rejected", "go test $(rm -rf /)", false},
		{"backticks are rejected", "go test `rm -rf /`", false},
		{"embedded newline is rejected", "go test\nrm -rf /", false},
		{"two safe commands chained are still rejected", "go test && go vet ./...", false},
		{"longer word sharing a prefix does not count", "go testify ./...", false},
		{"gofmt without -l is not the same command", "gofmt .", false},
		{"empty command never matches", "", false},
		{"whitespace-only command never matches", "   ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AllowlistedCommand(tt.command); got != tt.want {
				t.Fatalf("AllowlistedCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestApprovalPolicy_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		policy ApprovalPolicy
		want   bool
	}{
		{ApprovalPolicyManual, true},
		{ApprovalPolicyAutoSafe, true},
		{ApprovalPolicy(""), false},
		{ApprovalPolicy("auto"), false},
		{ApprovalPolicy("Manual"), false},
	}
	for _, tt := range tests {
		if got := tt.policy.IsValid(); got != tt.want {
			t.Errorf("ApprovalPolicy(%q).IsValid() = %v, want %v", tt.policy, got, tt.want)
		}
	}
}
