---
title: Troubleshooting
description: Fix a daemon that will not start, port clashes, login problems and lost connections.
summary: "This page helps users of sMind by Spacing Mind diagnose a daemon that is not running, a port in use, provider login problems, reconnect and resync behaviour, and where to find logs."
order: 11
---

# Troubleshooting

## The daemon is not running

The web UI, the desktop app and most `smind` commands talk to a running daemon.
If they cannot connect, check what the CLI sees:

```sh
smind status
```

It prints the daemon address, the home directory and the config file. A line
`status  NOT reachable: ...` means nothing answers at that address.

Start the daemon:

```sh
smind serve
```

If you installed it as a service, check and restart the service instead:

```sh
smind service status
smind service restart
```

`smind service status` says `not installed` when there is no service. Install
one with `smind service install`.

If `smind status` shows a different home than you expect, the `SMIND_HOME`
environment variable is set in this shell. The CLI and the daemon must use the
same home. See [Configuration](configuration.md).

## The port is in use

The daemon listens on port 4648 by default. If another program holds the port,
`smind serve` stops with a `listen:` error and starts nothing. Pick another
port, then start again:

```sh
smind config set server.port 4650
smind serve
```

A daemon that is already running keeps its old port until it restarts. If one
is running, stop it first with `smind stop`.

The CLI reads the same setting, so later `smind` commands find the daemon on
the new port. Open the web UI on the new port too.

If you set `server.listen` as well, both must name the same port. Otherwise
the daemon logs a `config:` error and carries on with its default settings, so
it may not listen where you expect. Read the log (see below) when a setting
seems to be ignored.

## Login problems

**The provider does not start.** Run `smind account test <provider>`. For
`claude-native`, `codex-native` and `glm` it tells you whether the command
(`claude`, `codex`, `npx`) is on the daemon's `PATH`. A daemon started from a
service or the desktop app may have a shorter `PATH` than your shell. Fix the
`PATH` or reinstall the command where the daemon can see it.

**Agent runs fail to log in.** Claude Code and Codex runs use the command's own
login, not an account in smind. Log in with `claude` or `codex` in a terminal.
If you use another config directory, set it under **Settings** → **Providers**.
See [Providers](providers.md).

**A browser login does not finish.** `smind account login` prints a URL. Open
it, even if no browser opened on its own. The login waits for a callback on
`localhost:54545` for Anthropic and on `localhost:1455` for OpenAI, so those
ports must be free on the machine that runs the daemon. On a machine without a
browser, use `smind account login --device openai <label>` and enter the code it
prints. Press Ctrl+C to cancel a login that hangs.

**The web UI asks for a password.** A daemon that listens beyond localhost
needs a password. Set one with `smind set-password`, then restart the daemon.
To start the daemon on a LAN or Tailscale address, see `server.listen` in
[Configuration](configuration.md).

**A host name is rejected.** The daemon accepts `localhost` and IP addresses.
Add any other name you reach it by:

```sh
smind config set server.hostnames smind.example.com
```

## Reconnect and resync

If the daemon restarts or the connection drops, the web UI reconnects by
itself. The status shows `Reconnecting to daemon…` and the UI retries with a
growing, randomized delay of up to 10 seconds. When the connection returns, it
shows `Connected to daemon` and reloads its data: workspaces, tasks and open
runs are fetched again. You do not need to refresh the page.

If your session has expired, the UI shows the login form instead of retrying.
Sign in again.

If it shows `Disconnected` and does not recover, the first connection failed or
the daemon is gone. Check `smind status`, start the daemon, and reload the page.

The desktop app shows the same UI. For a phone, see
[Mobile and relay pairing](mobile-and-relay.md).

## Where the logs are

The daemon writes `daemon.log` in the smind home directory (`~/.smind` unless
`SMIND_HOME` says otherwise). It also writes to the terminal when you run
`smind serve`. Read the log with:

```sh
smind service logs
smind service logs -n 200
smind service logs -f
```

`-n` sets how many lines to print (100 by default), and `-f` follows the log
until Ctrl+C. If there is no log yet, the daemon has not run.

The file rotates at 10 MB and keeps 5 files. Change that with `log.maxSizeMB`
and `log.maxFiles`. See [Configuration](configuration.md).

In the desktop app, **Settings** → **Daemon** shows the log file path.

## smind.db is very large

Most of the file is the detailed history of finished runs. Every day the
daemon removes the large tool output (the copies over 512 bytes) of finished
runs older than `runs.historyRetentionDays` (30 by default) of archived tasks,
while their prompts, answers and tool-call summaries stay; runs finished before
this rule, and runs the daemon could not convert, are removed whole.
`smind runs prune --dry-run` shows what that would remove. The file only
shrinks after `smind db compact` (see the CLI reference).

## The end of a run's output is missing after a crash

If the daemon was killed hard (`kill -9`, a power loss, a crash) while a run was
streaming, the last moments of that run's output can be missing: smind buffers
the open text for up to 250 ms or 4 KiB before writing it, and a hard kill
loses that buffer, at most 250 ms or 4 KiB per running run. The run comes back
as interrupted. A normal stop, an agent error or a daemon shutdown (including
`smind service stop`) writes the buffer first and loses nothing.

## A run shows "Showing a summary"

Once a run has finished, smind keeps its prompts, answers, tool calls and a trimmed copy of large tool output
(see [Configuration](configuration.md)). Opening it shows a note above the transcript saying so. For a Claude Code or Codex
run, **Load full history** asks smind to read the complete turn back from that CLI's own session file on this machine. It
only works while that file is still there, and only for you: agents and automations always read the summary. If it cannot,
the note says why and the summary stays:

| The note says | What it means |
| --- | --- |
| the provider's session file is no longer there | the CLI deleted or moved it (check its own retention setting) |
| this turn could not be matched | the session no longer holds this run's turn, or two turns look the same |
| the session format was not recognised | the CLI changed its file format; smind falls back until it is updated |
| could not be loaded (too large or too slow) | the session file is over 64 MB or reading it took over 10 s |
| this provider does not keep a history smind can read | other providers have no session file smind can open |
| another history is loading / this chat is running | try again in a moment, or when the chat's run ends |
| large tool output was removed after the retention period | the daily retention pass deleted the trimmed copies of large tool output; the summary stays, and Load full history still works while the provider's session file exists |

## The daemon uses a lot of memory

Run `smind debug memory`. The daemon keeps only a bounded amount of finished
runs' output in memory (`runs.historyBudgetMB`, 64 MB by default); the rest is
read from the database when you open a run, so a big `smind.db` does not mean a
big process. If the numbers show something else growing, enable
`debug.pprof` and take a heap profile (see [CLI reference](cli-reference.md)).
