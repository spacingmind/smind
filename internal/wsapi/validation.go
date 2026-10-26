package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/validation"
	"github.com/spacingmind/smind/internal/workspace"
)

// The validation.* RPCs (ADR-0045). A contract is submitted by the task's own
// run or a human; a verdict only by the validator's own run, holding the
// one-time token the daemon gave that run alone. Reads are open to every
// connection (relay included) except that a run-scoped caller reads only its
// own task's records. There is no RPC that merges, waives or edits a verdict.

// TopicValidationUpdated carries {taskId, validationId, status}: a contract
// version was stored (status "contract", validationId 0), a validation
// started ("running"), a verdict was written ("pass"|"fail"|"blocked") or
// the lead's worktree was flagged ("scope_violation").
const TopicValidationUpdated = "validation.updated"

type validationUpdatedPayload struct {
	TaskID       int64  `json:"taskId"`
	ValidationID int64  `json:"validationId"`
	Status       string `json:"status"`
}

// validationMethods lists every validation RPC; a test walks it.
var validationMethods = []string{
	"validation.contract.submit", "validation.contract.get",
	"validation.verdict.submit", "validation.get", "validation.list",
}

// validationLimits reads validation.* from config.yaml on every call, so a
// human edit applies without a restart.
func validationLimits() validation.Limits {
	cfg, err := config.Load()
	if err != nil {
		return validation.DefaultLimits
	}
	return validation.Limits{MaxBudgetUSD: cfg.Validation.MaxBudgetUsd, DefaultBudgetUSD: cfg.Validation.DefaultBudgetUsd}
}

func newValidationService(db *store.Store, bus *eventBus) *validation.Service {
	return &validation.Service{
		DB:           db,
		EvidenceRoot: filepath.Join(config.Dir(), "validation"),
		Limits:       validationLimits,
		Head: func(taskID int64) (string, error) {
			t, err := db.GetTask(taskID)
			if err != nil || t.WorktreePath == nil {
				return "", fmt.Errorf("task %d has no worktree", taskID)
			}
			return workspace.GitHead(context.Background(), *t.WorktreePath)
		},
		TaskDir: func(taskID int64) (string, error) {
			t, err := db.GetTask(taskID)
			if err != nil || t.WorktreePath == nil {
				return "", fmt.Errorf("task %d has no worktree", taskID)
			}
			return *t.WorktreePath, nil
		},
		Publish: func(taskID, id int64, status string) {
			if bus != nil {
				bus.Publish(Event{Topic: TopicValidationUpdated, Payload: validationUpdatedPayload{TaskID: taskID, ValidationID: id, Status: status}})
			}
		},
	}
}

// validationErr maps service errors onto wire codes.
func validationErr(method string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, validation.ErrNotFound):
		return &rpcCodeError{code: codeNotFound, msg: method + ": " + err.Error(), err: err}
	case errors.Is(err, validation.ErrBadToken), errors.Is(err, validation.ErrWrongRun):
		return forbidden(method + ": " + err.Error())
	case errors.Is(err, validation.ErrInvalid):
		return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	case errors.Is(err, validation.ErrVerdictExists), errors.Is(err, validation.ErrValidationOpen):
		return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	}
	return fmt.Errorf("%s: %w", method, err)
}

// contractView is the wire shape of a stored contract. Body (the human-only
// markdown) is omitted for a validator.
type contractView struct {
	ID       int64               `json:"id"`
	TaskID   int64               `json:"taskId"`
	Version  int                 `json:"version"`
	Sha256   string              `json:"sha256"`
	Contract validation.Contract `json:"contract"`
	Body     string              `json:"body,omitempty"`
	Current  bool                `json:"current"`
}

func validationHandlers(svc *validation.Service) map[string]handlerFunc {
	hs := map[string]handlerFunc{}

	contractOf := func(rec validation.ContractRecord, withBody bool) contractView {
		v := contractView{ID: rec.ID, TaskID: rec.TaskID, Version: rec.Version, Sha256: rec.Sha256, Contract: rec.Contract}
		if withBody {
			v.Body = rec.Body
		}
		if latest, err := svc.LatestContract(rec.TaskID); err == nil {
			v.Current = latest.ID == rec.ID
		}
		return v
	}

	hs["validation.contract.submit"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "validation.contract.submit"
		if rc == nil {
			return nil, forbidden(m + " is not available")
		}
		var p struct {
			TaskID int64  `json:"taskId"`
			Source string `json:"source"` // "file" | "inline"
			Path   string `json:"path"`
			Body   string `json:"body"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.TaskID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": taskId is required"}
		}
		if rc.runScoped && rc.ident.taskID != p.TaskID {
			return nil, forbidden(m + " is only available for the caller's own task")
		}
		var src []byte
		switch p.Source {
		case "file":
			b, err := svc.ReadContractFile(p.TaskID, p.Path)
			if err != nil {
				return nil, validationErr(m, err)
			}
			src = b
		case "inline", "":
			src = []byte(p.Body)
		default:
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": source must be file or inline"}
		}
		rec, err := svc.SubmitContract(p.TaskID, src)
		if err != nil {
			return nil, validationErr(m, err)
		}
		return contractView{ID: rec.ID, TaskID: rec.TaskID, Version: rec.Version, Sha256: rec.Sha256, Contract: rec.Contract, Current: true}, nil
	}

	hs["validation.contract.get"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "validation.contract.get"
		var p struct {
			ContractID int64 `json:"contractId"`
			TaskID     int64 `json:"taskId"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		var rec validation.ContractRecord
		var err error
		switch {
		case p.ContractID > 0:
			rec, err = svc.GetContract(p.ContractID)
		case p.TaskID > 0:
			rec, err = svc.LatestContract(p.TaskID)
		default:
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": contractId or taskId is required"}
		}
		if err != nil {
			return nil, validationErr(m, err)
		}
		if rc == nil || !rc.runScoped {
			return contractOf(rec, true), nil
		}
		// A run-scoped caller is the contract's own task (full record) or a
		// validator of exactly this contract (no markdown body).
		if rc.ident.taskID == rec.TaskID {
			return contractOf(rec, true), nil
		}
		if vs, err := svc.DB.ListValidationsByRun(rc.ident.runID); err == nil {
			for _, v := range vs {
				if v.ContractID == rec.ID {
					return contractOf(rec, false), nil
				}
			}
		}
		return nil, forbidden(m + " is only available for the caller's own task or validation")
	}

	hs["validation.verdict.submit"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "validation.verdict.submit"
		if rc == nil || !rc.runScoped {
			return nil, forbidden(m + " is only available to the validator's own run")
		}
		var p struct {
			ValidationID int64           `json:"validationId"`
			Token        string          `json:"token"`
			Verdict      json.RawMessage `json:"verdict"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.ValidationID <= 0 || len(p.Verdict) == 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": validationId and verdict are required"}
		}
		v, err := svc.SubmitVerdict(p.ValidationID, p.Token, rc.ident.runID, p.Verdict)
		return v, validationErr(m, err)
	}

	hs["validation.get"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "validation.get"
		var p struct {
			ID int64 `json:"id"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.ID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": id is required"}
		}
		var v validation.View
		var err error
		if rc != nil && rc.runScoped {
			v, err = svc.ViewOwnedBy(p.ID, rc.ident.taskID)
		} else {
			v, err = svc.View(p.ID)
		}
		return v, validationErr(m, err)
	}

	hs["validation.list"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "validation.list"
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.TaskID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": taskId is required"}
		}
		if rc != nil && rc.runScoped && rc.ident.taskID != p.TaskID {
			return nil, forbidden(m + " is only available for the caller's own task")
		}
		l, err := svc.List(p.TaskID)
		return l, validationErr(m, err)
	}
	return hs
}
