package wsapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/spacingmind/smind/internal/plugins"
)

// plugin.view {pluginId, panelId, ui, context} returns the validated view
// document of one panel (ADR-0042 section 5). ui is the client's view schema
// level, stated per request (a client that omits it gets ui_level_too_low).
//
// Access rules are those of plugin.call: a human on a direct connection and a
// relay session may read a view, a run-scoped (agent) caller and a nil request
// context may not, and plugin.view is on neither RunScopedMethods nor
// PluginCapabilityTable, so a plugin never reads another plugin's views.
func pluginViewHandler(mgr *plugins.Manager) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc == nil || rc.runScoped {
			return nil, forbidden("plugin.view is not available to agents")
		}
		var p struct {
			PluginID string          `json:"pluginId"`
			PanelID  string          `json:"panelId"`
			UI       int             `json:"ui"`
			Context  json.RawMessage `json:"context"`
		}
		if err := pluginParams("plugin.view", raw, &p); err != nil {
			return nil, err
		}
		if p.PluginID == "" || p.PanelID == "" {
			return nil, &rpcCodeError{code: codeInvalid, msg: "plugin.view: pluginId and panelId are required"}
		}
		view, err := mgr.View(ctx, p.PluginID, p.PanelID, p.UI, p.Context)
		if err != nil {
			var pe *plugins.Error
			if errors.As(err, &pe) {
				return nil, &rpcCodeError{code: pe.Code, msg: "plugin.view: " + pe.Msg, err: err,
					data: map[string]string{"plugin": p.PluginID, "panel": p.PanelID}}
			}
			return nil, err
		}
		return map[string]any{"pluginId": p.PluginID, "panelId": p.PanelID, "view": view}, nil
	}
}
