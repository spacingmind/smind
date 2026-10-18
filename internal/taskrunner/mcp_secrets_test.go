package taskrunner

import (
	"sort"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func TestMcpSecrets_EnvHeadersArgs(t *testing.T) {
	t.Parallel()
	got := McpSecrets([]store.McpServer{{
		Env:     `{"API_KEY":"sk-secret-1","SHORT":"on"}`,
		Headers: `{"Authorization":"Bearer tok-abc123"}`,
		Args:    `["-y","--token=tkn-xyz789","positional-secret","--flag"]`,
	}})
	sort.Strings(got)
	want := map[string]bool{"sk-secret-1": true, "Bearer tok-abc123": true, "tok-abc123": true, "tkn-xyz789": true, "positional-secret": true}
	if len(got) != len(want) {
		t.Fatalf("McpSecrets = %q, want %d entries", got, len(want))
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("unexpected secret %q in %q", s, got)
		}
		if len(s) < MinSecretLen {
			t.Fatalf("secret %q shorter than MinSecretLen", s)
		}
	}
}
