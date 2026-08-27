// Command smind is both the Spacing Mind daemon (`smind serve`) and the
// CLI client that talks to it (`smind workspace ...`, `smind task ...`) --
// one binary, subcommand-dispatched, modeled on Paseo's real CLI
// (refs/paseo/public-docs/cli.md) but much smaller in scope: no
// project/plugin/schedule/hub/permission/agent-mode/multi-host machinery,
// no --host remote daemon support (smind is single-machine for now), and
// no persistent multi-turn sessions (every send/prompt is a fresh one-shot
// subprocess turn -- see docs/plans/active/run-registry-and-cli.md).
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches to a subcommand and returns the process exit code, rather
// than calling os.Exit itself, so it stays testable (and so deferred
// cleanup in subcommands, e.g. closing a *wsclient.Client, actually runs
// before the process exits).
func run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 2
	}

	switch args[0] {
	case "serve":
		return cmdServe(args[1:])
	case "workspace":
		return cmdWorkspace(args[1:])
	case "space":
		return cmdSpace(args[1:])
	case "task":
		return cmdTask(args[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "smind: unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `smind is the Spacing Mind daemon and CLI.

Usage:
  smind serve                                             start the daemon

  smind workspace create <repoPath> <name> <policy>       register a workspace
  smind workspace ls                                      list workspaces

  smind space create <workspaceId> <title>                create a space within a workspace
  smind space ls <workspaceId>                             list a workspace's spaces

  smind task new <workspaceId> <title> [--space <id>]     create a task, optionally scoped to a space
  smind task ls <workspaceId>                              list a workspace's tasks
  smind task send <taskId> <provider> <prompt>            start a run, stream it (Ctrl+C to detach)
  smind task attach <runId>                                stream a run (Ctrl+C to detach)
  smind task logs <runId> [-f|--follow] [--tail N]        show (or follow) a run's history
  smind task stop <runId>                                  stop a running run

Every subcommand except "serve" talks to a locally running daemon over its
WebSocket API, authenticating with the token the daemon itself wrote to
~/.spacingmind/token (override the home directory with $SMIND_HOME).
`)
}
