---
title: Getting started
description: Install smind and run your first task.
summary: "This page shows developers how to install sMind by Spacing Mind (the CLI, the desktop app or the mobile app), start the daemon, and run a first task in a new workspace."
order: 2
---

# Getting started

smind is a daemon with a web UI and a command line. This page installs it,
starts the daemon, and runs a first task.

## Install the CLI

On Linux or macOS:

```sh
curl -fsSL https://spacingmind.com/install.sh | sh
```

The script downloads the latest release for your OS and architecture, checks
it against `checksums.txt`, and installs `smind` to `/usr/local/bin`. If that
directory is not writable it uses `~/.local/bin`.

Options and environment variables:

| Option | Effect |
| --- | --- |
| `--version <v>` | Install a specific release, for example `--version 0.11.1`. |
| `SMIND_VERSION` | Same as `--version`. The flag wins if both are set. |
| `SMIND_INSTALL_DIR` | Install into this directory. |
| `--service` | After installing, run `smind service install` (a systemd user unit on Linux, a LaunchAgent on macOS). |
| `--system` | With `--service` on Linux, install a system unit. Run the script with `sudo`. |

```sh
curl -fsSL https://spacingmind.com/install.sh | sh -s -- --version 0.11.1
```

Run the script again to update. You can also update an installed binary with
`smind update`.

On Windows, in PowerShell 5.1 or 7:

```powershell
irm https://raw.githubusercontent.com/spacingmind/smind/master/scripts/install.ps1 | iex
```

The script installs `smind.exe` to `%LOCALAPPDATA%\Programs\smind` and adds
that directory to your user `PATH`. Open a new terminal afterwards. It checks
the SHA-256 of the download, honours `SMIND_VERSION` and `SMIND_INSTALL_DIR`,
and leaves a previous install untouched if anything fails. Git for Windows is
required. To also run the daemon at every logon, set `SMIND_SERVICE=1` before
the command.

From source, with Go installed:

```sh
go install github.com/spacingmind/smind/cmd/smind@latest
```

## Install the desktop app

The desktop app bundles the matching `smind` daemon and starts it for you. Each
release carries the installers.

### macOS

Each release has one `.dmg` per architecture: `smind-desktop-<version>-macos-arm64.dmg`
for Apple Silicon and `smind-desktop-<version>-macos-x86_64.dmg` for Intel. It
needs macOS 13 or newer.

The app is not notarized yet. A copy downloaded in a browser is quarantined, so
Gatekeeper blocks it or warns that it cannot be verified. That is expected.
Do one of these:

- Drag `sMind.app` to `/Applications`, then right-click it, choose **Open**,
  and confirm **Open** in the dialog.
- Clear the quarantine flag once from Terminal:

  ```sh
  xattr -dr com.apple.quarantine /Applications/sMind.app
  ```

Or install with the script. It downloads with `curl`, which sets no quarantine
flag:

```sh
curl -fsSL https://spacingmind.com/install.sh | sh -s -- --desktop
```

The script puts `sMind.app` in `/Applications`, or in `~/Applications` if that
is not writable. `SMIND_APP_DIR` picks another folder. Quit sMind first: the
script refuses to replace a running app and never closes it.

Quitting the app leaves the daemon running. Reopen the app to reconnect.

### Windows

Each release has an x64 installer, as a `*-setup.exe` and as a `.msi`. The
installer bundles `smind.exe`. **Install** or **Update** in the app copies it to
`%LOCALAPPDATA%\smind\bin\` and runs it from there. Git for Windows is
required.

The app installs separately from the command-line install above. Both can exist
at once.

## Install the mobile app

The repository does not document an app store listing. The app lives in the
`mobile/` directory and you build it yourself:

```sh
cd mobile
npm ci
npx expo run:ios      # or: npx expo run:android
```

Expo Go cannot run it; use a development build. After it is installed, pair the
phone with your daemon through a relay. See [Mobile and relay pairing](mobile-and-relay.md).

## Start the daemon

```sh
smind serve
```

The daemon listens on `http://127.0.0.1:4648` by default. Open
<http://localhost:4648> in a browser for the web UI. The desktop app starts a
daemon for you, so skip this step there.

To keep the daemon running across logouts and reboots, run
`smind service install`. Check on it with `smind status`. To change the port,
see [Configuration](configuration.md).

Before you can run agents, log in to a provider. See [Providers](providers.md).

## Create your first workspace

A workspace is an existing git repository. Tasks are created as worktrees
branched off it.

From the command line:

```sh
smind workspace create /path/to/repo "my project" hard
smind workspace ls
```

The path must be absolute. `ls` prints each workspace's ID, which you need for
the next step.

In the web UI, open the **New workspace** dialog from the sidebar, enter the
repository path (or use **Browse…**), optionally a title, and choose
**Create workspace**.

## Create your first task

From the command line:

```sh
smind task new <workspaceId> "fix the failing test"
smind task send <taskId> glm "fix the failing test"
```

`task new` creates the task and its worktree. `task send` starts a run with the
named provider and streams it. Press Ctrl+C to detach. The run keeps going.
The provider id is one of `claude-native`, `codex-native`, `glm` or `kimi`.

In the web UI, use **Add task** in the workspace's menu in the sidebar, enter
a title, and choose **Create task**. Then send a prompt from the task's chat.

For every command, see the [CLI reference](cli-reference.md).
