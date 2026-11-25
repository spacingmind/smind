package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/acp"
	"github.com/spacingmind/smind/internal/providers"
	"github.com/spacingmind/smind/internal/store"

	"github.com/spacingmind/smind/internal/securefile"
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
	if err := securefile.WriteFile(path, []byte(config)); err != nil {
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

// smindMcpName is the reserved name of the built-in orchestration server.
const smindMcpName = "smind"

// browserMcpName is the reserved name of the built-in browser server
// (ADR-0061). Unlike smindMcpName it is reserved for EVERY run: a registered
// server of that name never reaches any run, managed or not, so only the
// daemon can ever offer the browser tools.
const browserMcpName = "smind-browser"

func withoutName(rows []store.McpServer, name string) []store.McpServer {
	out := make([]store.McpServer, 0, len(rows))
	for _, row := range rows {
		if row.Name != name {
			out = append(out, row)
		}
	}
	return out
}

// WithSmindBinary sets the executable the built-in orchestration MCP entry
// runs (`<path> mcp serve`). Default: the daemon's own binary
// (os.Executable, resolved lazily), falling back to "smind".
func WithSmindBinary(path string) Option {
	return func(r *Runner) { r.smindBinary = path }
}

func (r *Runner) smindBinaryPath() string {
	if r.smindBinary != "" {
		return r.smindBinary
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return smindMcpName
}

// mcpRowsFor is mcpServersForWorkspace under the run's seat contract. With
// no contract (or no Mcp policy) the rows are exactly the workspace's. With
// one, every row named "smind" is removed (reserved name) and, for an
// orchestration seat, the built-in stdio entry is appended.
func (r *Runner) mcpRowsFor(ctx context.Context, workspaceID int64) ([]store.McpServer, error) {
	// The Claude CLI's process id is not known when its rows are built (the SDK
	// starts the CLI later and does not report it), so this path gets no browser
	// row: a browser needs the run's root process to bind its socket to.
	return r.mcpRowsForDelivery(ctx, workspaceID, true, 0)
}

// mcpRowsForDelivery is mcpRowsFor for a caller that knows whether the rows
// will really be handed to the agent. With deliver false the browser row is
// not built at all (a codex-native run takes no per-run MCP rows, a plugin
// provider never gets smind's own): no row, and no browser socket opened for a
// row nobody will receive. rootPID is the agent process the run's browser socket
// is bound to; 0 (unknown) builds no browser row: never a weaker check.
func (r *Runner) mcpRowsForDelivery(ctx context.Context, workspaceID int64, deliver bool, rootPID int) ([]store.McpServer, error) {
	var open func(*BrowserPolicy) (string, error)
	if deliver && rootPID > 0 {
		open = func(b *BrowserPolicy) (string, error) {
			if b.Socket == nil {
				return "", errNoBrowserSocket
			}
			return b.Socket(rootPID)
		}
	}
	return r.mcpRowsWith(ctx, workspaceID, open)
}

var errNoBrowserSocket = errors.New("taskrunner: no browser socket opener")

// mcpRowsPending is mcpRowsFor for an agent whose process does not exist yet when
// its rows are built (the Claude CLI, started later by the SDK): the browser
// row's socket is created UNBOUND and bind(pid) attaches it to the agent's
// process once it runs. bind is nil when no browser row was built. A connection
// before bind is refused by the daemon.
func (r *Runner) mcpRowsPending(ctx context.Context, workspaceID int64) (rows []store.McpServer, bind func(rootPID int) error, err error) {
	rows, err = r.mcpRowsWith(ctx, workspaceID, func(b *BrowserPolicy) (string, error) {
		if b.OpenPending == nil {
			return "", errNoBrowserSocket
		}
		path, bd, perr := b.OpenPending()
		if perr != nil {
			return "", perr
		}
		bind = bd
		return path, nil
	})
	return rows, bind, err
}

// mcpRowsWith builds the run's rows; openSocket (nil: no browser row) opens the
// run's browser socket when a browser row is to be delivered.
func (r *Runner) mcpRowsWith(ctx context.Context, workspaceID int64, openSocket func(*BrowserPolicy) (string, error)) ([]store.McpServer, error) {
	rows, err := r.mcpServersForWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	rows = withoutName(withHookMcp(ctx, rows), browserMcpName)
	policy := RunContractFrom(ctx).Mcp
	if policy == nil {
		return rows, nil
	}
	out := make([]store.McpServer, 0, len(rows)+1)
	for _, row := range rows {
		if row.Name != smindMcpName {
			out = append(out, row)
		}
	}
	if policy.Orchestration {
		args, _ := json.Marshal([]string{"mcp", "serve"})
		env := policy.Env
		if env == nil {
			env = map[string]string{}
		}
		envJSON, err := json.Marshal(env)
		if err != nil {
			return nil, fmt.Errorf("smind mcp env: %w", err)
		}
		out = append(out, store.McpServer{
			Name: smindMcpName, Transport: "stdio", Command: r.smindBinaryPath(),
			Args: string(args), Env: string(envJSON), Enabled: true,
		})
	}
	if policy.Browser != nil && openSocket != nil {
		sock, serr := openSocket(policy.Browser)
		if serr != nil || sock == "" {
			// No socket, no browser (logged by the host that failed): the run goes on without it.
			return out, nil
		}
		args, _ := json.Marshal([]string{"mcp", "browser"})
		env := map[string]string{}
		for k, v := range policy.Browser.Env {
			env[k] = v
		}
		env["SMIND_BROWSER_SOCKET"] = sock
		envJSON, err := json.Marshal(env)
		if err != nil {
			return nil, fmt.Errorf("smind-browser mcp env: %w", err)
		}
		out = append(out, store.McpServer{
			Name: browserMcpName, Transport: "stdio", Command: r.smindBinaryPath(),
			Args: string(args), Env: string(envJSON), Enabled: true,
		})
	}
	return out, nil
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

// McpSecrets is mcpSecrets for packages that scrub text of MCP server
// secrets themselves (the learning collector).
func McpSecrets(rows []store.McpServer) []string { return mcpSecrets(rows) }

// MinSecretLen is the shortest value McpSecrets reports.
const MinSecretLen = minSecretLen

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

// useStrictMcp says whether a claude run hides every MCP server but the
// daemon's own (--strict-mcp-config). A managed run does; a policy that exists
// only to carry the browser grant (BrowserOnly) must not: it would hide the
// person's own servers from a run that never asked for a managed tool set.
func useStrictMcp(c RunContract) bool { return c.Mcp != nil && !c.Mcp.BrowserOnly }

// browserRowDelivered says whether the browser row can reach the agent of a
// provider, by the provider reach table (providers.BrowserReachOf): a provider
// the table says no to (a plugin provider, codex-native, an agent that takes no
// MCP servers) and a provider the registry does not know get no row and no socket.
func browserRowDelivered(spec providers.Spec, haveSpec bool) bool {
	return haveSpec && providers.BrowserReachOf(spec).Offered()
}

// revokeBrowserAll closes the run's browser socket (the provider took none
// of the rows).
func revokeBrowserAll(ctx context.Context) {
	if p := RunContractFrom(ctx).Mcp; p != nil && p.Browser != nil && p.Browser.Revoke != nil {
		p.Browser.Revoke()
	}
}

// revokeBrowserIfSkipped drops it when the provider skipped the browser row
// (names are the servers it left inactive or unresolved).
func revokeBrowserIfSkipped(ctx context.Context, names ...[]string) {
	for _, list := range names {
		for _, n := range list {
			if n == browserMcpName || strings.HasPrefix(n, browserMcpName+" ") {
				revokeBrowserAll(ctx)
				return
			}
		}
	}
}
