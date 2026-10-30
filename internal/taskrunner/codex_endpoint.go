package taskrunner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacingmind/smind/internal/codex"
	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/providers"
)

// codexEndpointLaunch returns the extra codex client options and env entries
// for a codex-native instance that has an endpoint; (nil, nil, nil) for any
// other provider.
func (r *Runner) codexEndpointLaunch(provider Provider) (opts []codex.Option, env []string, err error) {
	spec, ok := r.providerRegistry().Get(string(provider))
	if !ok || !spec.HasEndpoint() {
		return nil, nil, nil
	}
	if spec.Endpoint.Kind != providers.EndpointOpenAIResponses {
		return nil, nil, fmt.Errorf("taskrunner: provider %q: endpoint kind %q is unsupported by the Codex harness (Codex only speaks the OpenAI Responses API)", spec.ID, spec.Endpoint.Kind)
	}
	envMap := r.ProviderEnv(provider)
	// PR2's secret refs plug in here.
	hasKey := envMap[codex.EndpointKeyEnv] != ""
	ep := codex.Endpoint{ID: spec.ID, Name: spec.Label, BaseURL: spec.Endpoint.BaseURL, HasKey: hasKey}
	mp, cfg, err := ep.ThreadConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("taskrunner: provider %q: %w", spec.ID, err)
	}
	opts = []codex.Option{codex.WithModelProvider(mp, cfg)}
	if envMap["CODEX_HOME"] == "" {
		dir := filepath.Join(config.Dir(), "providers", spec.ID, "codex")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("taskrunner: provider %q: codex home: %w", spec.ID, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("taskrunner: provider %q: codex home: %w", spec.ID, err)
		}
		env = []string{"CODEX_HOME=" + dir}
	}
	return opts, env, nil
}
