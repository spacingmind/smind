package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file is the one allowlist table of ADR-0062 section 3: the only keys
// config.get may answer and (its writable subset) config.set may change.
// Everything not listed is invisible to both RPCs, and no row is a secret.

// ConfigKey is one row of the allowlist.
type ConfigKey struct {
	// Key is the yaml path, as the CLI writes it.
	Key string
	// Kind is the value's type. Writable rows are bools; read-only rows use
	// the kind that describes their value.
	Kind keyKind
	// Default is the built-in default, formatted as `smind config get`
	// prints it.
	Default string
	// Writable says config.set accepts this key (human-only, loopback-only).
	Writable bool
	// Restart says a change needs a daemon restart to take effect.
	Restart bool
	// LoopbackOnly marks the network-topology rows (the server.* keys):
	// config.get returns them only to a loopback connection.
	LoopbackOnly bool
	// Env is the environment variable that overrides the file value ("" when
	// none does). Only the variable's NAME is ever reported.
	Env string
	// Confirm is the one sentence a person sees before turning a writable
	// key on ("" for read-only rows).
	Confirm string
}

// kindFloat is the kind of the read-only budget ceilings (floats in the
// config structs). config.Set never sees it: those rows are not writable.
const kindFloat keyKind = 100

// configKeys is the table of ADR-0062 section 3, in its order. The defaults
// come from Default() and the Default* constants, not from prose.
var configKeys = []ConfigKey{
	// Writable: config.set and config.get (human-only, loopback-only).
	{Key: "automations.enabled", Kind: kindBool, Default: "false", Writable: true,
		Confirm: "Turning automations on arms the engine: it polls schedules and fires runs without you asking."},
	{Key: "automations.paused", Kind: kindBool, Default: "false", Writable: true,
		Confirm: "Pausing the engine records every signal as paused and fires nothing until you resume."},
	{Key: "plugins.enabled", Kind: kindBool, Default: "false", Writable: true,
		Confirm: "Turning plugins on lets installed plugin code run inside the daemon; each plugin still needs its own consent."},
	{Key: "fleet.enabled", Kind: kindBool, Default: "false", Writable: true, Restart: true,
		Confirm: "Turning the fleet on opens links to peer hosts you have paired; it applies after the daemon restarts."},
	{Key: "learning.enabled", Kind: kindBool, Default: "true", Writable: true, Restart: true, Env: EnvLearning,
		Confirm: "Turning learning on lets sMind collect run evidence and distill skills in workspaces that opted in."},

	// Read-only: config.get only; a human edits the file or uses the CLI.
	{Key: "browser.disabled", Kind: kindBool, Default: "false", Env: EnvBrowserDisabled},
	{Key: "automations.allowUnattendedModes", Kind: kindBool, Default: "false"},
	{Key: "automations.maxBudgetUsd", Kind: kindFloat, Default: strconv.FormatFloat(DefaultMaxAutomationBudgetUSD, 'f', -1, 64)},
	{Key: "automations.allowRemoteManage", Kind: kindBool, Default: "false"},
	{Key: "plugins.allowRemoteManage", Kind: kindBool, Default: "false"},
	{Key: "relay.legacyDevices", Kind: kindBool, Default: "false"},
	{Key: "relay.maxDevices", Kind: kindInt, Default: strconv.Itoa(DefaultRelayMaxDevices)},
	{Key: "accountUsage.enabled", Kind: kindBool, Default: "true"},
	{Key: "accountUsage.ttlSeconds", Kind: kindInt, Default: strconv.Itoa(MinAccountUsageTTLSeconds)},
	{Key: "accountUsage.routing", Kind: kindBool, Default: "true"},
	{Key: "accountUsage.exhaustThresholdPct", Kind: kindInt, Default: "100"},
	{Key: "accountUsage.passive", Kind: kindBool, Default: "true"},
	{Key: "validation.maxBudgetUsd", Kind: kindFloat, Default: "5"},
	{Key: "validation.defaultBudgetUsd", Kind: kindFloat, Default: "1.5"},
	{Key: "orchestration.maxDepth", Kind: kindInt, Default: "2"},
	{Key: "orchestration.maxConcurrentRunsPerTree", Kind: kindInt, Default: "4"},
	{Key: "orchestration.notifyParent", Kind: kindBool, Default: "true"},
	{Key: "orchestration.stallAlertMinutes", Kind: kindInt, Default: "15"},
	{Key: "steward.enabled", Kind: kindBool, Default: "false"},
	{Key: "steward.twoAxis", Kind: kindBool, Default: "false"},

	// Loopback-only: network topology, read by a loopback config.get only.
	{Key: "server.port", Kind: kindInt, Default: strconv.Itoa(DefaultPort), LoopbackOnly: true},
	{Key: "server.listen", Kind: kindString, Default: "127.0.0.1:" + strconv.Itoa(DefaultPort), LoopbackOnly: true, Env: EnvListen},
	{Key: "server.hostnames", Kind: kindList, Default: "", LoopbackOnly: true, Env: EnvHostnames},
}

// ConfigKeys returns the allowlist, sorted by key (a copy).
func ConfigKeys() []ConfigKey {
	out := make([]ConfigKey, len(configKeys))
	copy(out, configKeys)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// LookupConfigKey returns the allowlist row for key.
func LookupConfigKey(key string) (ConfigKey, bool) {
	for _, ck := range configKeys {
		if ck.Key == key {
			return ck, true
		}
	}
	return ConfigKey{}, false
}

// EffectiveValue is what config.get reports for one allowlisted key.
type EffectiveValue struct {
	// Value is the effective value as the daemon uses it: the file value,
	// else the default, with the environment override applied.
	Value string
	// Source is "file", "default" or "env".
	Source string
	// Env is the overriding variable's NAME when Source is "env"; never its
	// value.
	Env string
}

// Effective returns the effective value of one allowlisted key. An unknown
// key (anything outside the table, secrets included) is an error, never a
// silent miss.
func Effective(key string) (EffectiveValue, error) {
	ck, ok := LookupConfigKey(key)
	if !ok {
		return EffectiveValue{}, fmt.Errorf("unknown config key %q", key)
	}
	if ev, ok := envEffective(ck); ok {
		return ev, nil
	}
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return EffectiveValue{}, fmt.Errorf("%w: %w", ErrConfigUnreadable, err)
	}
	// An unreadable or unparsable config.yaml is not "the default": the loader
	// fails closed on it, so reporting defaults here would show the opposite of
	// what the daemon runs for the kill switches. Decoding into a generic map
	// resolves aliases and merge keys the way the loader does, and reads
	// yes/on/True as the bool they are.
	var doc map[string]any
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return EffectiveValue{}, fmt.Errorf("%w: %w", ErrConfigUnreadable, err)
		}
	}
	if v, present := decodedValue(doc, key); present {
		if ck.Kind == kindBool {
			v = normalizeBool(v)
		}
		return EffectiveValue{Value: v, Source: "file"}, nil
	}
	return EffectiveValue{Value: ck.Default, Source: "default"}, nil
}

// ErrConfigUnreadable wraps a failure to read or parse config.yaml.
var ErrConfigUnreadable = errors.New("config.yaml could not be read or parsed")

// normalizeBool turns the spellings the loader reads as a bool (yes, on, no,
// off, True, ...) into true/false; anything else is returned as written.
func normalizeBool(v string) string {
	var b bool
	if err := yaml.Unmarshal([]byte(v), &b); err != nil {
		return v
	}
	return strconv.FormatBool(b)
}

// envEffective mirrors applyEnv for the rows with an Env entry, so Value is
// what the daemon actually uses. SMIND_LEARNING only turns learning OFF
// (any other value is no override), and SMIND_BROWSER_DISABLED turns the
// kill switch ON with any value except an explicit "no"; both are kept.
func envEffective(ck ConfigKey) (EffectiveValue, bool) {
	switch ck.Key {
	case "learning.enabled":
		if v := strings.TrimSpace(os.Getenv(EnvLearning)); v != "" {
			switch strings.ToLower(v) {
			case "0", "false", "off", "no", "n", "f", "disabled":
				return EffectiveValue{Value: "false", Source: "env", Env: EnvLearning}, true
			}
		}
	case "browser.disabled":
		if v := strings.TrimSpace(os.Getenv(EnvBrowserDisabled)); v != "" {
			switch strings.ToLower(v) {
			case "0", "false", "off", "no", "n", "f":
				// an explicit no: the file value stands
			default:
				return EffectiveValue{Value: "true", Source: "env", Env: EnvBrowserDisabled}, true
			}
		}
	case "server.listen":
		if v := strings.TrimSpace(os.Getenv(EnvListen)); v != "" {
			return EffectiveValue{Value: v, Source: "env", Env: EnvListen}, true
		}
	case "server.hostnames":
		if v := os.Getenv(EnvHostnames); strings.TrimSpace(v) != "" {
			return EffectiveValue{Value: strings.Join(envHostnames(v), ","), Source: "env", Env: EnvHostnames}, true
		}
	}
	return EffectiveValue{}, false
}

// envHostnames splits a SMIND_HOSTNAMES value the way applyServerEnv does.
func envHostnames(v string) []string {
	var out []string
	for _, h := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		out = append(out, strings.ToLower(h))
	}
	return out
}

// decodedValue walks a decoded config document to key and formats a scalar the
// way `smind config get` prints it (a list comma-joined). present is false when
// any step is absent or null.
func decodedValue(doc map[string]any, key string) (string, bool) {
	var cur any = doc
	for _, p := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[p]; !ok || cur == nil {
			return "", false
		}
	}
	switch v := cur.(type) {
	case []any:
		parts := make([]string, 0, len(v))
		for _, e := range v {
			parts = append(parts, fmt.Sprint(e))
		}
		return strings.Join(parts, ","), true
	case map[string]any:
		return "", false
	}
	return fmt.Sprint(cur), true
}
