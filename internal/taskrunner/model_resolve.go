package taskrunner

import "fmt"

// NoModelError refuses a claude-native launch that resolved no model: an
// unpinned `claude` takes the CLI's own default, which may be Opus. The
// message starts with the code "no_model".
type NoModelError struct {
	// Provider is the provider the run was for; Base is the built-in whose
	// providers.<id>.defaultModel the remedy names (differs for an instance).
	Provider, Base Provider
}

func (e *NoModelError) Error() string {
	return fmt.Sprintf("no_model: no model resolved for %s: pass --model, set the profile's model, or set providers.%s.defaultModel (the Claude CLI's own default may be Opus; \"default\" keeps it and is for a human to ask for)", e.Provider, e.Base)
}

// ResolveModel is the model a run of provider launches with: the requested
// one, else provider's configured default (an instance falls back to its base
// provider's), else "" -- and for a claude-native run, "" is refused with a
// *NoModelError instead of letting the CLI pick its own. Other providers keep
// their own default when nothing resolves (ADR-0025).
func (r *Runner) ResolveModel(provider Provider, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	if m := r.DefaultModel(provider); m != "" {
		return m, nil
	}
	base := Provider(r.providerRegistry().BaseOf(string(provider)))
	if base == "" {
		base = provider
	}
	if m := r.DefaultModel(base); m != "" {
		return m, nil
	}
	if r.isClaude(provider) {
		return "", &NoModelError{Provider: provider, Base: base}
	}
	return "", nil
}

// noModel is the refusal for a claude launch that reached the launcher with no
// model: the last line of defence behind ResolveModel, so no spawn path
// (user, isolated, probe) can omit --model.
func (r *Runner) noModel(provider Provider) error {
	base := Provider(r.providerRegistry().BaseOf(string(provider)))
	if base == "" {
		base = provider
	}
	return &NoModelError{Provider: provider, Base: base}
}
