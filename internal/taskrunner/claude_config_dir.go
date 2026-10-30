package taskrunner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
)

// claudeConfigDirEntries is the private CLAUDE_CONFIG_DIR a claude-code
// instance with a compatible endpoint (ANTHROPIC_BASE_URL set) launches
// with, so its login/config never lands in ~/.claude (the same private-home
// rule codexEndpointLaunch applies to CODEX_HOME). Nil -- changing nothing
// -- for anything else: a non-claude-code provider, the base provider, an
// instance that opted into ~/.claude (InheritClaudeConfig), an endpoint
// without a base URL, or one that already pins its own dir.
func (r *Runner) claudeConfigDirEntries(spec providers.Spec, provider Provider) ([]string, error) {
	if spec.Harness() != providers.HarnessClaudeCode || !spec.IsInstance() || spec.InheritClaudeConfig {
		return nil, nil
	}
	env := r.ProviderEnv(provider)
	if env["ANTHROPIC_BASE_URL"] == "" || env["CLAUDE_CONFIG_DIR"] != "" {
		return nil, nil
	}
	dir := filepath.Join(config.Dir(), "providers", spec.ID, "claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("provider %q: claude config dir: %w", spec.ID, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("provider %q: claude config dir: %w", spec.ID, err)
	}
	return []string{"CLAUDE_CONFIG_DIR=" + dir}, nil
}
