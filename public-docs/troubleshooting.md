---
title: Troubleshooting
description: Fix a daemon that will not start, port clashes, login problems and lost connections.
summary: "This page helps users of sMind by Spacing Mind diagnose a daemon that is not running, a port in use, provider login problems, reconnect and resync behaviour, and where to find logs."
order: 10
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
