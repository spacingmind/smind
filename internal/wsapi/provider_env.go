package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Where an effective launch-env path comes from.
const (
	envSourceSettings    = "settings"    // providers.<id>.env in smind's config
	envSourceEnvironment = "environment" // the daemon's own process environment
	envSourceDefault     = "default"     // the CLI's own default (~/.claude, ~/.codex)
)

// providerEnvEffective is the path a launch of the provider will use for one
// env key, where it comes from, and whether the directory exists. Only the
// path and a stat result: never a file's contents.
type providerEnvEffective struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Exists bool   `json:"exists"`
}

// providerEnvView is the additive launch-env part of provider.list entries
// and of the provider.updated payload. Paths only.
type providerEnvView struct {
	// Env is the configured env (providers.<id>.env), key -> path.
	Env map[string]string `json:"env,omitempty"`
	// EnvDirExists reports, per configured key, whether the directory exists.
	EnvDirExists map[string]bool `json:"envDirExists,omitempty"`
	// EnvEffective is, per allowlisted key, what a launch will use.
	EnvEffective map[string]providerEnvEffective `json:"envEffective,omitempty"`
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// effectiveProviderEnv resolves one key: the configured value, else the
// daemon's own environment, else the CLI default under the user's home.
func effectiveProviderEnv(runner *taskrunner.Runner, provider string, k config.ProviderEnvKey) providerEnvEffective {
	var path, source string
	if v := runner.ProviderEnv(taskrunner.Provider(provider))[k.Key]; v != "" {
		path, source = v, envSourceSettings
	} else if v := os.Getenv(k.Key); v != "" {
		path, source = v, envSourceEnvironment
	} else {
		source = envSourceDefault
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, k.DefaultDir)
		} else {
			path = "~/" + k.DefaultDir
		}
	}
	return providerEnvEffective{Path: path, Source: source, Exists: dirExists(path)}
}

// providerEnvViewFor builds the launch-env view of provider; the zero view
// for a provider with no allowlisted keys.
func providerEnvViewFor(runner *taskrunner.Runner, provider string) providerEnvView {
	var v providerEnvView
	for _, k := range config.ProviderEnvKeys[provider] {
		if v.EnvEffective == nil {
			v.EnvEffective = make(map[string]providerEnvEffective)
		}
		v.EnvEffective[k.Key] = effectiveProviderEnv(runner, provider, k)
		if val := runner.ProviderEnv(taskrunner.Provider(provider))[k.Key]; val != "" {
			if v.Env == nil {
				v.Env, v.EnvDirExists = make(map[string]string), make(map[string]bool)
			}
			v.Env[k.Key] = val
			v.EnvDirExists[k.Key] = dirExists(val)
		}
	}
	return v
}

// providerEnvDetail is the line provider.test appends for a provider with a
// launch-env setting, e.g. "config dir: /Users/x/.claude-scopedocs (from smind
// settings)". It names only the path and whether the directory exists.
func providerEnvDetail(runner *taskrunner.Runner, provider string) string {
	var parts []string
	for _, k := range config.ProviderEnvKeys[provider] {
		eff := effectiveProviderEnv(runner, provider, k)
		var src string
		switch eff.Source {
		case envSourceSettings:
			src = "from smind settings"
		case envSourceEnvironment:
			src = "from the daemon's environment"
		default:
			src = "default ~/" + k.DefaultDir
		}
		part := fmt.Sprintf("%s: %s (%s)", k.Short, eff.Path, src)
		if !eff.Exists {
			part += " [directory does not exist]"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

// handleProviderSetEnv sets providers.<id>.env.<key> live: it validates the
// key against the allowlist and the value as an absolute path
// (config.ValidateProviderEnv), writes config.yaml through
// config.SetProviderEnv, and only after that updates the Runner and publishes
// provider.updated. A null or empty value clears the key. The next launch of
// the provider uses it; a run already started keeps its environment. Humans
// only: a run-scoped (agent) caller is refused, and no MCP tool exposes it.
func handleProviderSetEnv(runner *taskrunner.Runner, bus *eventBus) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "provider.setEnv is not available to agents"}
		}
		var p struct {
			Provider string  `json:"provider"`
			Key      string  `json:"key"`
			Value    *string `json:"value"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provider.setEnv: invalid params: %w", err)
		}
		value := ""
		if p.Value != nil {
			value = strings.TrimSpace(*p.Value)
		}
		providerSetDefaultModelMu.Lock()
		defer providerSetDefaultModelMu.Unlock()
		stored, err := config.SetProviderEnv(p.Provider, p.Key, value)
		if err != nil {
			return nil, fmt.Errorf("provider.setEnv: %w", err)
		}
		runner.SetProviderEnv(taskrunner.Provider(p.Provider), p.Key, stored)
		res := newProviderUpdatedPayload(runner, p.Provider)
		if bus != nil {
			bus.Publish(Event{Topic: TopicProviderUpdated, Payload: res})
		}
		return res, nil
	}
}
