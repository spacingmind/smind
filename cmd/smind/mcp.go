package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/version"
	"github.com/spacingmind/smind/internal/wsclient"
)

// cmdMcpUsage is printed when `mcp` is invoked with no (or an unknown)
// subcommand. The group covers both ADR-0017's orchestrator-facing MCP
// server (`serve`) and ADR-0018's agent-side MCP server management
// (add|ls|rm|enable|disable), which manage the daemon-global mcp_servers
// registry runs are handed (internal/mcpservers.Registry) -- unrelated
// registries sharing one command-group name, matching resolved decision 8.
const cmdMcpUsage = "usage: smind mcp <serve|add|ls|rm|enable|disable> ..."

// cmdMcp dispatches the `smind mcp` command group.
func cmdMcp(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cmdMcpUsage)
		return 2
	}
	switch args[0] {
	case "serve":
		return cmdMcpServe(args[1:])
	case "add":
		return cmdMcpAdd(args[1:])
	case "ls":
		return cmdMcpList(args[1:])
	case "rm":
		return cmdMcpRemove(args[1:])
	case "enable":
		return cmdMcpSetEnabled(args[1:], true)
	case "disable":
		return cmdMcpSetEnabled(args[1:], false)
	default:
		fmt.Fprintf(os.Stderr, "smind mcp: unknown subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, cmdMcpUsage)
		return 2
	}
}

// cmdMcpServe runs the MCP server over stdio (ADR-0017): a thin client of
// the already-running daemon -- every tool call is a wsapi RPC over the
// one /ws connection dialed here, exactly like every other CLI
// subcommand. It dials before speaking any MCP so an unreachable daemon
// fails fast with a clear stderr message (resolved decision 5), rather
// than as a stream of per-tool-call transport errors; and it exits when
// stdin closes (the orchestrating client went away) or on SIGINT/SIGTERM.
func cmdMcpServe(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, cmdMcpUsage)
		return 2
	}

	client, err := dialDaemon(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp serve: %v\n", err)
		return 1
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A daemon restart kills this /ws connection; without this race the
	// MCP server would keep serving (every tool call failing with a
	// transport error) until the host closed its stdin. Exiting non-zero
	// lets the MCP host notice and respawn us against the new daemon
	// (ADR-0017 resolved decision 5's "daemon must already be running"
	// read the same way at runtime, not just at startup).
	srvDone := make(chan error, 1)
	go func() { srvDone <- newMCPServer(client).Run(ctx, &mcp.StdioTransport{}) }()

	select {
	case err := <-srvDone:
		// A signal-driven shutdown or the client closing stdin are normal
		// ends for a stdio server, not failures.
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			fmt.Fprintf(os.Stderr, "mcp serve: %v\n", err)
			return 1
		}
		return 0
	case <-client.Done():
		fmt.Fprintln(os.Stderr, "mcp serve: lost connection to the smind daemon (was it restarted?); exiting -- the MCP host should restart this server")
		return 1
	}
}

// newMCPServer builds the MCP server and registers its tool catalog: the
// read-only/simple wrappers of ADR-0017's tool set. Approval tools are
// deliberately absent entirely (resolved decision 1) -- the human approves
// via `smind task approve` or the web UI, never the orchestrating agent.
func newMCPServer(client *wsclient.Client) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "smind",
		Title:   "smind",
		Version: version.Version,
	}, nil)
	registerMCPTools(srv, client)
	return srv
}
