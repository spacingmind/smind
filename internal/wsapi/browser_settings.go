package wsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/browser"
	"github.com/spacingmind/smind/internal/store"
)

// The browser settings RPCs (ADR-0061, docs/plans/active/browser-for-agents-pr3.md).
// ALL THREE ARE HUMAN-ONLY: none is on RunScopedMethods or in a plugin
// capability, each refuses a nil or run-scoped caller (refuseAgent), and all
// three names are on permpolicy.HumanOnlyRPCs so the policy floor denies a shell
// command that names one next to a transport. The enable method is as
// human-only as the RPC principal is today (ADR-0056 is still Proposed: an
// unidentified local connection counts as the human); the floor rule and the
// CLI refusal cover the common routes, and that limit is stated rather than hidden.
//
//	browser.settings.get   read a workspace's switch and allowlist
//	browser.settings.set   change them (closes the workspace's browser session)
//	browser.data.clear     end the workspace's browser session and forget its data

// browserSettingsMethods lists the human-only browser RPCs; a test walks it.
var browserSettingsMethods = []string{"browser.settings.get", "browser.settings.set", "browser.data.clear"}

const (
	// maxAllowHosts bounds one workspace's allowlist.
	maxAllowHosts = 50
	// allowNote is shown with every result: the rule for names.
	allowNote = "A host name may only resolve to public addresses; to allow an intranet or local-network host, add its IP address instead. Loopback (localhost, 127.0.0.1, ::1) is always allowed and needs no entry."
)

// browserSettingsStore is the part of the store the settings RPCs use.
type browserSettingsStore interface {
	GetWorkspace(id int64) (store.Workspace, error)
	GetBrowserSettings(workspaceID int64) (store.BrowserSettings, error)
	SaveBrowserSettings(bs store.BrowserSettings) error
	DeleteBrowserSettings(workspaceID int64) error
}

// browserSettingsView is what get and set return.
type browserSettingsView struct {
	WorkspaceID     int64      `json:"workspaceId"`
	Enabled         bool       `json:"enabled"`
	AllowHosts      []string   `json:"allowHosts"`
	ProfileMode     string     `json:"profileMode"`
	KillSwitch      bool       `json:"killSwitch"`
	Available       bool       `json:"available"` // browser.GateReady: false while the tools are dark
	ChromeInstalled bool       `json:"chromeInstalled"`
	InstallStep     string     `json:"installStep,omitempty"`
	Note            string     `json:"note"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy       string     `json:"updatedBy,omitempty"`
}

func (h *browserHost) view(bs store.BrowserSettings) browserSettingsView {
	v := browserSettingsView{
		WorkspaceID: bs.WorkspaceID, Enabled: bs.Enabled, AllowHosts: bs.AllowHosts, ProfileMode: bs.ProfileMode,
		KillSwitch: h.killSwitch(), Available: browser.GateReady, Note: allowNote, UpdatedBy: bs.UpdatedBy,
	}
	if v.AllowHosts == nil {
		v.AllowHosts = []string{}
	}
	if !bs.UpdatedAt.IsZero() {
		t := bs.UpdatedAt
		v.UpdatedAt = &t
	}
	if _, err := browser.DetectChrome(""); err == nil {
		v.ChromeInstalled = true
	} else {
		var ue *browser.UnavailableError
		if errors.As(err, &ue) {
			v.InstallStep = ue.InstallStep
		}
	}
	return v
}

func settingsInvalid(m, format string, a ...any) error {
	return &rpcCodeError{code: codeInvalid, msg: m + ": " + fmt.Sprintf(format, a...)}
}

// workspaceParam decodes the strict {workspaceId} of a settings call and checks
// the workspace exists.
func (h *browserHost) workspaceParam(m string, raw json.RawMessage, p any, ws func() int64) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		return settingsInvalid(m, "invalid params")
	}
	id := ws()
	if id <= 0 {
		return settingsInvalid(m, "workspaceId is required")
	}
	if _, err := h.settings.GetWorkspace(id); err != nil {
		return &rpcCodeError{code: codeNotFound, msg: m + ": unknown workspace " + strconv.FormatInt(id, 10)}
	}
	return nil
}

func (h *browserHost) settingsGet(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const m = "browser.settings.get"
	if err := refuseAgent(m, rc); err != nil {
		return nil, err
	}
	var p struct {
		WorkspaceID int64 `json:"workspaceId"`
	}
	if err := h.workspaceParam(m, raw, &p, func() int64 { return p.WorkspaceID }); err != nil {
		return nil, err
	}
	bs, err := h.settings.GetBrowserSettings(p.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m, err)
	}
	return h.view(bs), nil
}

// normalizeAllowHosts validates and canonicalises a full replacement
// allowlist: each entry exactly one host (see browser.NormalizeAllowHost), no
// duplicates, at most maxAllowHosts. The whole list is refused if any entry is.
func normalizeAllowHosts(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		h, err := browser.NormalizeAllowHost(raw)
		if err != nil {
			return nil, err
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	if len(out) > maxAllowHosts {
		return nil, fmt.Errorf("at most %d hosts may be allowed (%d given)", maxAllowHosts, len(out))
	}
	return out, nil
}

func (h *browserHost) settingsSet(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const m = "browser.settings.set"
	if err := refuseAgent(m, rc); err != nil {
		return nil, err
	}
	var p struct {
		WorkspaceID int64     `json:"workspaceId"`
		Enabled     *bool     `json:"enabled"`
		AllowHosts  *[]string `json:"allowHosts"`  // the full replacement list
		AllowAdd    []string  `json:"allowAdd"`    // hosts to add (applied atomically with the rest)
		AllowRemove []string  `json:"allowRemove"` // hosts to remove (each must be on the list)
		ProfileMode *string   `json:"profileMode"`
	}
	if err := h.workspaceParam(m, raw, &p, func() int64 { return p.WorkspaceID }); err != nil {
		return nil, err
	}
	if p.AllowHosts != nil && (len(p.AllowAdd) > 0 || len(p.AllowRemove) > 0) {
		return nil, settingsInvalid(m, "give allowHosts (a replacement) or allowAdd/allowRemove, not both")
	}
	// One writer at a time: the read-modify-write below must not interleave
	// with another human's (a lost "disable" would silently re-enable).
	h.setMu.Lock()
	defer h.setMu.Unlock()
	cur, err := h.settings.GetBrowserSettings(p.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m, err)
	}
	if p.Enabled != nil {
		cur.Enabled = *p.Enabled
	}
	if p.AllowHosts != nil {
		hosts, err := normalizeAllowHosts(*p.AllowHosts)
		if err != nil {
			return nil, settingsInvalid(m, "%v", err)
		}
		cur.AllowHosts = hosts
	}
	if len(p.AllowRemove) > 0 || len(p.AllowAdd) > 0 {
		hosts := slices.Clone(cur.AllowHosts)
		for _, raw := range p.AllowRemove {
			h1, err := browser.NormalizeAllowHost(raw)
			if err != nil {
				return nil, settingsInvalid(m, "%v", err)
			}
			if !slices.Contains(hosts, h1) {
				return nil, settingsInvalid(m, "host not in the allowlist: %s", h1)
			}
			hosts = slices.DeleteFunc(hosts, func(x string) bool { return x == h1 })
		}
		hosts, err := normalizeAllowHosts(append(hosts, p.AllowAdd...))
		if err != nil {
			return nil, settingsInvalid(m, "%v", err)
		}
		cur.AllowHosts = hosts
	}
	if p.ProfileMode != nil {
		switch strings.TrimSpace(*p.ProfileMode) {
		case "ephemeral", "":
			cur.ProfileMode = "ephemeral"
		case "persistent":
			return nil, settingsInvalid(m, "a persistent profile is not available yet; the browser profile is ephemeral")
		default:
			return nil, settingsInvalid(m, "profileMode must be ephemeral")
		}
	}
	if cur.ProfileMode == "" {
		cur.ProfileMode = "ephemeral"
	}
	cur.UpdatedBy = inboxActor(rc)
	cur.UpdatedAt = time.Now().UTC()
	if err := h.settings.SaveBrowserSettings(cur); err != nil {
		if _, gerr := h.settings.GetWorkspace(p.WorkspaceID); gerr != nil {
			return nil, &rpcCodeError{code: codeNotFound, msg: m + ": unknown workspace " + strconv.FormatInt(p.WorkspaceID, 10)}
		}
		return nil, fmt.Errorf("%s: %w", m, err)
	}
	// The new policy applies at once: a running browser never keeps an old one.
	h.closeWorkspace(p.WorkspaceID)
	return h.view(cur), nil
}

func (h *browserHost) dataClear(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
	const m = "browser.data.clear"
	if err := refuseAgent(m, rc); err != nil {
		return nil, err
	}
	var p struct {
		WorkspaceID int64 `json:"workspaceId"`
	}
	if err := h.workspaceParam(m, raw, &p, func() int64 { return p.WorkspaceID }); err != nil {
		return nil, err
	}
	// Profiles are ephemeral: ending the session removes its profile, so
	// "clear the data" is "end the session now".
	h.closeWorkspace(p.WorkspaceID)
	return struct{}{}, nil
}

// browserLocalOnly refuses a settings call on any connection that is not the local
// /ws one: a paired phone is not a place to widen what an agent's browser may
// reach (ADR-0061 decision 12: no browser UI on mobile). The Settings UI of the
// desktop and web apps is a local connection.
func browserLocalOnly(method string, h handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc == nil || !rc.local {
			return nil, &rpcCodeError{code: codeForbidden, msg: method + " is only available on this machine, not over the relay"}
		}
		return h(ctx, rc, raw)
	}
}

// closeWorkspace ends the workspace's browser session, if it has one.
func (h *browserHost) closeWorkspace(ws int64) {
	h.gateMu.Lock()
	defer h.gateMu.Unlock()
	h.closeSession(ws)
}
