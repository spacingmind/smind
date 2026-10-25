package main

import (
	"context"
	"errors"
	"os"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/version"
)

// pluginManager is a SECOND plugins.Manager over the daemon API's own store and
// plugins home (SMIND_HOME), so the plugin-* commands can install and enable
// plugins the way `smind plugin` does on a real daemon: the install/enable RPCs
// are local-only (refused over the relay), so a test cannot reach them through
// a device. The API's manager derives everything from the store and the files
// on every read, so what this one changes shows in plugin.list / plugin.commands
// / plugin.themes at once. TEST ONLY, like the rest of this helper.
func (h *harness) pluginManager() *plugins.Manager {
	if h.plugins == nil {
		h.plugins = plugins.New(plugins.Options{
			Home: config.Dir(), Store: h.store, Version: version.Version,
			Enabled: func() bool {
				cfg, err := config.Load()
				return err == nil && cfg.Plugins.Enabled
			},
		})
	}
	return h.plugins
}

// pluginCmd handles plugin-install <dir> | plugin-link <dir> | plugin-enable <id> |
// plugin-disable <id> | plugin-reload <id> | plugin-update <id> <dir>. Enabling confirms the consent
// fingerprint it just read, as the CLI's --yes does.
func (h *harness) pluginCmd(cmd string, args []string) (map[string]any, error) {
	if err := need(args, 1, cmd+" <dir|id>"); err != nil {
		return nil, err
	}
	// Never fall back to ~/.smind: a run without an isolated home refuses.
	if os.Getenv("SMIND_HOME") == "" {
		return nil, errors.New(cmd + ": set SMIND_HOME to an isolated directory first")
	}
	m := h.pluginManager()
	ctx := context.Background()
	var (
		in  plugins.Info
		err error
	)
	switch cmd {
	case "plugin-install":
		in, err = m.Install(ctx, args[0])
	case "plugin-link":
		in, err = m.Link(ctx, args[0])
	case "plugin-enable":
		cur, gerr := m.Get(args[0])
		if gerr != nil {
			return nil, gerr
		}
		in, err = m.Enable(args[0], cur.Consent.Fingerprint)
	case "plugin-disable":
		in, err = m.Disable(args[0])
	case "plugin-update":
		if err := need(args, 2, "plugin-update <id> <dir>"); err != nil {
			return nil, err
		}
		// Stages the new version; a changed consent stays PENDING (never applied here).
		if _, err = m.Update(ctx, args[0], args[1], false); err == nil {
			in, err = m.Get(args[0])
		}
	case "plugin-reload":
		in, err = m.Reload(args[0])
	default:
		return nil, errors.New("unknown plugin command " + cmd)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": in.ID, "enabled": in.Enabled, "lastError": in.LastError}, nil
}
