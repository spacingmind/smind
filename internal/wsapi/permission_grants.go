package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// Saved grants over the wire (docs/plans/active/saved-grants.md). All three
// methods are human-only: none is on RunScopedMethods or in a plugin
// capability, each refuses a run-scoped caller, and all three names are on
// permpolicy.HumanOnlyRPCs so the policy floor denies a shell command that
// names one next to a transport.
//
//	run.answerPermission     answer a pending permission, optionally saving it (scope "project")
//	permission.grants.list   read the saved grants, their status and audit
//	permission.grants.revoke revoke one
var permissionGrantMethods = []string{"run.answerPermission", "permission.grants.list", "permission.grants.revoke"}

// handleRunAnswerPermission is run.respondPermission plus a scope. With scope
// "project" and the offered allow_once option, the answer is also saved as a
// grant for the project and the exact command (the card showed that scope).
// Without a scope it behaves exactly like run.respondPermission (a deny reason
// included), so a client may use this one method for every answer.
func handleRunAnswerPermission(reg *runs.Registry) handlerFunc {
	respond := handleRunRespondPermission(reg)
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "run.answerPermission"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			RunID     string `json:"runId"`
			RequestID string `json:"requestId"`
			OptionID  string `json:"optionId"`
			Scope     string `json:"scope"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("%s: invalid params: %w", m, err)
		}
		if p.Scope == "" || p.Scope == "once" {
			return respond(ctx, rc, raw)
		}
		if err := reg.RespondPermissionScoped(p.RunID, p.RequestID, p.OptionID, p.Scope, inboxActor(rc)); err != nil {
			if errors.Is(err, runs.ErrGrantNotOffered) {
				return nil, &rpcCodeError{code: codeInvalid, msg: m + ": " + err.Error(), err: err}
			}
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		return struct{}{}, nil
	}
}

// grantView is one grant as the clients read it: the stored scope, and a
// computed status. The raw command's hash and the MCP fingerprint stay inside.
type grantView struct {
	store.PermissionGrant
	Status string `json:"status"` // active | expired | void | revoked
}

type grantAuditView struct {
	store.GrantAuditRow
}

func grantStatus(g store.PermissionGrant, fingerprintNow string, now time.Time) string {
	switch {
	case g.RevokedAt != nil:
		return "revoked"
	case !now.Before(g.ExpiresAt):
		return "expired"
	case g.MCPFingerprint != fingerprintNow:
		return "void"
	default:
		return "active"
	}
}

func handlePermissionGrantsList(db *store.Store, reg *runs.Registry) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "permission.grants.list"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			WorkspaceID int64 `json:"workspaceId"`
			GrantID     int64 `json:"grantId"`
		}
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("%s: invalid params: %w", m, err)
			}
		}
		now := time.Now()
		list, err := db.ListGrants(p.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		views := make([]grantView, 0, len(list))
		for _, g := range list {
			if p.GrantID != 0 && g.ID != p.GrantID {
				continue
			}
			fp, ferr := reg.GrantFingerprint(g.WorkspaceID)
			status := grantStatus(g, fp, now)
			if ferr != nil && status == "active" {
				status = "void" // cannot read the project's tool config: not applied, so not reported active
			}
			views = append(views, grantView{PermissionGrant: g, Status: status})
		}
		res := struct {
			Grants []grantView      `json:"grants"`
			Audit  []grantAuditView `json:"audit,omitempty"`
		}{Grants: views}
		if p.GrantID != 0 && len(views) == 1 {
			rows, err := db.ListGrantAudit(p.GrantID)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", m, err)
			}
			for _, r := range rows {
				res.Audit = append(res.Audit, grantAuditView{r})
			}
		}
		return res, nil
	}
}

func handlePermissionGrantsRevoke(db *store.Store) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "permission.grants.revoke"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			GrantID int64 `json:"grantId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.GrantID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": grantId is required"}
		}
		changed, err := db.RevokeGrant(p.GrantID, strings.TrimSpace(inboxActor(rc)), time.Now())
		if errors.Is(err, store.ErrGrantNotFound) {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": " + err.Error(), err: err}
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		return struct {
			Revoked bool `json:"revoked"`
		}{changed}, nil
	}
}
