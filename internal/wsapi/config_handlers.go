package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sync"

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
		loopback := rc.atMachine()

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
				return nil, configErr("config.get", err)
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

// config.set (ADR-0062): change one writable allowlisted key. Human-only and
// loopback-only (no allowRemoteManage escape hatch); every attempt, allowed or
// refused, leaves one line in config-audit.jsonl.

// configSetMu serialises the read-old / write / re-read sequence so two calls
// cannot lose each other's update or misreport the old value.
var configSetMu sync.Mutex

// configAfterWrite is a test seam: it runs after config.Set wrote the file and
// before the value is re-read, so a test can prove the response comes from the
// file and not from the submitted value.
var configAfterWrite func()

// configCaller classifies the caller for the audit line.
func configCaller(rc *requestContext) (channel, caller string) {
	switch {
	case rc == nil:
		return "", "unknown"
	case rc.runScoped:
		return string(rc.channel), "run"
	case !rc.local:
		return string(rc.channel), "relay"
	case !rc.atMachine():
		return string(rc.channel), "remote"
	}
	return string(rc.channel), "human"
}

// auditConfig appends one audit line; a failure is logged and returned so the
// success path can say the change was applied but not audited.
func auditConfig(e config.AuditEntry) error {
	err := config.AppendAudit(e)
	if err != nil {
		log.Printf("config.set: audit write failed: %v", err)
	}
	return err
}

// configErr turns a config package error into the text a caller sees: the
// parser's own words (which may carry a path or a YAML fragment) stay in the
// daemon log.
func configErr(method string, err error) *rpcCodeError {
	if errors.Is(err, config.ErrConfigUnreadable) {
		log.Printf("%s: %v", method, err)
		return &rpcCodeError{code: codeInvalid, msg: method + ": " + config.ErrConfigUnreadable.Error() + "; fix it and try again"}
	}
	return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error()}
}

func handleConfigSet(publish func(key string)) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		channel, caller := configCaller(rc)
		entry := config.AuditEntry{Channel: channel, Caller: caller}
		refuse := func(outcome string, err error) (any, error) {
			entry.Outcome = outcome
			auditConfig(entry)
			return nil, err
		}
		switch {
		case rc == nil || rc.runScoped:
			return refuse("refused: run-scoped", forbidden("config.set is not available to agents"))
		case !rc.local:
			return refuse("refused: relay", forbidden("config.set is only available on a direct connection, not over the relay"))
		case !rc.atMachine():
			return refuse("refused: non-loopback", forbidden("config.set needs a direct local (loopback) connection, not one forwarded by a proxy"))
		}

		var params struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return refuse("refused: invalid params", &rpcCodeError{code: codeInvalid, msg: "config.set: invalid params: " + err.Error()})
		}
		entry.Key = params.Key
		ck, ok := config.LookupConfigKey(params.Key)
		if !ok {
			return refuse("refused: not in allowlist", &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("config.set: unknown key %q", params.Key)})
		}
		if !ck.Writable {
			return refuse("refused: read-only", &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("config.set: %s is read-only over the API (change it in config.yaml)", params.Key)})
		}
		var value string
		switch v := params.Value.(type) {
		case string:
			value = v
		case bool:
			value = fmt.Sprintf("%t", v)
		default:
			return refuse("refused: invalid", &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("config.set: %s wants true or false", params.Key)})
		}

		// The locked section is a closure with a deferred unlock, so a panic in
		// the write cannot leave every later config.set waiting forever.
		var cur config.EffectiveValue
		var auditErr error
		res, rerr := func() (any, error) {
			configSetMu.Lock()
			defer configSetMu.Unlock()
			old, err := config.Effective(params.Key)
			if err != nil {
				return refuse("refused: unreadable config.yaml", configErr("config.set", err))
			}
			entry.Old = old.Value
			if old.Source == "env" {
				return refuse("refused: env-overridden", &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("overridden by %s, unset it first", old.Env)})
			}
			// Every writable row is a bool (the table says so).
			if value != "true" && value != "false" {
				return refuse("refused: invalid", &rpcCodeError{code: codeInvalid, msg: fmt.Sprintf("config.set: %s wants true or false", params.Key)})
			}
			if err := config.Set(params.Key, value); err != nil {
				return refuse("refused: invalid: "+err.Error(), configErr("config.set", err))
			}
			if configAfterWrite != nil {
				configAfterWrite()
			}
			cur, err = config.Effective(params.Key)
			if err != nil {
				return refuse("refused: unreadable config.yaml after write", configErr("config.set", err))
			}
			entry.New = cur.Value
			entry.Outcome = "ok"
			auditErr = auditConfig(entry)
			return nil, nil
		}()
		if rerr != nil || res != nil {
			return res, rerr
		}

		if publish != nil {
			publish(params.Key)
		}
		if auditErr != nil {
			// The switch moved; say so rather than pretend it did not.
			return nil, &rpcCodeError{code: codeInvalid, msg: "config.set: the change was applied but could not be written to the audit log; see the daemon log"}
		}
		return map[string]any{"key": params.Key, "value": cur.Value, "restart": ck.Restart}, nil
	}
}
