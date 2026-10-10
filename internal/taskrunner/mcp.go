package taskrunner

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/store"
)

// McpServerSource is the slice of *mcpservers.Registry the Runner needs:
// the enabled MCP servers applicable to one workspace (ADR-0018).
type McpServerSource interface {
	ListForWorkspace(workspaceID int64) ([]store.McpServer, error)
}

// WithMcpServers enables passing configured MCP servers (ADR-0018) to
// ACP and Claude-native turns. Left unset, the feature is fully off and
// behavior is identical to before.
func WithMcpServers(src McpServerSource) Option {
	return func(r *Runner) { r.mcpServers = src }
}

// mcpCapableBackend is the capability slice of acpBackend that
// acpMcpServers filters by.
type mcpCapableBackend interface {
	SupportsMcpStdio() bool
	SupportsMcpHttp() bool
	SupportsMcpSSE() bool
}

// acpMcpServers maps enabled store rows to ACP wire entries, dropping
// (and naming in inactive) each server whose transport the agent doesn't
// advertise. lookPath resolves a stdio server's bare command to an
// absolute path; a command that can't be resolved fails with an error
// naming only the server name and command -- never env/headers/args
// values, which may carry secrets.
func acpMcpServers(rows []store.McpServer, client mcpCapableBackend, lookPath func(string) (string, error)) (wire []any, inactive []string, err error) {
	wire = []any{}
	for _, row := range rows {
		switch row.Transport {
		case "stdio":
			if !client.SupportsMcpStdio() {
				inactive = append(inactive, row.Name)
				continue
			}
			command := row.Command
			if !filepath.IsAbs(command) {
				resolved, err := lookPath(command)
				if err != nil {
					return nil, inactive, fmt.Errorf("mcp server %q: command %q not found in PATH", row.Name, row.Command)
				}
				resolved, err = filepath.Abs(resolved)
				if err != nil {
					return nil, inactive, fmt.Errorf("mcp server %q: command %q not found in PATH", row.Name, row.Command)
				}
				command = resolved
			}
			var args []string
			if err := json.Unmarshal([]byte(row.Args), &args); err != nil && strings.TrimSpace(row.Args) != "" {
				return nil, inactive, fmt.Errorf("mcp server %q: args: %w", row.Name, err)
			}
			env, err := nameValues(row.Env)
			if err != nil {
				return nil, inactive, fmt.Errorf("mcp server %q: env: %w", row.Name, err)
			}
			wire = append(wire, acp.McpServerStdio{Type: "stdio", Name: row.Name, Command: command, Args: args, Env: env})
		case "http":
			if !client.SupportsMcpHttp() {
				inactive = append(inactive, row.Name)
				continue
			}
			headers, err := nameValues(row.Headers)
			if err != nil {
				return nil, inactive, fmt.Errorf("mcp server %q: headers: %w", row.Name, err)
			}
			wire = append(wire, acp.McpServerHttp{Type: "http", Name: row.Name, URL: row.URL, Headers: headers})
		case "sse":
			if !client.SupportsMcpSSE() {
				inactive = append(inactive, row.Name)
				continue
			}
			headers, err := nameValues(row.Headers)
			if err != nil {
				return nil, inactive, fmt.Errorf("mcp server %q: headers: %w", row.Name, err)
			}
			wire = append(wire, acp.McpServerHttp{Type: "sse", Name: row.Name, URL: row.URL, Headers: headers})
		}
	}
	return wire, inactive, nil
}

// nameValues decodes a row's env/headers JSON object into []NameValue
// sorted by key, so the wire shape is deterministic.
func nameValues(objJSON string) ([]acp.NameValue, error) {
	m := map[string]string{}
	if strings.TrimSpace(objJSON) != "" {
		if err := json.Unmarshal([]byte(objJSON), &m); err != nil {
			return nil, err
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]acp.NameValue, 0, len(keys))
	for _, k := range keys {
		out = append(out, acp.NameValue{Name: k, Value: m[k]})
	}
	return out, nil
}

// claudeMcpConfig builds Claude CLI --mcp-config JSON from the enabled
// rows: {"mcpServers":{"<name>":{...}}}. A stdio command stays bare (no
// LookPath -- the CLI resolves it in the spawned server's own
// environment); empty args/env/headers are omitted. Returns "" with no
// error when rows is empty, so the caller adds no option at all.
func claudeMcpConfig(rows []store.McpServer) (string, error) {
	servers := map[string]any{}
	for _, row := range rows {
		switch row.Transport {
		case "stdio":
			srv := map[string]any{"type": "stdio", "command": row.Command}
			var args []string
			if err := json.Unmarshal([]byte(row.Args), &args); err != nil && strings.TrimSpace(row.Args) != "" {
				return "", fmt.Errorf("mcp server %q: args: %w", row.Name, err)
			}
			if len(args) > 0 {
				srv["args"] = args
			}
			env := map[string]string{}
			if strings.TrimSpace(row.Env) != "" {
				if err := json.Unmarshal([]byte(row.Env), &env); err != nil {
					return "", fmt.Errorf("mcp server %q: env: %w", row.Name, err)
				}
			}
			if len(env) > 0 {
				srv["env"] = env
			}
			servers[row.Name] = srv
		case "http", "sse":
			srv := map[string]any{"type": row.Transport, "url": row.URL}
			headers := map[string]string{}
			if strings.TrimSpace(row.Headers) != "" {
				if err := json.Unmarshal([]byte(row.Headers), &headers); err != nil {
					return "", fmt.Errorf("mcp server %q: headers: %w", row.Name, err)
				}
			}
			if len(headers) > 0 {
				srv["headers"] = headers
			}
			servers[row.Name] = srv
		}
	}
	if len(servers) == 0 {
		return "", nil
	}
	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// mcpServersForWorkspace lists the enabled MCP servers for workspaceID,
// or an empty slice when no source is configured (feature off).
func (r *Runner) mcpServersForWorkspace(workspaceID int64) ([]store.McpServer, error) {
	if r.mcpServers == nil {
		return nil, nil
	}
	rows, err := r.mcpServers.ListForWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
