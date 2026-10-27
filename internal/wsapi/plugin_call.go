package wsapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/spacingmind/smind/internal/plugins"
)

// plugin.call {pluginId, rpc, params} (ADR-0042 section 4, PB.4) routes to the
// plugin process's `rpc.<name>`, only for a name the manifest declares in
// contributes.rpc.
//
// Who may call: a human on a direct connection (local or remote login) and a
// relay session, because the phone is the Human and ADR-0042 section 8 says it
// may trigger plugin actions. plugins.allowRemoteManage does NOT apply: that
// switch gates install/enable/settings (changing what runs), and a call only
// asks an already consented process to do what it declared. Refused: a
// run-scoped (agent) caller, since the ADR gives agents no plugin API and no
// `agentCallable` flag exists; a plugin-scoped session, whose capability table
// never lists plugin.* (so a plugin cannot call another plugin or itself); a
// nil request context (fails closed). plugin.call is on neither
// RunScopedMethods nor PluginCapabilityTable.
func pluginCallHandler(mgr *plugins.Manager) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc == nil || rc.runScoped {
			return nil, forbidden("plugin.call is not available to agents")
		}
		var p struct {
			PluginID string          `json:"pluginId"`
			RPC      string          `json:"rpc"`
			Params   json.RawMessage `json:"params"`
		}
		if err := pluginParams("plugin.call", raw, &p); err != nil {
			return nil, err
		}
		if p.PluginID == "" || p.RPC == "" {
			return nil, &rpcCodeError{code: codeInvalid, msg: "plugin.call: pluginId and rpc are required"}
		}
		res, err := mgr.Call(ctx, p.PluginID, p.RPC, p.Params)
		if err != nil {
			var pe *plugins.Error
			if errors.As(err, &pe) {
				return nil, &rpcCodeError{code: pe.Code, msg: "plugin.call: " + pe.Msg, err: err,
					data: map[string]string{"plugin": p.PluginID, "rpc": p.RPC}}
			}
			return nil, err
		}
		return res, nil
	}
}
