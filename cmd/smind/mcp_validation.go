package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/version"
	"github.com/spacingmind/smind/internal/wsclient"
)

// ADR-0045. Two surfaces:
//   - a Lead's catalogue gains validation_contract_submit, validation_request
//     and validation_get. None writes, edits or waives a verdict; "merge-ready"
//     is a label a person reads.
//   - a validator run's built-in smind entry (SMIND_VALIDATION_ID in its env)
//     serves ONLY validation_contract_get and validation_verdict_submit. The
//     one-time token comes from the entry's env, never from a tool argument,
//     so the model never sees it and cannot be talked into passing another.

// registerMCPValidationTools adds the Lead's validation tools.
func registerMCPValidationTools(srv *mcp.Server, client *wsclient.Client) {
	mcp.AddTool(srv, &mcp.Tool{Name: "validation_contract_submit", Description: "Store the validation contract for your task BEFORE implementing (ADR-0045): a file whose front matter lists the ACs, named tests, mutation checks, frames and commands a fresh-context validator will check. Pass path (relative to your worktree) or body. Returns {id, version, sha256}, or a field-path error to fix. A new submit is a new version; the validator judges the newest."}, mcpValidationContractSubmit(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "validation_request", Description: "Ask a fresh-context validator to judge your task's branch head (or a commitSha on it) against its newest contract. It runs in its own scratch worktree and never sees your reasoning; the daemon wakes you with a [child event] when it ends. Do not touch your worktree meanwhile. One at a time. Returns {validationId, ...}; read the result with validation_get. You cannot write, edit or waive a verdict."}, mcpValidationRequest(client))
	mcp.AddTool(srv, &mcp.Tool{Name: "validation_get", Description: "Read a validation (validationId) or your task's validations, newest first: status pass|fail|blocked|running, the verdict with per-item evidence, and mergeReady (a label for the human: true only for a pass at the current branch head and newest contract; smind never merges)."}, mcpValidationGet(client))
}

type validationContractSubmitInput struct {
	Path string `json:"path,omitempty" jsonschema:"contract file path relative to your worktree (YAML front matter + markdown)"`
	Body string `json:"body,omitempty" jsonschema:"the contract document inline, instead of path"`
}

func mcpValidationContractSubmit(client *wsclient.Client) mcp.ToolHandlerFor[validationContractSubmitInput, map[string]any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in validationContractSubmitInput) (*mcp.CallToolResult, map[string]any, error) {
		params := map[string]any{"source": "inline", "body": in.Body}
		if in.Path != "" {
			params = map[string]any{"source": "file", "path": in.Path}
		}
		var out map[string]any
		if err := callWS(ctx, client, "validation_contract_submit", "validation.contract.submit", params, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

type validationRequestInput struct {
	ContractID int64  `json:"contractId,omitempty" jsonschema:"contract id; default your newest"`
	CommitSHA  string `json:"commitSha,omitempty" jsonschema:"full 40-hex commit on your branch; default the branch head"`
}

func mcpValidationRequest(client *wsclient.Client) mcp.ToolHandlerFor[validationRequestInput, map[string]any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in validationRequestInput) (*mcp.CallToolResult, map[string]any, error) {
		params := map[string]any{}
		if in.ContractID > 0 {
			params["contractId"] = in.ContractID
		}
		if in.CommitSHA != "" {
			params["commitSha"] = in.CommitSHA
		}
		var out map[string]any
		if err := callWS(ctx, client, "validation_request", "validation.request", params, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

type validationGetInput struct {
	ValidationID int64 `json:"validationId,omitempty" jsonschema:"one validation; omit to list your task's"`
}

func mcpValidationGet(client *wsclient.Client) mcp.ToolHandlerFor[validationGetInput, map[string]any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in validationGetInput) (*mcp.CallToolResult, map[string]any, error) {
		method, params := "validation.list", map[string]any{}
		if in.ValidationID > 0 {
			method, params = "validation.get", map[string]any{"id": in.ValidationID}
		}
		var out any
		if err := callWS(ctx, client, "validation_get", method, params, &out); err != nil {
			return nil, nil, err
		}
		if m, ok := out.(map[string]any); ok {
			return nil, m, nil
		}
		return nil, map[string]any{"validations": out}, nil
	}
}

// validatorEnv is what a validator run's smind entry carries.
type validatorEnv struct {
	ValidationID int64
	ContractID   int64
	Token        string
}

// validatorEnvFromOS reads the validator env; ok is false for any other run.
func validatorEnvFromOS() (validatorEnv, bool, error) {
	raw := os.Getenv("SMIND_VALIDATION_ID")
	if raw == "" {
		return validatorEnv{}, false, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	cid, cerr := strconv.ParseInt(os.Getenv("SMIND_CONTRACT_ID"), 10, 64)
	tok := os.Getenv("SMIND_VERDICT_TOKEN")
	if err != nil || cerr != nil || id <= 0 || cid <= 0 || tok == "" {
		return validatorEnv{}, true, errors.New("invalid SMIND_VALIDATION_ID / SMIND_CONTRACT_ID / SMIND_VERDICT_TOKEN")
	}
	return validatorEnv{ValidationID: id, ContractID: cid, Token: tok}, true, nil
}

// newValidatorMCPServer is the validator seat's whole catalogue.
func newValidatorMCPServer(client *wsclient.Client, env validatorEnv) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "smind", Title: "sMind validator", Version: version.Version}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "validation_contract_get", Description: "The stored contract you are judging: ACs, tests, mutations, frames, commands, write scope, limits. Authoritative over any paraphrase."}, mcpValidatorContractGet(client, env))
	mcp.AddTool(srv, &mcp.Tool{Name: "validation_verdict_submit", Description: "Submit your one verdict (pass, fail or blocked) with per-item evidence. The daemon rejects an incomplete pass and names the missing item: fix it and submit again. Accepted once."}, mcpValidatorVerdictSubmit(client, env))
	return srv
}

func mcpValidatorContractGet(client *wsclient.Client, env validatorEnv) mcp.ToolHandlerFor[struct{}, map[string]any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		var out map[string]any
		if err := callWS(ctx, client, "validation_contract_get", "validation.contract.get", map[string]any{"contractId": env.ContractID}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

type verdictSubmitInput struct {
	Verdict json.RawMessage `json:"verdict" jsonschema:"the verdict object"`
}

func mcpValidatorVerdictSubmit(client *wsclient.Client, env validatorEnv) mcp.ToolHandlerFor[verdictSubmitInput, map[string]any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in verdictSubmitInput) (*mcp.CallToolResult, map[string]any, error) {
		if len(in.Verdict) == 0 {
			return nil, nil, fmt.Errorf("validation_verdict_submit: verdict is required")
		}
		var out map[string]any
		params := map[string]any{"validationId": env.ValidationID, "token": env.Token, "verdict": in.Verdict}
		if err := callWS(ctx, client, "validation_verdict_submit", "validation.verdict.submit", params, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}
