package taskrunner

import (
	"encoding/json"
	"fmt"
	"os"
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
// absolute path; a command that can't be resolved is skipped (degrade,
// don't fail the run) and reported in unresolved as `name (command "x")`
// -- never env/headers/args values, which may carry secrets. states
// mirrors every row (row order) with its disposition for
// EventTypeMcpServers: active, or dropped with the reason.
func acpMcpServers(rows []store.McpServer, client mcpCapableBackend, lookPath func(string) (string, error)) (wire []any, inactive, unresolved []string, states []McpServerState, err error) {
	wire = []any{}
	for _, row := range rows {
		switch row.Transport {
		case "stdio":
			if !client.SupportsMcpStdio() {
				inactive = append(inactive, row.Name)
				states = append(states, McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusDropped, Reason: McpReasonTransportUnsupported})
				continue
			}
			command := row.Command
			if !filepath.IsAbs(command) {
				resolved, err := lookPath(command)
				if err == nil {
					resolved, err = filepath.Abs(resolved)
				}
				if err != nil {
					unresolved = append(unresolved, fmt.Sprintf("%s (command %q not found in PATH)", row.Name, row.Command))
					states = append(states, McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusDropped, Reason: McpReasonCommandNotFound})
					continue
				}
				command = resolved
			}
			var args []string
			if err := json.Unmarshal([]byte(row.Args), &args); err != nil && strings.TrimSpace(row.Args) != "" {
				return nil, inactive, unresolved, states, fmt.Errorf("mcp server %q: args: %w", row.Name, err)
			}
			env, err := nameValues(row.Env)
			if err != nil {
				return nil, inactive, unresolved, states, fmt.Errorf("mcp server %q: env: %w", row.Name, err)
			}
			wire = append(wire, acp.McpServerStdio{Type: "stdio", Name: row.Name, Command: command, Args: args, Env: env})
			states = append(states, McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusActive})
		case "http":
			if !client.SupportsMcpHttp() {
				inactive = append(inactive, row.Name)
				states = append(states, McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusDropped, Reason: McpReasonTransportUnsupported})
				continue
			}
			headers, err := nameValues(row.Headers)
			if err != nil {
				return nil, inactive, unresolved, states, fmt.Errorf("mcp server %q: headers: %w", row.Name, err)
			}
			wire = append(wire, acp.McpServerHttp{Type: "http", Name: row.Name, URL: row.URL, Headers: headers})
			states = append(states, McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusActive})
		case "sse":
			if !client.SupportsMcpSSE() {
				inactive = append(inactive, row.Name)
				states = append(states, McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusDropped, Reason: McpReasonTransportUnsupported})
				continue
			}
			headers, err := nameValues(row.Headers)
			if err != nil {
				return nil, inactive, unresolved, states, fmt.Errorf("mcp server %q: headers: %w", row.Name, err)
			}
			wire = append(wire, acp.McpServerHttp{Type: "sse", Name: row.Name, URL: row.URL, Headers: headers})
			states = append(states, McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusActive})
		}
	}
	return wire, inactive, unresolved, states, nil
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

// claudeMcpStates maps rows to their Claude-native McpServerState list
// (row order): every row active, except the names in failed (the CLI
// init message's failed servers) which come out failed/failed_to_start.
// failed is nil for the initial all-active event.
func claudeMcpStates(rows []store.McpServer, failed []string) []McpServerState {
	failedSet := map[string]bool{}
	for _, name := range failed {
		failedSet[name] = true
	}
	states := make([]McpServerState, len(rows))
	for i, row := range rows {
		state := McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusActive}
		if failedSet[row.Name] {
			state.Status, state.Reason = McpStatusFailed, McpReasonFailedToStart
		}
		states[i] = state
	}
	return states
}

// codexMcpStates maps rows to the Codex-native McpServerState list: every
// row dropped/provider_unsupported -- Codex has no MCP mechanism in v1
// (ADR-0018 resolved decision 1).
func codexMcpStates(rows []store.McpServer) []McpServerState {
	states := make([]McpServerState, len(rows))
	for i, row := range rows {
		states[i] = McpServerState{Name: row.Name, Transport: row.Transport, Status: McpStatusDropped, Reason: McpReasonProviderUnsupported}
	}
	return states
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

// writeClaudeMcpConfigFile writes the --mcp-config JSON to a file (mode
// 0600) inside a fresh private temp directory (0700, as os.MkdirTemp
// creates it) and returns the file path plus a cleanup func that removes
// the whole directory. The CLI accepts a file path for --mcp-config, which
// keeps secrets in the JSON out of the process argv. On error nothing is
// left behind.
func writeClaudeMcpConfigFile(config string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "smind-mcp-")
	if err != nil {
		return "", nil, fmt.Errorf("create mcp config dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	path = filepath.Join(dir, "mcp-config.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write mcp config: %w", err)
	}
	return path, cleanup, nil
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

// redactedPlaceholder replaces a known MCP secret inside scrubbed text.
const redactedPlaceholder = "[redacted]"

// minSecretLen is the shortest value scrubbed: shorter ones ("1", "on")
// would mangle ordinary text and are not meaningfully secret.
const minSecretLen = 4

// mcpSecrets collects the values in rows that may be secret -- env and
// header values, plus stdio args that aren't flags (for `--k=v` flags,
// just the `v`; positional args are scrubbed too, so a package name there
// may be over-redacted in error text, which is harmless) -- so agent-originated error text can be scrubbed of them
// before it reaches a session note, log line, or run error. A header
// value like "Bearer tok" also contributes its last word, since an agent
// may echo just the token.
func mcpSecrets(rows []store.McpServer) []string {
	var out []string
	add := func(v string) {
		if len(v) >= minSecretLen {
			out = append(out, v)
		}
	}
	addMap := func(objJSON string) {
		m := map[string]string{}
		if strings.TrimSpace(objJSON) == "" || json.Unmarshal([]byte(objJSON), &m) != nil {
			return
		}
		for _, v := range m {
			add(v)
			if f := strings.Fields(v); len(f) > 1 {
				add(f[len(f)-1])
			}
		}
	}
	for _, row := range rows {
		addMap(row.Env)
		addMap(row.Headers)
		var args []string
		if json.Unmarshal([]byte(row.Args), &args) != nil {
			continue
		}
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				if _, v, ok := strings.Cut(a, "="); ok {
					add(v)
				}
				continue
			}
			add(a)
		}
	}
	return out
}

// scrubSecrets replaces every occurrence of each secret in s with
// redactedPlaceholder. Longer secrets go first so one that contains
// another is replaced whole.
func scrubSecrets(s string, secrets []string) string {
	if len(secrets) == 0 {
		return s
	}
	sorted := append([]string(nil), secrets...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, sec := range sorted {
		s = strings.ReplaceAll(s, sec, redactedPlaceholder)
	}
	return s
}

// scrubbedError is err with its message scrubbed of secrets; it still
// unwraps to err so errors.Is/As keep working.
type scrubbedError struct {
	msg string
	err error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.err }

// scrubError returns err unchanged when it carries no secret, else a
// scrubbedError. nil stays nil.
func scrubError(err error, secrets []string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if scrubbed := scrubSecrets(msg, secrets); scrubbed != msg {
		return &scrubbedError{msg: scrubbed, err: err}
	}
	return err
}

// claudeFailedMcpServers returns the names of MCP servers a Claude CLI
// "init" system message reports with status "failed" (the CLI tolerates
// a failing server and carries on without it). Names only, sorted.
func claudeFailedMcpServers(raw json.RawMessage) []string {
	var w struct {
		McpServers []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"mcp_servers"`
	}
	if json.Unmarshal(raw, &w) != nil {
		return nil
	}
	var failed []string
	for _, s := range w.McpServers {
		if s.Status == "failed" && s.Name != "" {
			failed = append(failed, s.Name)
		}
	}
	sort.Strings(failed)
	return failed
}
