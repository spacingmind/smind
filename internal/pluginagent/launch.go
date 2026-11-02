package pluginagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/providers"
)

// providerPartRe is the <provider> part of a plugin provider id: it becomes
// the one path component under the plugin's data directory, so it is strict
// lowercase and never the full id.
var providerPartRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// reservedEnv are the env names a plugin's literal env may not set: they are
// built by BaseEnv, and a plugin value must not override them.
var reservedEnv = []string{"PATH", "HOME", "USERPROFILE", "TMPDIR", "TEMP", "TMP"}

func errPrefix(spec providers.Spec) string {
	return fmt.Sprintf("provider %s (plugin %s): ", spec.ID, spec.Plugin)
}

// ResolveCommand returns spec.Command with argv[0] made absolute: CommandForm
// "path" -> ConfinedExecutable(spec.PluginRoot, spec.Command[0]) (re-checked
// at EVERY call); "bare" -> LookPath(spec.Command[0], SafePath(os.Environ())),
// found only on the fixed PATH. Any other form, or an empty Command, is an
// error. Errors start with `provider <id> (plugin <plugin>): command `.
func ResolveCommand(spec providers.Spec) ([]string, error) {
	if len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return nil, fmt.Errorf("%scommand is empty", errPrefix(spec))
	}
	var exe string
	var err error
	switch spec.CommandForm {
	case "path":
		exe, err = ConfinedExecutable(spec.PluginRoot, spec.Command[0])
	case "bare":
		exe, err = LookPath(spec.Command[0], SafePath(os.Environ()))
	default:
		return nil, fmt.Errorf("%scommand form %q is not a plugin command form", errPrefix(spec), spec.CommandForm)
	}
	if err != nil {
		return nil, fmt.Errorf("%scommand %q: %w", errPrefix(spec), spec.Command[0], err)
	}
	argv := append([]string(nil), spec.Command...)
	argv[0] = exe
	return argv, nil
}

// EnvOptions is what LaunchEnv builds the child environment from.
type EnvOptions struct {
	// Environ is the daemon's environ; only passthrough names are used.
	Environ []string
	// RealHome is the daemon user's real HOME, used only when
	// spec.InheritHome.
	RealHome string
	// Secret resolves a secret slot, may be nil.
	Secret func(name string) (string, bool)
	// SkipSecrets: do not resolve or require secrets (version probe).
	SkipSecrets bool
}

// LaunchEnv builds the COMPLETE child environment: BaseEnv with HOME/tmp
// under spec.PluginData/providers/<providerPart>/{home,tmp} (both created
// 0700; providerPart is the id after "<plugin>/" and MUST match
// ^[a-z][a-z0-9-]{0,63}$ -- the full id is NEVER a path component), or
// HOME=RealHome when spec.InheritHome (tmp stays the private one); then the
// spec.Env entries sorted by name, skipping PATH HOME USERPROFILE TMPDIR
// TEMP TMP; then for each SecretSlot sorted by name, Secret(name) found ->
// NAME=value, missing and required -> error, missing and optional -> skipped.
// With SkipSecrets no secret is added at all. It never reads os.Environ
// directly.
func LaunchEnv(spec providers.Spec, o EnvOptions) ([]string, error) {
	part := strings.TrimPrefix(spec.ID, spec.Plugin+"/")
	if spec.Plugin == "" || !providerPartRe.MatchString(part) {
		return nil, fmt.Errorf("%sprovider id %q does not name a safe per-provider directory", errPrefix(spec), spec.ID)
	}
	if spec.InheritHome && o.RealHome == "" {
		return nil, fmt.Errorf("%sinheritHome is set but the daemon user's home is unknown", errPrefix(spec))
	}
	base := filepath.Join(spec.PluginData, "providers", part)
	home, tmp := filepath.Join(base, "home"), filepath.Join(base, "tmp")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("%screate %s: %w", errPrefix(spec), home, err)
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, fmt.Errorf("%screate %s: %w", errPrefix(spec), tmp, err)
	}
	if spec.InheritHome {
		home = o.RealHome
	}
	out := BaseEnv(o.Environ, home, tmp)

	names := make([]string, 0, len(spec.Env))
	for name := range spec.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if nameIn(name, reservedEnv) {
			continue
		}
		out = append(out, name+"="+spec.Env[name])
	}

	if !o.SkipSecrets {
		slots := append([]providers.SecretSlot(nil), spec.SecretSlots...)
		sort.Slice(slots, func(i, j int) bool { return slots[i].Name < slots[j].Name })
		for _, slot := range slots {
			var value string
			var ok bool
			if o.Secret != nil {
				value, ok = o.Secret(slot.Name)
			}
			if !ok {
				if slot.Required {
					return nil, fmt.Errorf("%srequired secret %s is not set", errPrefix(spec), slot.Name)
				}
				continue
			}
			out = append(out, slot.Name+"="+value)
		}
	}
	return out, nil
}

// RunVersion executes the resolved command once with arg: cwd =
// spec.PluginRoot, env = LaunchEnv(SkipSecrets), 3s timeout,
// exec.CommandContext (no shell). It returns the trimmed CombinedOutput, ""
// on any error.
func RunVersion(spec providers.Spec, arg string, environ []string) string {
	argv, err := ResolveCommand(spec)
	if err != nil {
		return ""
	}
	env, err := LaunchEnv(spec, EnvOptions{Environ: environ, SkipSecrets: true})
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], append(argv[1:], arg)...)
	cmd.Dir = spec.PluginRoot
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
