package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/store"
)

// The plugin.* RPCs (ADR-0042). Reads (plugin.list, plugin.get) are open to
// every connection that may call them, relay sessions included. Everything that
// changes plugin state or could run code (install, link, enable, disable,
// remove, update, reload) is HUMAN + LOCAL only: it is refused with
// "forbidden" for a run-scoped (agent) caller and for a relay session
// (rc.local false), exactly like the other administrative handlers. A nil
// request context fails closed.
//
// The global switch (plugins.enabled in config.yaml) has no RPC at all: only a
// human editing the file turns plugins on.

// pluginMutating lists the methods the local-only guard wraps; a test walks it.
var pluginMutating = []string{
	"plugin.install", "plugin.link", "plugin.enable", "plugin.disable",
	"plugin.remove", "plugin.update", "plugin.reload",
}

// configPluginsEnabled reads the switch from config.yaml on every call, so a
// human edit applies without a restart. An unreadable or invalid file is OFF.
func configPluginsEnabled() bool {
	cfg, err := config.Load()
	return err == nil && cfg.Plugins.Enabled
}

// configPluginsAllowRemote reads plugins.allowRemoteManage on every call, like
// the switch; an unreadable file is false. Tests replace it.
var configPluginsAllowRemote = func() bool {
	cfg, err := config.Load()
	return err == nil && cfg.Plugins.AllowRemoteManage
}

func humanLocalOnly(method string, h handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		switch {
		case rc == nil || rc.runScoped:
			return nil, forbidden(method + " is not available to agents")
		case !rc.local:
			return nil, forbidden(method + " is only available on a direct connection, not over the relay")
		case rc.channel != ChannelLoopback && !configPluginsAllowRemote():
			return nil, forbidden(method + " needs a local connection: set plugins.allowRemoteManage: true in config.yaml to manage plugins from a remote connection")
		}
		return h(ctx, rc, raw)
	}
}

// pluginErr maps a plugin package error onto a wire code: typed plugin
// errors keep their code (hash_mismatch, unpinned_ref, ...), a manifest error
// is bad_manifest, anything else is a plain error.
func pluginErr(method string, err error) error {
	if err == nil {
		return nil
	}
	if code := plugins.CodeOf(err); code != "" {
		return &rpcCodeError{code: code, msg: method + ": " + err.Error(), err: err}
	}
	return fmt.Errorf("%s: %w", method, err)
}

// codeReadOnlyOwned is the typed error for a write to a row a plugin owns.
const codeReadOnlyOwned = "read_only_owned"

// ownedRPCErr maps a write to a plugin-owned row onto the typed
// read_only_owned error naming the plugin (data: {plugin}); any other error
// keeps the usual method-prefixed shape.
func ownedRPCErr(method string, err error) error {
	var oe *store.OwnedRowError
	if errors.As(err, &oe) {
		return &rpcCodeError{code: codeReadOnlyOwned, msg: method + ": " + oe.Error(), err: err, data: map[string]string{"plugin": oe.Plugin}}
	}
	return fmt.Errorf("%s: %w", method, err)
}

// pluginResultID is the plugin a mutation touched: the id in the params, or
// for install and link the id of the plugin just recorded.
func pluginResultID(res any, paramID string) string {
	if paramID != "" {
		return paramID
	}
	if in, ok := res.(plugins.Info); ok {
		return in.ID
	}
	return ""
}

func pluginParams(method string, raw json.RawMessage, p any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, p); err != nil {
		return &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error()}
	}
	return nil
}

func pluginHandlers(mgr *plugins.Manager, ev *pluginEvents) map[string]handlerFunc {
	hs := map[string]handlerFunc{}
	hs["plugin.list"] = func(context.Context, *requestContext, json.RawMessage) (any, error) {
		l, err := mgr.List()
		return l, pluginErr("plugin.list", err)
	}
	hs["plugin.get"] = func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := pluginParams("plugin.get", raw, &p); err != nil {
			return nil, err
		}
		in, err := mgr.Get(p.ID)
		return in, pluginErr("plugin.get", err)
	}

	// Prompt commands and themes of the ACTIVE plugins (empty with the switch
	// off): data only, readable by every caller the other reads allow.
	hs["plugin.commands"] = func(context.Context, *requestContext, json.RawMessage) (any, error) {
		return orEmpty(mgr.Commands()), nil
	}
	hs["plugin.themes"] = func(context.Context, *requestContext, json.RawMessage) (any, error) {
		return orEmpty(mgr.Themes()), nil
	}

	guard := func(method string, h func(ctx context.Context, raw json.RawMessage) (any, error)) {
		hs[method] = humanLocalOnly(method, func(ctx context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
			res, err := h(ctx, raw)
			if err == nil && ev != nil {
				var p struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(raw, &p)
				ev.changed(pluginResultID(res, p.ID))
			}
			return res, pluginErr(method, err)
		})
	}
	guard("plugin.install", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Source string `json:"source"`
		}
		if err := pluginParams("plugin.install", raw, &p); err != nil {
			return nil, err
		}
		return mgr.Install(ctx, p.Source)
	})
	guard("plugin.link", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := pluginParams("plugin.link", raw, &p); err != nil {
			return nil, err
		}
		return mgr.Link(ctx, p.Path)
	})
	guard("plugin.enable", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID      string `json:"id"`
			Confirm string `json:"confirm"`
		}
		if err := pluginParams("plugin.enable", raw, &p); err != nil {
			return nil, err
		}
		return mgr.Enable(p.ID, p.Confirm)
	})
	guard("plugin.disable", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := pluginParams("plugin.disable", raw, &p); err != nil {
			return nil, err
		}
		return mgr.Disable(p.ID)
	})
	guard("plugin.remove", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID       string `json:"id"`
			KeepData bool   `json:"keepData"`
		}
		if err := pluginParams("plugin.remove", raw, &p); err != nil {
			return nil, err
		}
		if err := mgr.Remove(p.ID, p.KeepData); err != nil {
			return nil, err
		}
		return map[string]any{"id": p.ID, "removed": true, "keptData": p.KeepData}, nil
	})
	guard("plugin.update", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Apply  bool   `json:"apply"`
		}
		if err := pluginParams("plugin.update", raw, &p); err != nil {
			return nil, err
		}
		return mgr.Update(ctx, p.ID, p.Source, p.Apply)
	})
	guard("plugin.reload", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := pluginParams("plugin.reload", raw, &p); err != nil {
			return nil, err
		}
		return mgr.Reload(p.ID)
	})
	for _, m := range pluginMutating {
		if hs[m] == nil {
			panic("wsapi: plugin method without a handler: " + m)
		}
	}
	return hs
}

// StartPlugins reconciles the plugins directory with the database (removes
// install leftovers, disables an enabled plugin that no longer validates).
// The daemon calls it once at startup.
func (a *API) StartPlugins() {
	if a.plugins != nil {
		a.plugins.Start()
	}
}

// orEmpty makes an empty list marshal as [] rather than null.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
