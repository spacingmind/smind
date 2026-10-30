package taskrunner

import (
	"fmt"
	"os"
	"sort"

	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"
)

// ProviderSecretSource is the slice of *store.Store the Runner resolves a
// provider instance's stored secrets from (ADR-0046 section 4); *store.Store
// satisfies it.
type ProviderSecretSource interface {
	GetProviderSecret(instanceID, name string) (string, bool, error)
	ListProviderSecrets(instanceID string) (map[string]store.ProviderSecretInfo, error)
	PruneProviderSecrets(keepInstanceIDs []string) (int, error)
}

// WithProviderSecrets sets the store the Runner resolves a spec's SecretEnv
// refs from at launch. Without one, a spec that references a secret fails
// the launch instead of silently dropping the key.
func WithProviderSecrets(src ProviderSecretSource) Option {
	return func(r *Runner) { r.providerSecrets = src }
}

// launchEnvEntries is everything a launch of provider appends after the
// daemon's own environment, in last-wins order: the literal launch env
// (an instance's spec.Env, providers.<id>.env), then one entry per
// SecretEnv ref resolved from the secret store, then the PassthroughEnv
// names found in the daemon's environment (absent ones are skipped). Each
// group is sorted by name. A resolved value reaches the child process env
// only -- never argv, never a log line, never an error string.
func (r *Runner) launchEnvEntries(provider Provider) ([]string, error) {
	spec, _ := r.providerRegistry().Get(string(provider))
	var literals []string
	if spec.Runner == providers.RunnerACP {
		literals = r.acpEnvEntries(provider)
	} else {
		literals = r.providerEnvEntries(provider)
	}
	entries := make([]string, 0, len(literals)+len(spec.SecretEnv)+len(spec.PassthroughEnv))
	entries = append(entries, literals...)

	if len(spec.SecretEnv) > 0 && r.providerSecrets == nil {
		return nil, fmt.Errorf("provider %q: the secret store is not available", provider)
	}
	names := make([]string, 0, len(spec.SecretEnv))
	for name := range spec.SecretEnv {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, envName := range names {
		secretName := spec.SecretEnv[envName]
		value, ok, err := r.providerSecrets.GetProviderSecret(string(provider), secretName)
		if err != nil {
			return nil, fmt.Errorf("provider %q: read secret %q (for %s): %w", provider, secretName, envName, err)
		}
		if !ok {
			return nil, fmt.Errorf("provider %q: secret %q (for %s) is not set; add it in Settings > Providers", provider, secretName, envName)
		}
		entries = append(entries, envName+"="+value)
	}

	pass := append([]string(nil), spec.PassthroughEnv...)
	sort.Strings(pass)
	for _, name := range pass {
		if value, ok := os.LookupEnv(name); ok {
			entries = append(entries, name+"="+value)
		}
	}
	return entries, nil
}

// ProviderSecretInfo lists an instance's stored secrets by name, each with
// only a fingerprint of its value. Nil when no source is configured or the
// listing fails.
func (r *Runner) ProviderSecretInfo(instanceID string) map[string]store.ProviderSecretInfo {
	if r.providerSecrets == nil {
		return nil
	}
	info, err := r.providerSecrets.ListProviderSecrets(instanceID)
	if err != nil {
		return nil
	}
	return info
}

// PruneProviderSecrets drops the stored secrets of every provider no longer
// in the registry, keeping those of every custom spec, and returns how many
// rows went. A no-op (0, nil) without a source.
func (r *Runner) PruneProviderSecrets() (int, error) {
	if r.providerSecrets == nil {
		return 0, nil
	}
	var keep []string
	for _, s := range r.providerRegistry().List() {
		if s.Custom {
			keep = append(keep, s.ID)
		}
	}
	return r.providerSecrets.PruneProviderSecrets(keep)
}
