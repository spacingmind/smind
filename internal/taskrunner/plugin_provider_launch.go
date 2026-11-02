package taskrunner

import (
	"fmt"
	"os"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/pluginagent"
	"github.com/spacingmind/smind/internal/providers"
)

// pluginLaunch is the ONLY way a plugin provider's agent is started (Wave D,
// docs/plans/active/plugins-wave-d.md, PD2): the command is re-resolved and
// re-confined at this very call (a plugin file swapped since registration is
// refused), the environment is BUILT (never the daemon's: no SMIND_*, no
// tokens, a private HOME, the fixed PATH), and the plugin's own secret slots
// are resolved from the secret store for THIS provider id only. The returned
// options make the child get exactly that environment, with dir as its cwd.
func (r *Runner) pluginLaunch(spec providers.Spec, dir string) (command []string, opts []acp.Option, err error) {
	command, err = pluginagent.ResolveCommand(spec)
	if err != nil {
		return nil, nil, err
	}
	home, _ := os.UserHomeDir()
	env, err := pluginagent.LaunchEnv(spec, pluginagent.EnvOptions{
		Environ:  os.Environ(),
		RealHome: home,
		Secret: func(name string) (string, bool) {
			if r.providerSecrets == nil {
				return "", false
			}
			v, ok, gerr := r.providerSecrets.GetProviderSecret(spec.ID, name)
			if gerr != nil || !ok {
				return "", false
			}
			return v, true
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return command, []acp.Option{acp.WithCleanEnv(), acp.WithEnv(env...), acp.WithDir(dir)}, nil
}

// errPluginLaunchBypass is what the generic launch env builder answers for a
// plugin provider: it must never build the env of one (it would inherit the
// daemon's), so a caller that did not branch to pluginLaunch fails closed.
func errPluginLaunchBypass(provider Provider, plugin string) error {
	return fmt.Errorf("provider %q (plugin %s): the generic launch env is not used for plugin providers", provider, plugin)
}
