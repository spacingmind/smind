package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/spacingmind/smind/internal/config"
)

// config.get (ADR-0062): the effective value, default, source and restart
// flag of every allowlisted config key. Read-only; any authenticated
// connection may call it except a run-scoped agent. The server.* rows are
// network topology and only a loopback connection sees them.

// configKeyView is one row of the config.get response.
type configKeyView struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Default  string `json:"default"`
	Source   string `json:"source"`
	Restart  bool   `json:"restart"`
	Writable bool   `json:"writable"`
	Env      string `json:"env,omitempty"`
}

var secretKeyRE = regexp.MustCompile(`(?i)password|token|secret`)

// redactConfigValue blanks a value whose key looks like a secret. The table
// holds none, so this is a second line of defence, not the first.
func redactConfigValue(key, value string) string {
	if secretKeyRE.MatchString(key) && value != "" {
		return "<set>"
	}
	return value
}

func handleConfigGet() handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc == nil || rc.runScoped {
			return nil, forbidden("config.get is not available to agents")
		}
		var params struct {
			Keys []string `json:"keys"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &params); err != nil {
				return nil, &rpcCodeError{code: codeInvalid, msg: "config.get: invalid params: " + err.Error()}
			}
		}
		loopback := rc.local && rc.channel == ChannelLoopback

		var rows []config.ConfigKey
		if len(params.Keys) == 0 {
			for _, ck := range config.ConfigKeys() {
				if ck.LoopbackOnly && !loopback {
					continue
				}
				rows = append(rows, ck)
			}
		} else {
			for _, k := range params.Keys {
				ck, ok := config.LookupConfigKey(k)
				if !ok {
					return nil, &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("config.get: unknown key %q", k)}
				}
				if ck.LoopbackOnly && !loopback {
					return nil, forbidden(fmt.Sprintf("config.get: key %s is only available on a local connection", k))
				}
				rows = append(rows, ck)
			}
		}

		out := make([]configKeyView, 0, len(rows))
		for _, ck := range rows {
			ev, err := config.Effective(ck.Key)
			if err != nil {
				return nil, &rpcCodeError{code: codeInvalid, msg: "config.get: " + err.Error()}
			}
			out = append(out, configKeyView{
				Key:      ck.Key,
				Value:    redactConfigValue(ck.Key, ev.Value),
				Default:  redactConfigValue(ck.Key, ck.Default),
				Source:   ev.Source,
				Restart:  ck.Restart,
				Writable: ck.Writable && loopback && ev.Source != "env",
				Env:      ev.Env,
			})
		}
		return map[string]any{"keys": out}, nil
	}
}
