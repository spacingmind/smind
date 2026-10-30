package config

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/spacingmind/smind/internal/providers"
)

// EnvKind is the value kind of one launch-env name (ADR-0046 section 3): how
// its value is validated and where it may live.
type EnvKind string

const (
	EnvKindPath   EnvKind = "path"
	EnvKindURL    EnvKind = "url"
	EnvKindSecret EnvKind = "secret"
	EnvKindPlain  EnvKind = "plain"
	EnvKindInt    EnvKind = "int"
)

// maxEnvValueLen bounds one env literal (a base URL, a model id, ...).
const maxEnvValueLen = 2048

// secretNameRe is the shape of a name in `secret: <name>`: a reference into
// the secret store, never the value itself.
var secretNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// EnvValue is one providers.custom env value: a literal (non-secret, may sit
// in config.yaml), a reference to a stored secret, or a passthrough from the
// daemon's own environment. Exactly one form is set; the zero value is the
// empty literal (some vendors need NAME="").
type EnvValue struct {
	Literal     string
	Secret      string
	Passthrough bool
}

func (v *EnvValue) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	switch node.Kind {
	case yaml.ScalarNode:
		// The raw text: an unquoted int or bool is its literal form too.
		v.Literal, v.Secret, v.Passthrough = node.Value, "", false
		return nil
	case yaml.MappingNode:
		if len(node.Content) != 2 {
			return fmt.Errorf("the value must set exactly one of literal, secret or passthrough")
		}
		key, val := node.Content[0].Value, node.Content[1]
		if val.Kind == yaml.AliasNode {
			val = val.Alias
		}
		switch key {
		case "literal", "secret":
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("%s must be a string", key)
			}
			if key == "secret" {
				if val.Value == "" {
					return fmt.Errorf("secret must name a stored secret")
				}
				v.Secret = val.Value
			} else {
				v.Literal = val.Value
			}
			return nil
		case "passthrough":
			var b bool
			if err := val.Decode(&b); err != nil {
				return fmt.Errorf("passthrough must be a boolean")
			}
			if !b {
				return fmt.Errorf("passthrough must be true")
			}
			v.Passthrough = true
			return nil
		default:
			return fmt.Errorf("unknown key %q (allowed keys: literal, secret, passthrough)", key)
		}
	}
	return fmt.Errorf("the value must be a literal or a mapping with one of literal, secret, passthrough")
}

func (v EnvValue) MarshalYAML() (any, error) {
	switch {
	case v.Secret != "":
		return map[string]any{"secret": v.Secret}, nil
	case v.Passthrough:
		return map[string]any{"passthrough": true}, nil
	default:
		return v.Literal, nil
	}
}

// HarnessEnvKinds is the per-harness allowlist of launch-env names and their
// value kinds (ADR-0046 section 3). ACP is free-form: nil, every name is
// taken (EnvKindOf derives its kind). A name outside the table is a reviewed
// code change away, never config: no PATH, LD_PRELOAD, proxies or headers.
func HarnessEnvKinds(r providers.Runner) map[string]EnvKind {
	switch r {
	case providers.RunnerClaude:
		return map[string]EnvKind{
			"ANTHROPIC_BASE_URL":                         EnvKindURL,
			"ANTHROPIC_AUTH_TOKEN":                       EnvKindSecret,
			"ANTHROPIC_API_KEY":                          EnvKindSecret,
			"ANTHROPIC_MODEL":                            EnvKindPlain,
			"ANTHROPIC_DEFAULT_OPUS_MODEL":               EnvKindPlain,
			"ANTHROPIC_DEFAULT_SONNET_MODEL":             EnvKindPlain,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":              EnvKindPlain,
			"ANTHROPIC_DEFAULT_FABLE_MODEL":              EnvKindPlain,
			"ANTHROPIC_SMALL_FAST_MODEL":                 EnvKindPlain,
			"CLAUDE_CODE_SUBAGENT_MODEL":                 EnvKindPlain,
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":   EnvKindPlain,
			"CLAUDE_CODE_EFFORT_LEVEL":                   EnvKindPlain,
			"CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": EnvKindPlain,
			"ENABLE_TOOL_SEARCH":                         EnvKindPlain,
			"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB":           EnvKindPlain,
			"API_TIMEOUT_MS":                             EnvKindInt,
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":            EnvKindInt,
			"CLAUDE_CONFIG_DIR":                          EnvKindPath,
		}
	case providers.RunnerCodex:
		return map[string]EnvKind{
			"OPENAI_BASE_URL": EnvKindURL,
			"OPENAI_API_KEY":  EnvKindSecret,
			"CODEX_HOME":      EnvKindPath,
		}
	}
	return nil
}

// EnvKindOf is the kind of name under runner: the allowlist for claude and
// codex (ok false when the name is absent), and for ACP every name is taken,
// secret when it looks like a credential by suffix, else plain.
func EnvKindOf(r providers.Runner, name string) (EnvKind, bool) {
	kinds := HarnessEnvKinds(r)
	if kinds == nil {
		up := strings.ToUpper(name)
		for _, suffix := range []string{"_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_API_KEY"} {
			if strings.HasSuffix(up, suffix) {
				return EnvKindSecret, true
			}
		}
		return EnvKindPlain, true
	}
	kind, ok := kinds[name]
	return kind, ok
}

// allowedEnvNames lists the allowlisted names of runner, sorted (diagnostics
// and the not-allowed error; names only, never values).
func allowedEnvNames(r providers.Runner) []string {
	kinds := HarnessEnvKinds(r)
	names := make([]string, 0, len(kinds))
	for name := range kinds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateInstanceEnv checks one providers.custom env entry against the
// runner's allowlist and the name's value kind, returning the normalised
// value (a path "~/" expanded and cleaned). A secret-kind name takes only a
// secret reference, a passthrough or the empty literal: a key never goes
// inline in config.yaml. Errors name the field, never the value.
func ValidateInstanceEnv(r providers.Runner, name string, v EnvValue) (EnvValue, error) {
	if !envNameRe.MatchString(name) {
		return EnvValue{}, fmt.Errorf("the name is not a valid environment variable name")
	}
	kind, ok := EnvKindOf(r, name)
	if !ok {
		return EnvValue{}, fmt.Errorf("%s is not an allowed env name for %s (allowed: %s)", name, r, strings.Join(allowedEnvNames(r), ", "))
	}
	switch {
	case (v.Secret != "" && v.Passthrough) || (v.Secret != "" && v.Literal != "") || (v.Passthrough && v.Literal != ""):
		return EnvValue{}, fmt.Errorf("only one of literal, secret or passthrough may be set")
	case v.Secret != "":
		if kind != EnvKindSecret {
			return EnvValue{}, fmt.Errorf("a secret reference is only valid for a secret-kind name")
		}
		if !secretNameRe.MatchString(v.Secret) {
			return EnvValue{}, fmt.Errorf("the secret name must be 1-64 characters of A-Z, a-z, 0-9, . _ - and start with a letter or digit")
		}
		return v, nil
	case v.Passthrough:
		if kind != EnvKindSecret {
			return EnvValue{}, fmt.Errorf("passthrough is only valid for a secret-kind name")
		}
		return v, nil
	}
	// Literal form (possibly empty: an all-zero EnvValue is the empty literal).
	switch kind {
	case EnvKindSecret:
		if v.Literal != "" {
			return EnvValue{}, fmt.Errorf("a secret-kind name must be a secret reference ({secret: <name>}) or passthrough, never an inline value")
		}
		return v, nil
	case EnvKindPath:
		clean, err := cleanEnvPath(name, v.Literal)
		if err != nil {
			return EnvValue{}, err
		}
		return EnvValue{Literal: clean}, nil
	case EnvKindURL:
		if len(v.Literal) > maxEnvValueLen {
			return EnvValue{}, fmt.Errorf("%s: the value must be at most %d characters", name, maxEnvValueLen)
		}
		for _, c := range v.Literal {
			if c < 0x20 || c == 0x7f {
				return EnvValue{}, fmt.Errorf("%s: the value must not contain NUL or control characters", name)
			}
		}
		u, err := url.Parse(v.Literal)
		if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
			return EnvValue{}, fmt.Errorf("%s: the value must be an http or https URL", name)
		}
		if u.User != nil {
			return EnvValue{}, fmt.Errorf("%s: the URL must not carry credentials", name)
		}
		return v, nil
	case EnvKindInt:
		if n, err := strconv.Atoi(v.Literal); err != nil || n < 0 {
			return EnvValue{}, fmt.Errorf("%s: the value must be a non-negative integer", name)
		}
		return v, nil
	default: // plain
		if len(v.Literal) > maxEnvValueLen {
			return EnvValue{}, fmt.Errorf("%s: the value must be at most %d characters", name, maxEnvValueLen)
		}
		for _, c := range v.Literal {
			if c < 0x20 || c == 0x7f {
				return EnvValue{}, fmt.Errorf("%s: the value must not contain NUL or control characters", name)
			}
		}
		return v, nil
	}
}
