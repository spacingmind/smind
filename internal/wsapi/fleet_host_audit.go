package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// fleet.host.audit: the human's read-only view of fleet_audit (ADR-0048, AC14).
// Like every fleet.host.* admin method it is local-only (fleetLocalOnly: a relay
// caller is refused), not run-scoped and not in the plugin capability table, so
// an agent, a plugin and a peer host never reach it. It returns direction, the
// peer (alias), method, decision, the redacted summary and the time; never a
// trace or request id, and never a prompt: rows hold only a hash and a masked,
// redacted prefix, and the view masks the free-text fields again for rows
// written before the writer did (see auditSummaryForView).

const (
	fleetAuditDefaultLimit = 50
	fleetAuditMaxLimit     = 200
)

type fleetHostAuditParams struct {
	fleetHostRef
	Limit int `json:"limit"`
}

// FleetAuditEntry is one row of the audit view.
type FleetAuditEntry struct {
	ID        int64     `json:"id"`
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	HostID    string    `json:"hostId"`
	Peer      string    `json:"peer"`
	Method    string    `json:"method"`
	Decision  string    `json:"decision"`
	Code      string    `json:"code,omitempty"`
	Summary   string    `json:"summary"`
	LatencyMs int64     `json:"latencyMs"`
}

type fleetHostAuditResult struct {
	Rows []FleetAuditEntry `json:"rows"`
}

// auditBodyField matches a free-text field's string value in a stored summary,
// which is a (possibly cut-off) JSON prefix: the value runs to its closing quote
// or, when the prefix ended inside it, to the end. Groups: key and opening quote,
// value, closing quote (empty when cut off).
var auditBodyField = regexp.MustCompile(`("(?:prompt|text|message|body|content|input)"\s*:\s*")((?:\\.|[^"\\])*)("?)`)

// auditMasked is what the writer leaves in place of a free-text value.
var auditMasked = regexp.MustCompile(`^<\d+ chars>$`)

// auditSummaryForView masks the free-text fields of a stored summary. The writer
// already does this (serve.Summary, leaving "<N chars>"); rows from before that,
// and any future writer slip, must not become readable here. A value the writer
// already masked is kept, so the length stays visible.
func auditSummaryForView(s string) string {
	return auditBodyField.ReplaceAllStringFunc(s, func(m string) string {
		g := auditBodyField.FindStringSubmatch(m)
		if auditMasked.MatchString(g[2]) {
			return m
		}
		return g[1] + "…" + g[3]
	})
}

func (f *fleetHost) handleHostAudit(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
	const method = "fleet.host.audit"
	var p fleetHostAuditParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, &rpcCodeError{code: codeInvalid, msg: method + ": invalid params: " + err.Error(), err: err}
		}
	}
	if p.Limit < 0 {
		return nil, &rpcCodeError{code: codeInvalid, msg: method + ": limit must not be negative"}
	}
	limit := p.Limit
	if limit == 0 {
		limit = fleetAuditDefaultLimit
	}
	if limit > fleetAuditMaxLimit {
		limit = fleetAuditMaxLimit
	}
	hostID := ""
	if p.ref() != "" {
		h, err := f.resolveHost(method, p.ref())
		if err != nil {
			return nil, err
		}
		hostID = h.HostID
	}
	rows, err := f.opts.Store.ListFleetAudit(hostID, time.Time{}, limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	aliases := map[string]string{}
	if hosts, err := f.opts.Registry.List(); err == nil {
		for _, h := range hosts {
			aliases[h.HostID] = h.Alias
		}
	}
	return fleetHostAuditResult{Rows: auditEntries(rows, aliases)}, nil
}

func auditEntries(rows []store.FleetAuditRow, aliases map[string]string) []FleetAuditEntry {
	out := make([]FleetAuditEntry, 0, len(rows))
	for _, r := range rows {
		peer := aliases[r.HostID]
		if peer == "" {
			// A removed host keeps its rows; show its id, short.
			peer = r.HostID
			if len(peer) > 8 {
				peer = peer[:8]
			}
		}
		out = append(out, FleetAuditEntry{
			ID: r.ID, Time: r.TS, Direction: r.Direction, HostID: r.HostID, Peer: peer, Method: r.Method,
			Decision: r.Outcome, Code: r.Code, Summary: auditSummaryForView(r.Summary), LatencyMs: r.LatencyMs,
		})
	}
	return out
}
