---
title: CLI reference
description: Every smind command, with the usage text the binary prints.
summary: "This page lists every command of the smind binary, the daemon and command line of sMind by Spacing Mind, with the usage text the binary prints. It is for people who script or operate smind from a terminal."
order: 10
---

# CLI reference

The `smind` binary is the daemon and the command line. Most commands talk to a
running daemon (start one with `smind serve`). Set `SMIND_HOME` to use another
home directory than `~/.smind`.

## Command summary

This block is generated from `smind --help`. Do not edit it by hand: run
`task docs:cli` to refresh it.

<!-- BEGIN GENERATED: smind --help -->
```text
sMind is the Spacing Mind daemon and CLI.

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

  smind provider update <id> [--models a,b] [--add-models a,b]   edit an instance in config.yaml: its model list, extra models, disallowed tools
      [--disallow T,U] [--inherit-claude-config=true|false]     (humans only; applied live; keys stay on the Providers settings)

  smind update [--check] [--version <v>] [--force]        update this binary to the latest release (SHA-256 verified)
      [--dry-run] [--json]                                 --check only reports; refuses while runs are active unless --force

Every subcommand except "serve", "skill", "config", "set-password" and "update" talks to a locally running daemon over its
WebSocket API, authenticating with the token the daemon itself wrote to
~/.smind/token (override the home directory with $SMIND_HOME).
```
<!-- END GENERATED: smind --help -->

### cooldown

After a rate limit the daemon holds a provider cooldown until the parsed
reset time and keeps `--on-rate-limit=wait` retries waiting until then.
`smind cooldown ls` shows which providers are still cooling down and when
they reset. When you reset your quota by hand, `smind cooldown clear
<provider>` clears the cooldown and retries the held runs now (humans only).

### provider update

`smind provider update <id>` edits one custom instance in config.yaml:
`--models a,b` replaces its model list, `--add-models a,b` adds models to the
provider's own catalog, `--disallow T,U` names tools the instance may not
call, and `--inherit-claude-config=true|false` shares ~/.claude instead of a
private config directory. An explicitly empty value (`--models ""`) removes
the field; a flag left out leaves it alone (humans only; the daemon applies
the change live, and keys are edited on the Providers settings page).

## Relay

This block is generated from `smind relay --help`.

<!-- BEGIN GENERATED: smind relay --help -->
```text
smind relay — self-hostable E2EE relay server

Usage:
  smind relay [--listen <addr>] [--grpc-web-listen <addr>] [--data-dir <dir>] [--max-devices-per-host <n>]
              [--tls-cert <pem> --tls-key <pem>]           start the relay server (default 16 devices per relay host);
                                                           with --tls-cert/--tls-key it serves your CA certificate
                                                           (e.g. Let's Encrypt) instead of a self-signed one
  smind relay host new <id>                               enroll a relay host (secret printed once)
  smind relay host ls                                     list enrolled relay hosts
  smind relay host rm <id>                                remove a relay host
  smind relay host rotate <id>                            issue a new secret (printed once; the old one stops admitting)
  smind relay connect [--fingerprint <hex>] <address> <host-id> <secret>
                                                           configure this daemon to use a relay
  smind relay offer [--window <duration>] [--max-devices <n>] [--qr] [--png <file> [--force]]
                                                           print a pairing link (and QR code) for a mobile device or the desktop app
                                                           and open a pairing window (default 10m, 1 new device)
  smind relay devices ls [--pending]                      list paired devices (and those awaiting approval)
  smind relay devices approve <id>                        approve a pending device
  smind relay devices revoke <id>                         revoke a device (it is disconnected and cannot return)
  smind relay devices rename <id> <label>                 change a device's label

Device ids accept any unique prefix. With the daemon running, "relay devices"
and "relay offer" go through it; otherwise they edit the device registry file
directly and the change applies when the daemon next starts.

Trusting the relay's certificate: "relay connect" first verifies it like a web
browser would (public CA, matching host name, not expired); if that works nothing
is pinned. Otherwise (a self-signed relay, the default) it shows the certificate
fingerprint and asks you to confirm it, or takes --fingerprint <hex>; it never
pins a certificate silently.

A relay host is one daemon's mailbox on a relay: a host id plus an admission
secret. ("smind relay workspace ..." is the deprecated name for "smind relay
host ...".) Host changes take effect when the relay restarts.

"smind relay offer" prints the pairing link on stdout; on a terminal, or with
--qr, it also draws a QR code on stderr, and --png writes one to a file. Treat
the link like a password: anyone with it can reach this daemon through the
relay; to revoke it, run "smind relay host rotate". Traffic is end-to-end
encrypted; the relay cannot read your code or messages.

The relay is a dumb pipe: it authenticates relay hosts (SCRAM-style
challenge-response) and forwards opaque ciphertext between the daemon and
paired mobile devices. It never sees plaintext. Data lives under
$SMIND_HOME/relay (default ~/.smind/relay): a self-signed TLS
certificate (pin its fingerprint, printed at startup, at pairing time)
and the relay host secret hashes.

"smind relay connect"/"smind relay offer" are the OTHER side: they make
this daemon a client of some relay (self-hosted or not), so "smind serve"
dials out to it in the background and a paired mobile device can reach
this daemon's workspaces through it.

Connecting registers this daemon once with the relay; every paired client sees all of this daemon's workspaces, spaces and tasks; nothing is created in smind.
```
<!-- END GENERATED: smind relay --help -->

## Commands the summary leaves out

These commands exist but are not in the summary above. The usage lines are the
ones the binary prints when you run a command with the wrong arguments.

```sh
smind task wait <runId|item:ID> [--timeout <duration>]
smind task permissions <runId>
smind task approve <runId> [requestId] [optionId]
smind task deny <runId> [requestId]
smind task options <runId>
smind task set-option <runId> <configId> <value>
smind task queue ls --chat <chatId>
smind task queue cancel <itemId>
smind profile edit <id> [--name=..] [--provider=..] [every `profile add` flag]
smind profile seed roles
smind mcp serve
smind mcp add <name> <stdio|http|sse> [--command <cmd>] [--arg <value>]... [--url <url>] [--env <KEY=VALUE>]... [--header <KEY=VALUE>]...
smind mcp ls
smind mcp rm <id>
smind mcp enable <id>
smind mcp disable <id>
smind relay connect [--fingerprint <hex>] <relay-address> <host-id> <secret>
smind relay offer [--window <duration>] [--max-devices <n>] [--qr] [--png <file> [--force]]
smind relay devices ls [--pending]
smind relay devices approve <id>
smind relay devices revoke <id>
smind relay devices rename <id> <label>
```
