package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/mcpservers"
	"github.com/spacingmind/smind/internal/store"
)

// mcpSecretPlaceholder replaces every env/headers value on every read path
// (mcp.list/mcp.get/mcp.create/mcp.update/mcp.setEnabled results, and the
// mcpServer.created/updated event payloads) -- ADR-0018's "Secrets in env/
// headers": a full value is accepted only on write (mcp.create/mcp.update's
// params), never echoed back. Keys are preserved so a client can still show
// which env vars/headers are configured, matching the ADR's "per-value
// placeholder" convention.
const mcpSecretPlaceholder = "[redacted]"

// mcpServerResult is the wire shape every mcp.* method returns: store.
// McpServer's fields, with Env/Headers redacted (mcpServerResultFrom is the
// only place a store.McpServer becomes one of these -- never marshal a bare
// store.McpServer over wsapi, or its raw secret values leak). Args is not
// secret-bearing (ADR-0018) and passes through unredacted.
type mcpServerResult struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Transport string          `json:"transport"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
	Env       json.RawMessage `json:"env"`
	URL       string          `json:"url"`
	Headers   json.RawMessage `json:"headers"`
	Enabled   bool            `json:"enabled"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
}

// mcpServerResultFrom redacts m's env/headers before it ever reaches the
// wire -- the one conversion every mcp.* handler and the mcpServer.*
// notifier below must route through.
func mcpServerResultFrom(m store.McpServer) mcpServerResult {
	return mcpServerResult{
		ID: m.ID, Name: m.Name, Transport: m.Transport, Command: m.Command,
		Args: rawJSONOrEmptyArray(m.Args), Env: redactMcpSecretValues(m.Env),
		URL: m.URL, Headers: redactMcpSecretValues(m.Headers),
		Enabled:   m.Enabled,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
	}
}

// rawJSONOrEmptyArray returns raw as a json.RawMessage, or "[]" for an empty
// string -- store.McpServer.Args is never actually "" in practice
// (internal/mcpservers.normalizeValidate canonicalizes it), but a bare
// store.McpServer built by hand (as in a test fixture) might leave it unset.
func rawJSONOrEmptyArray(raw string) json.RawMessage {
	if raw == "" {
		return json.RawMessage("[]")
	}
	return json.RawMessage(raw)
}

// redactMcpSecretValues decodes raw (a JSON string->string object, or "")
// and replaces every value with mcpSecretPlaceholder, keeping the keys --
// so a client can see which env vars/headers are configured without ever
// seeing what they're set to. A raw blob that doesn't decode as a
// string->string object (which normalizeValidate should have prevented from
// ever being stored) is redacted wholesale, failing safe rather than
// passing an unredacted value through.
func redactMcpSecretValues(raw string) json.RawMessage {
	if raw == "" {
		return json.RawMessage("{}")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return json.RawMessage(`"` + mcpSecretPlaceholder + `"`)
	}
	redacted := make(map[string]string, len(m))
	for k := range m {
		redacted[k] = mcpSecretPlaceholder
	}
	out, err := json.Marshal(redacted)
	if err != nil {
		return json.RawMessage("{}")
	}
	return out
}

// mcpServerParams is the params shape shared by mcp.create/mcp.update
// (ADR-0018's wsapi surface table): Args/Env/Headers arrive as raw JSON
// ([]string / string->string objects respectively) and are stored verbatim
// as their JSON-text form -- internal/mcpservers.Registry (via
// normalizeValidate) is what actually validates/canonicalizes their shape,
// not this handler.
type mcpServerParams struct {
	Name      string          `json:"name"`
	Transport string          `json:"transport"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
	Env       json.RawMessage `json:"env"`
	URL       string          `json:"url"`
	Headers   json.RawMessage `json:"headers"`
}

func (p mcpServerParams) toStoreMcpServer() store.McpServer {
	return store.McpServer{
		Name: p.Name, Transport: p.Transport, Command: p.Command,
		Args: string(p.Args), Env: string(p.Env), URL: p.URL, Headers: string(p.Headers),
	}
}

// handleMcpCreate creates a new MCP server (ADR-0018). Validation (empty/
// invalid name, unknown transport, missing command/url) is
// mcpservers.Registry's job, not this handler's -- its error is passed
// straight through, matching handleProfileCreate's passthrough convention.
func handleMcpCreate(mcpReg *mcpservers.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p mcpServerParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("mcp.create: invalid params: %w", err)
		}
		created, err := mcpReg.Create(p.toStoreMcpServer())
		if err != nil {
			return nil, fmt.Errorf("mcp.create: %w", err)
		}
		return mcpServerResultFrom(created), nil
	}
}

// handleMcpList returns every MCP server, enabled and disabled alike,
// ordered by id, redacted.
func handleMcpList(mcpReg *mcpservers.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, _ json.RawMessage) (any, error) {
		servers, err := mcpReg.List()
		if err != nil {
			return nil, fmt.Errorf("mcp.list: %w", err)
		}
		result := make([]mcpServerResult, len(servers))
		for i, m := range servers {
			result[i] = mcpServerResultFrom(m)
		}
		return result, nil
	}
}

// handleMcpGet returns the MCP server with the given id, redacted.
func handleMcpGet(mcpReg *mcpservers.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("mcp.get: invalid params: %w", err)
		}
		m, err := mcpReg.Get(p.ID)
		if err != nil {
			return nil, fmt.Errorf("mcp.get: %w", err)
		}
		return mcpServerResultFrom(m), nil
	}
}

// handleMcpUpdate replaces every field of the MCP server p.ID (a
// full-record replace, not a partial patch -- matching handleProfileUpdate),
// redacted.
func handleMcpUpdate(mcpReg *mcpservers.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
			mcpServerParams
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("mcp.update: invalid params: %w", err)
		}
		m := p.toStoreMcpServer()
		m.ID = p.ID
		updated, err := mcpReg.Update(m)
		if err != nil {
			return nil, fmt.Errorf("mcp.update: %w", err)
		}
		return mcpServerResultFrom(updated), nil
	}
}

// handleMcpDelete permanently removes the MCP server with the given id
// (and its workspace_mcp_servers restriction rows -- see
// store.DeleteMcpServer). No deleteSummaryResult-shaped body: an empty
// object confirms success, matching handleProfileDelete.
func handleMcpDelete(mcpReg *mcpservers.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("mcp.delete: invalid params: %w", err)
		}
		if err := mcpReg.Delete(p.ID); err != nil {
			return nil, fmt.Errorf("mcp.delete: %w", err)
		}
		return struct{}{}, nil
	}
}

// handleMcpSetEnabled toggles the enabled flag of the MCP server with the
// given id, redacted. A dedicated toggle rather than a generic update
// field, matching ADR-0018's wsapi surface (and profile.update's own note
// on why task.archive-style toggles get their own RPC).
func handleMcpSetEnabled(mcpReg *mcpservers.Registry) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			ID      int64 `json:"id"`
			Enabled bool  `json:"enabled"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("mcp.setEnabled: invalid params: %w", err)
		}
		updated, err := mcpReg.SetEnabled(p.ID, p.Enabled)
		if err != nil {
			return nil, fmt.Errorf("mcp.setEnabled: %w", err)
		}
		return mcpServerResultFrom(updated), nil
	}
}
