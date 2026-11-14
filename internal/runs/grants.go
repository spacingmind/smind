package runs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// Saved grants (ADR-0055 section 8, docs/plans/active/saved-grants.md): a human
// answer with scope "project" saves "allow this exact command in this project";
// a later identical request is allowed without asking, until the grant expires,
// is revoked or is voided. The order is fixed: seat floor -> AutoAccept ->
// policy engine (floor, deny and ask rules) -> grant -> human. A grant only
// turns a plain, grantable `ask` into allow_once.

// GrantTTL is how long a saved grant lasts; a use does not renew it.
const GrantTTL = 30 * 24 * time.Hour

// maxGrantCommandBytes caps the raw command a grant can cover.
const maxGrantCommandBytes = 2000

// ErrGrantNotOffered refuses scope "project" for a request that carried no offer
// or for an option other than the offered allow_once.
var ErrGrantNotOffered = errors.New("this request cannot be saved as an always-allow")

// grantOffer is what a grantable pending request carries to its answer.
type grantOffer struct {
	sha           string
	length        int
	display       string
	folder        string
	project       string
	workspaceID   int64
	fingerprint   string
	allowOptionID string
}

func (reg *Registry) grantNow() time.Time {
	if reg.grantClock != nil {
		return reg.grantClock()
	}
	return time.Now()
}

// SetGrantClock replaces the clock grants expire by (tests); nil restores time.Now.
func (reg *Registry) SetGrantClock(now func() time.Time) {
	reg.mu.Lock()
	reg.grantClock = now
	reg.mu.Unlock()
}

func commandSHA(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// mcpFingerprint hashes the enabled MCP servers a project's agents see, minus
// their secrets (env, headers): any edit bumps updated_at, so a change of
// command, arguments or address, an added or removed server, all void a grant.
func mcpFingerprint(st *store.Store, workspaceID int64) (string, error) {
	servers, err := st.ListMcpServersForWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, m := range servers {
		fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\n", m.ID, m.Name, m.Transport, m.Command, m.Args, m.URL, boolInt(m.Enabled), m.UpdatedAt.UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// buildGrantOffer decides whether this pending request may be saved (and a
// stored grant applied): the engine called it a grantable ask (the caller
// checked), and the run, the request and the project all qualify. nil = no.
// The same function runs at offer time and at use time, so a request that
// stopped qualifying asks again.
func (d runPermissionDecider) buildGrantOffer(detail *taskrunner.PermissionDetail, options []taskrunner.PermissionOption) *grantOffer {
	// An attended, user-authored run only: no system run, seat, remote run, or AutoAccept.
	if d.r.purpose != "" || d.r.origin == store.ChatQueueSourceRemote || d.r.getPerm().AutoAccept {
		return nil
	}
	if _, seat := d.reg.seatSpec(d.r.taskID); seat {
		return nil
	}
	if detail == nil || detail.Kind != "execute" || detail.GitRule != nil || len(detail.Heredocs) > 0 || detail.Truncated || detail.PolicyTruncated || detail.Policy == nil {
		return nil
	}
	raw := detail.PolicyCommand
	if raw == "" || len(raw) > maxGrantCommandBytes {
		return nil
	}
	allow, ok := firstOptionByKind(options, "allow_once")
	if !ok {
		return nil
	}
	task, err := d.reg.st.GetTask(d.r.taskID)
	if err != nil || task.WorkspaceID == 0 {
		return nil
	}
	ws, err := d.reg.st.GetWorkspace(task.WorkspaceID)
	if err != nil {
		return nil
	}
	fp, err := mcpFingerprint(d.reg.st, task.WorkspaceID)
	if err != nil {
		return nil
	}
	display := detail.Policy.Normalized
	if display == "" {
		display = detail.Command
	}
	return &grantOffer{sha: commandSHA(raw), length: len(raw), display: display, folder: ws.Path, project: ws.Title, workspaceID: task.WorkspaceID, fingerprint: fp, allowOptionID: allow}
}

// withGrantOffer copies detail and sets the offer the card shows.
func withGrantOffer(detail *taskrunner.PermissionDetail, o *grantOffer) *taskrunner.PermissionDetail {
	c := *detail
	c.Grant = &taskrunner.GrantOffer{Command: o.display, Project: o.project, Folder: o.folder, ExpiresInDays: int(GrantTTL / (24 * time.Hour))}
	return &c
}

// applyGrant allows the request off a stored, active grant. ok is false when
// there is none, or it is revoked, expired or void; the caller then asks.
func (d runPermissionDecider) applyGrant(requestID, summary string, detail *taskrunner.PermissionDetail, options []taskrunner.PermissionOption, o *grantOffer) (string, bool) {
	g, found, err := d.reg.st.FindGrant(o.workspaceID, o.sha)
	if err != nil {
		slog.Warn("permission grant lookup failed; asking", "err", err)
		return "", false
	}
	if !found || g.RevokedAt != nil || g.CommandLen != o.length {
		return "", false
	}
	now := d.reg.grantNow()
	skip := func(kind, note string) {
		if err := d.reg.st.AppendGrantAudit(store.GrantAuditRow{GrantID: g.ID, At: now, Kind: kind, Actor: "daemon", RunID: d.r.id, RequestID: requestID, Note: note}); err != nil {
			slog.Warn("permission grant audit failed", "err", err)
		}
	}
	if !now.Before(g.ExpiresAt) {
		skip(store.GrantAuditSkippedExpired, "expired "+g.ExpiresAt.UTC().Format(time.RFC3339))
		return "", false
	}
	if g.MCPFingerprint != o.fingerprint {
		skip(store.GrantAuditSkippedVoid, "the project's MCP servers changed")
		return "", false
	}
	// The use is audited first: an allow that cannot be audited is not given.
	if err := d.reg.st.RecordGrantUse(g.ID, d.r.id, requestID, now); err != nil {
		slog.Warn("permission grant use audit failed; asking", "err", err)
		return "", false
	}
	d.recordPolicyResolved(requestID, summary, withGrantOffer(detail, o), options, o.allowOptionID, taskrunner.PermissionResolvedByGrant, fmt.Sprintf("grant:%d", g.ID))
	return o.allowOptionID, true
}

// saveGrant writes the grant a human's scope-project answer asked for. A write
// failure is logged and the single allow still goes through.
func (d runPermissionDecider) saveGrant(requestID string, o *grantOffer, actor string) int64 {
	now := d.reg.grantNow()
	g, err := d.reg.st.SaveGrant(store.PermissionGrant{
		WorkspaceID: o.workspaceID, CommandSHA: o.sha, CommandLen: o.length, CommandDisplay: truncateRunes(o.display, 500), Folder: o.folder, MCPFingerprint: o.fingerprint,
		CreatedAt: now, ExpiresAt: now.Add(GrantTTL), CreatedBy: actor, SourceRunID: d.r.id, SourceRequestID: requestID,
	})
	if err != nil {
		slog.Warn("saving a permission grant failed; the single allow still goes through", "err", err)
		return 0
	}
	return g.ID
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// RespondPermissionScoped answers a pending permission request like
// RespondPermission, and with scope "project" also saves it as a grant. The
// scope is honoured only for a request that carried the offer and only with
// its allow_once option; anything else is ErrGrantNotOffered and the request
// stays parked. actor names the human connection ("local", "device:<id>") for
// the audit. An empty scope or "once" is exactly RespondPermission.
func (reg *Registry) RespondPermissionScoped(runID, requestID, optionID, scope, actor string) error {
	switch scope {
	case "", "once":
		return reg.RespondPermission(runID, requestID, optionID)
	case "project":
	default:
		return fmt.Errorf("runs: unknown permission scope %q", scope)
	}
	r, err := reg.get(runID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	pp, ok := r.pendingPermissions[requestID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("runs: permission request %q: %w", requestID, ErrNotFound)
	}
	if pp.grant == nil || optionID != pp.grant.allowOptionID {
		r.mu.Unlock()
		return fmt.Errorf("runs: permission request %q: %w", requestID, ErrGrantNotOffered)
	}
	select {
	case pp.ch <- permissionAnswer{optionID: optionID, saveGrant: true, actor: strings.TrimSpace(actor)}:
		delete(r.pendingPermissions, requestID)
		r.mu.Unlock()
		return nil
	default:
		r.mu.Unlock()
		return fmt.Errorf("runs: permission request %q already resolved", requestID)
	}
}
