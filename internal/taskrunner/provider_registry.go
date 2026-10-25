package taskrunner

import (
	"sort"

	"github.com/spacingmind/smind/internal/providers"
)

// WithProviderRegistry makes the Runner read reg instead of providers.Default
// (tests that register a fake provider without touching the process-wide table).
func WithProviderRegistry(reg *providers.Registry) Option {
	return func(r *Runner) { r.registry = reg }
}

// providerRegistry is the table this Runner dispatches from.
func (r *Runner) providerRegistry() *providers.Registry {
	if r.registry != nil {
		return r.registry
	}
	return providers.Default()
}

// runnerOf is how provider's turns run ("" for an unknown provider).
func (r *Runner) runnerOf(provider Provider) providers.Runner {
	return r.providerRegistry().RunnerOf(string(provider))
}

func (r *Runner) isClaude(provider Provider) bool { return r.runnerOf(provider) == providers.RunnerClaude }

func (r *Runner) isACP(provider Provider) bool { return r.runnerOf(provider) == providers.RunnerACP }

// acpCommandFor is the argv spawned for an ACP provider: a WithACPCommand
// override (tests, daemon flags) first, else the registry entry's command.
func (r *Runner) acpCommandFor(provider Provider) ([]string, bool) {
	if c, ok := r.acpCommands[provider]; ok {
		return c, true
	}
	s, ok := r.providerRegistry().Get(string(provider))
	if !ok || s.Runner != providers.RunnerACP || len(s.Command) == 0 {
		return nil, false
	}
	return s.Command, true
}

// codexCommandFor is the argv of a codex-native provider (an instance runs the
// same command): the WithCodexCommand override, else the registry entry's.
func (r *Runner) codexCommandFor(provider Provider) []string {
	if r.codexCommand != nil {
		return r.codexCommand
	}
	s, _ := r.providerRegistry().Get(string(provider))
	return s.Command
}

// acpEnvEntries is a custom ACP provider's configured command environment
// (providers.custom[].env) as sorted NAME=VALUE entries; nil for built-ins.
func (r *Runner) acpEnvEntries(provider Provider) []string {
	s, ok := r.providerRegistry().Get(string(provider))
	if !ok || s.Runner != providers.RunnerACP {
		return nil
	}
	entries := make([]string, 0, len(s.Env))
	for k, v := range s.Env {
		entries = append(entries, k+"="+v)
	}
	sort.Strings(entries)
	return entries
}

// Providers is the registry this Runner dispatches from (the daemon's
// providers.Default unless WithProviderRegistry replaced it).
func (r *Runner) Providers() *providers.Registry { return r.providerRegistry() }
