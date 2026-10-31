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
	"path/filepath"

	"github.com/spacingmind/smind/internal/claudecli"
	"github.com/spacingmind/smind/internal/selfupdate"
)

func main() {
	// Windows only: the daemon starts smind.exe itself as the Claude CLI's
	// launcher (a Job Object around the real claude.exe, so killing the
	// launcher kills the whole tree). Must run before anything parses args;
	// a no-op everywhere else and whenever the launch env var is unset.
	if code, handled := claudecli.MaybeRunLauncher(); handled {
		os.Exit(code)
	}
	cleanupAfterUpdate()
	os.Exit(run(os.Args[1:]))
}

// cleanupAfterUpdate deletes the smind.exe.old a previous `smind update` could
// not remove while that binary was still running (Windows only; a file that is
// still in use is simply left for next time).
func cleanupAfterUpdate() {
	if !selfupdate.RenameAside {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real // update replaced the resolved file, so its leftovers sit next to that
	}
	selfupdate.CleanupStale(exe)
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
	case "stop":
		return cmdStop(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "relay":
		return cmdRelay(args[1:])
	case "workspace":
		return cmdWorkspace(args[1:])
	case "account":
		return cmdAccount(args[1:])
	case "space":
		return cmdSpace(args[1:])
	case "task":
		return cmdTask(args[1:])
	case "profile":
		return cmdProfile(args[1:])
	case "usage":
		return cmdUsage(args[1:])
	case "scratch":
		return cmdScratch(args[1:])
	case "mcp":
		return cmdMcp(args[1:])
	case "plugin":
		return cmdPlugin(args[1:])
	case "automation":
		return cmdAutomation(args[1:])
	case "validation":
		return cmdValidation(args[1:])
	case "skill":
		return cmdSkill(args[1:])
	case "learn":
		return cmdLearn(args[1:])
	case "push":
		return cmdPush(args[1:])
	case "cooldown":
		return cmdCooldown(args[1:])
	case "config":
		return cmdConfig(args[1:])
	case "set-password":
		return cmdSetPassword(args[1:])
	case "update":
		return cmdUpdate(args[1:])
	case "service":
		return cmdService(args[1:])
	case "version", "--version", "-v":
		return cmdVersion(osStdout)
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
	fmt.Fprint(w, `sMind is the Spacing Mind daemon and CLI.

Usage:
  smind serve                                             start the daemon

  smind stop                                              ask the running daemon to shut down gracefully
  smind status                                            which daemon (address, home, config) the CLI talks to

  smind config get [path]                                 show server.port / listen / hostnames as stored
  smind config set <path> <value>                         change one (comments kept; restart to apply)
  smind config unset <path>                               restore its default
  smind set-password [--password-stdin]                   set the password remote clients log in with
                                                           (bcrypt hash in config.yaml; needed to listen beyond localhost)
  smind service install|uninstall|status|start|stop|restart [--system]
                                                           run the daemon under systemd (Linux) or launchd (macOS)
  smind service install [--system] [--home <dir>] [--path <PATH>] [--no-start]
  smind service logs [-f] [-n <lines>]                    tail the daemon's own log (daemon.log)

  smind relay [--listen <addr>] [--data-dir <dir>]        start the self-hostable E2EE relay
  smind relay host new|ls|rm|rotate [<id>]                manage relay hosts (secret printed once)

  smind workspace create <repoPath> <name> <policy>       register a workspace
  smind workspace ls                                      list workspaces
  smind workspace setup show|set|clear <workspaceId>      worktree setup/teardown scripts
  smind workspace readiness [workspaceId] [--json]        check whether a repo is ready for agents
                                                           [--setup-file F|-] [--teardown-file F|-]

  smind account add [--base-url <url>] <provider> <label> < credential
                                                           register an account (credential from stdin)
  smind account ls                                        list accounts
  smind account login [--device] <provider> <label>       browser OAuth login (anthropic, openai);
                                                           --device: device-code login (openai), prints a code
  smind account rm <id>                                    remove an account (cascades routing/quota/workspace links)
  smind account test <provider>                           check whether a provider looks ready to run
  smind account usage [--refresh] [--json]               plan usage windows per account

  smind space create <workspaceId> <title>                create a space within a workspace
  smind space ls <workspaceId>                             list a workspace's spaces

  smind task new <workspaceId> <title> [--space <id>]     create a task, optionally scoped to a space
      [--budget <usd>]                                     cap the reported cost of its task tree (root tasks only)
  smind task budget <taskId> [<usd> | --clear]            show, set or clear a root task's tree budget (humans only)
  smind task ls <workspaceId>                              list a workspace's tasks
  smind task tree <workspaceId> | --task <taskId> [--json]  show the task hierarchy with run state, permissions and cost
  smind task send <taskId> <provider> <prompt>            start a run, stream it (Ctrl+C to detach)
                                                           [--chat <chatId>] [--mode <permissionMode>] [--auto-accept]
  smind task runs <taskId> [--chat <chatId>]              list a task's runs, optionally per chat
  smind task chat ls <taskId> [--all]                     list a task's chats (ID, title, provider, archived)
  smind task chat new <taskId> [title]                    create a chat
  smind task chat rename <chatId> <title>                 rename a chat
  smind task chat archive <chatId>                        archive a chat
  smind task mode <runId> [--auto-accept on|off] [<mode>]  change a live run's permission mode or auto-accept (humans only)
  smind task attach <runId>                                stream a run (Ctrl+C to detach)
  smind task logs <runId> [-f|--follow] [--tail N]        show (or follow) a run's history
  smind task stop <runId>                                  stop a running run
  smind task archive <taskId> [--delete-branch] [--force]  archive a task (shows what it puts at risk first)

  smind profile add <name> <provider> [--mode <permissionMode>] [--auto-accept]
      [--thinking-level=<level>] [--model=<model>]
      [--notes=<text>]                                    create an agent profile
  smind profile ls                                         list agent profiles
  smind profile rm <id>                                    remove an agent profile

  smind usage [--since <RFC3339>] [--until <RFC3339>]     token usage per account, model, or day
      [--by account|model|day]

  smind scratch usage|clean [--json]                      scratch disk usage (task temp dirs), or delete closed tasks' scratch

  smind plugin install <source>                           install a plugin DISABLED and show what it would run
                                                           (a directory, git+<url>@<40-hex sha>, or <archive>#sha256=<hash>)
  smind plugin link <dir>                                 use a directory in place (development; never copied or deleted)
  smind plugin list|show|enable|disable|remove|update|reload|logs
                                                           manage plugins (humans only, local only; run "smind plugin" for flags)

  smind automation list|show|create|enable|disable|resume|fire|audit|memory|delete
                                                           signal -> task rules (humans only, local only; run "smind automation" for flags)
  smind validation contract submit|show, list|show        write a validation contract before code; read validator verdicts
                                                           (run "smind validation" for flags; smind never merges)

  smind skill ls [--target claude|agents|codex|all]       bundled orchestration skills x targets (installed/modified/missing)
  smind skill install [--target ...] [--force] [name...]  install the bundled skills (explicit; no daemon needed)
  smind skill uninstall [--target ...] [name...]          remove only the files smind installed

  smind learn status|ls|show|approve|reject|pin|unpin|disable|enable|archive|restore|delete|undo|reflect|curate|mode|forget
                                                           manage learned skills and proposals (humans only; run "smind learn" for flags)

  smind push test [<device id prefix>]                    send a test notification to phones registered for push

  smind cooldown ls                                       list providers cooling down after a rate limit
  smind cooldown clear <provider>                         I reset my quota: clear a provider cooldown, retry held runs now (humans only)

  smind update [--check] [--version <v>] [--force]        update this binary to the latest release (SHA-256 verified)
      [--dry-run] [--json]                                 --check only reports; refuses while runs are active unless --force

Every subcommand except "serve", "skill", "config", "set-password" and "update" talks to a locally running daemon over its
WebSocket API, authenticating with the token the daemon itself wrote to
~/.smind/token (override the home directory with $SMIND_HOME).
`)
}
