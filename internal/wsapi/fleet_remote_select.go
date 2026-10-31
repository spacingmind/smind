package wsapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/spacingmind/smind/internal/fleet/client"
	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/selector"
)

// remote.select resolves operator host labels to one host alias (ADR-0048
// F5). It only names a host: labels never leave this daemon and the result
// carries none. There is no retry and no fallback to another host.

// RemoteSelectParams is the remote.select ask.
type RemoteSelectParams struct {
	Labels map[string]string `json:"labels"`
	Any    bool              `json:"any"`
}

// RemoteSelectResult names the chosen host.
type RemoteSelectResult struct {
	Host   string `json:"host"`
	HostID string `json:"hostId"`
}

// liveMirrorStates are the mirrored remote-task states that count as load.
var liveMirrorStates = map[string]bool{"running": true, "waiting_permission": true, "queued": true, "created": true}

func (r *fleetRemote) liveCount(hostID string) int {
	if r.f.opts.Store == nil {
		return 0
	}
	rows, err := r.f.opts.Store.ListFleetRemoteTasks(hostID)
	if err != nil {
		return 0
	}
	n := 0
	for _, t := range rows {
		if liveMirrorStates[t.State] {
			n++
		}
	}
	return n
}

func (r *fleetRemote) handleSelect(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "remote.select"
	var p RemoteSelectParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
	}
	if err := registry.ValidateLabels(p.Labels); err != nil {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	}
	hosts, err := r.f.opts.Registry.List()
	if err != nil {
		return nil, err
	}
	var cands []selector.Candidate
	for _, h := range hosts {
		if h.State != registry.StateApproved {
			continue
		}
		enabled := false
		if out, _, err := r.f.opts.Registry.Links(h.HostID); err == nil {
			enabled = out.Enabled
		}
		_, st := r.o.Conn(h.HostID)
		cands = append(cands, selector.Candidate{
			Alias: h.Alias, HostID: h.HostID, Labels: h.Labels,
			Enabled: enabled, Connected: st == client.LinkConnected,
			Live: r.liveCount(h.HostID),
		})
	}
	c, err := selector.Select(selector.Selector{Labels: p.Labels, Any: p.Any}, cands)
	if err != nil {
		var se *selector.Error
		if errors.As(err, &se) {
			return nil, &rpcCodeError{code: se.Code, msg: se.Error(), err: err}
		}
		return nil, err
	}
	return RemoteSelectResult{Host: c.Alias, HostID: c.HostID}, nil
}
