# sMind

[![CI](https://github.com/spacingmind/smind/actions/workflows/ci.yml/badge.svg)](https://github.com/spacingmind/smind/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/spacingmind/smind)](https://goreportcard.com/report/github.com/spacingmind/smind)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-blue.svg)](LICENSE)

**Space for your agents, free for you.**

sMind is a self-hosted platform for running coding agents, built around
per-workspace account routing so you can spread agent traffic across your own
provider accounts instead of a single shared pool. It ships as a single Go
binary that serves both the API and an embedded web UI.

> **Status: early / pre-1.0.** The core daemon, CLI, and web UI are usable
> day to day, but expect rough edges and breaking changes before `v1`. See
> [docs/ROADMAP.md](docs/ROADMAP.md) for what's done and what's next.

## Features

- **Multi-account routing** — spread requests across your own Anthropic,
  OpenAI, and other provider accounts with session affinity and automatic
  failover, instead of relying on a single shared credential.
- **Workspace → Space → Task model** — each task runs in a real, isolated
  `git worktree`.
- **Multiple agent backends** — drives agents over the [Agent Client
  Protocol](https://agentclientprotocol.com) (GLM and other ACP-speaking
  agents) or Claude Code's native headless protocol, behind one unified
  interface.
- **Embedded web UI** — agent chat timeline, a file explorer with a
  CodeMirror editor, a real diff viewer, an embedded terminal (real PTY),
  and inline permission prompts — all driven over a single WebSocket
  connection to the daemon.
- **CLI** — `workspace`/`space`/`task` management and `task
  send`/`attach`/`logs`/`stop`, modeled on real-world daemon CLIs (detaching
  never stops a run in progress).
- **MCP server** — `smind mcp serve` exposes the same orchestration surface
  ([ADR-0017](docs/decisions/0017-mcp-server.md)) to agent harnesses over
  stdio, so an orchestrating agent can drive sub-agents without shelling
  out to the CLI.

## Install

On Linux or macOS:

```sh
curl -fsSL https://spacingmind.com/install.sh | sh
```

[`scripts/install.sh`](scripts/install.sh) downloads the latest release for
your OS/arch, verifies it against `checksums.txt`, and installs `smind` to
`/usr/local/bin` (or `~/.local/bin` if that isn't writable). Pin a release
with `--version` (`curl -fsSL https://spacingmind.com/install.sh | sh -s --
--version 0.11.1`) or `SMIND_VERSION=0.11.1` (the flag wins if both are set),
and choose the target directory with `SMIND_INSTALL_DIR`. Re-running it
installs over the previous version, so it is also the update path.

On Windows, in PowerShell (5.1 or 7):

```powershell
irm https://raw.githubusercontent.com/spacingmind/smind/master/scripts/install.ps1 | iex
```

[`scripts/install.ps1`](scripts/install.ps1) downloads
`smind_<version>_windows_<amd64|arm64>.zip` and `checksums.txt` from the
release, verifies the SHA-256, and installs `smind.exe` to
`%LOCALAPPDATA%\Programs\smind`, adding that directory to your user `PATH`
once (open a new terminal afterwards). It honours `SMIND_VERSION` and
`SMIND_INSTALL_DIR` the same way (`$env:SMIND_VERSION = "0.11.1"` before the
command), is safe to re-run to update, and leaves any previous install
untouched if the download, checksum or extraction fails. It never writes to `%LOCALAPPDATA%\smind\`, the
directory the [Windows desktop app](#windows-desktop-app) manages; the two
installs are independent. Git for Windows is required (the script warns if it
is missing).

> `https://spacingmind.com/install.ps1` will serve the same script once the
> website redirect exists; until then use the raw GitHub URL above. The
> script is on `master` from the first release that includes it.

Then `smind serve` and open <http://localhost:4648>. To keep the daemon running across logouts and reboots on Linux or
macOS, or at every logon on Windows, run `smind service install` (or pass `--service` to the install script, `-Service` to install.ps1); see
[docs/homelab.md](docs/homelab.md#run-the-daemon-as-a-service-linux-macos-and-windows).

From source:

```sh
go install github.com/spacingmind/smind/cmd/smind@latest
```

### macOS desktop app

Each release attaches one unsigned `.dmg` per architecture
(`smind-desktop-<version>-macos-arm64.dmg` for Apple Silicon,
`…-macos-x86_64.dmg` for Intel; macOS 13 or newer). The app bundles the
matching `smind` daemon and starts it for you on first launch, so there is
nothing else to install. Quitting the app leaves the daemon running; reopen
it from the Dock to reconnect.

The app is not signed or notarized, so Gatekeeper blocks a DMG downloaded
through a browser. After dragging `sMind.app` to `/Applications`, clear the
quarantine flag once:

```sh
xattr -dr com.apple.quarantine /Applications/sMind.app
```

(Installs from releases before 0.12 are named `smind.app`; they keep updating
in place under that name.)

To build and install it yourself (needs Go, bun and Rust; a locally built app
has no quarantine flag and opens straight away):

```sh
task desktop:mac:install   # builds sMind.app + .dmg, copies the app to /Applications
task desktop:mac           # build only; output under desktop/src-tauri/target/<triple>/release/bundle/
```

### Windows desktop app

Each release attaches an x64 installer (NSIS `*-setup.exe` and `.msi`). The installer bundles the matching native `smind.exe`:
**Install** or **Update** in the app copies it to
`%LOCALAPPDATA%\smind\bin\` and runs it from there — nothing is downloaded,
so the daemon's version always matches the app's. Git for Windows is
required. An existing daemon running inside WSL is never touched: it can
still be used as a remote connection, but it must be stopped first if it
holds the port when you press **Install**.

### Updating

The macOS and Windows desktop apps check for updates on GitHub Releases
(shortly after launch, and from **Check for Updates…** in the app menu on
macOS / the Help menu on Windows). An update is only offered if its version
is strictly newer and a stable release; you're prompted before anything is
downloaded, and the package's signature is verified against a public key
built into the app — the corresponding private key is a repository secret
(`TAURI_SIGNING_PRIVATE_KEY`) used only by the release build.

- The **first release carrying this feature must be installed by hand**;
  only later releases can update in place.
- If the signing key is ever lost or rotated, installed apps can no longer
  verify new releases — one manual reinstall is needed, after which the new
  key's releases update normally again.
- The Linux `deb` package has no updater: install new versions by hand.

#### Updating the `smind` binary itself

`smind update` (`--check` to only look, `--version <v>`, `--dry-run`, `--force`) replaces
the CLI/daemon binary from a GitHub Release. It verifies the archive's SHA-256 against
`checksums.txt` **and** a minisign signature of `checksums.txt` (`checksums.txt.minisig`,
made with the same key as the desktop updater, whose public half is built into `smind`);
there is no flag to skip the signature, not even `--force`. A release without that
signature is refused ("release has no signature"), so **releases published before the first
signed one cannot be updated to with `smind update`**: install them with the install
script instead (the install scripts verify the SHA-256 only). If that key is ever rotated,
`smind` binaries built before the rotation cannot verify new releases, the same one-time
manual reinstall as the desktop app.

### Installing the daemon from a release manually

Each [GitHub Release](https://github.com/spacingmind/smind/releases) ships
prebuilt `smind` daemon binaries for linux/amd64, linux/arm64, darwin/amd64
and darwin/arm64 as `.tar.gz`, plus `smind_<version>_windows_amd64.zip` and
`smind_<version>_windows_arm64.zip` (each containing `smind.exe`), and a
`checksums.txt`. To install one:

```sh
version=0.8.0   # match the release you're installing
os=linux        # or darwin
arch=amd64      # or arm64

curl -fsSLO "https://github.com/spacingmind/smind/releases/download/v${version}/smind_${version}_${os}_${arch}.tar.gz"
curl -fsSLO "https://github.com/spacingmind/smind/releases/download/v${version}/checksums.txt"
sha256sum --ignore-missing -c checksums.txt
tar -xzf "smind_${version}_${os}_${arch}.tar.gz"
./smind --version
```

The native Windows daemon ships as `smind_<version>_windows_<arch>.zip`
(amd64/arm64) with `smind.exe` at the archive root. `install.ps1` above
automates installing it; to do it by hand, unzip it anywhere and run
`smind.exe serve`. The desktop app does not need this — its installer
bundles the daemon (amd64; on ARM64 Windows it runs under x64 emulation).

## Quickstart

```sh
smind serve                                          # start the daemon (http://localhost:4648)
smind workspace create /path/to/repo "my project" hard
smind task new <workspaceId> "fix the failing test"
smind task send <taskId> glm "fix the failing test"   # or open the web UI and use the Chat tab
```

### Permission modes

```sh
smind task send <id> claude-native <prompt> --mode plan
smind task send <id> glm <prompt> --auto-accept
```

Each run uses its provider's own permission modes
([ADR-0019](docs/decisions/0019-provider-native-permission-modes.md)):

- **Claude Code:** `acceptEdits` (default), `default`, `plan`, `auto`
  (Claude Code's classifier), `bypassPermissions`.
- **Codex:** `auto` (default) or `full-access`.
- **ACP agents (GLM, Kimi):** whatever modes the agent advertises, plus
  `--auto-accept` to approve every permission prompt.

Anything the chosen mode still escalates waits for a human in the web UI or
`smind task approve`. The web UI's prompt form has the same mode picker.

### MCP server (`smind mcp serve`)

`smind mcp serve` ([ADR-0017](docs/decisions/0017-mcp-server.md)) runs an
MCP server over stdio, for orchestrating agents (Claude Code, Cursor, ...)
to drive sMind sub-agents without the CLI shell-and-poll loop:

```json
{ "mcpServers": { "smind": { "command": "smind", "args": ["mcp", "serve"] } } }
```

The daemon (`smind serve`) must already be running; the server reuses the
CLI's own token and fails fast if it can't connect. Tools:
`task_new`, `task_list`, `chat_list`, `chat_new`, `task_send`,
`task_wait`, `task_status`, `task_logs`, `task_permissions`, `task_stop`.
`task_send` returns a `runId` immediately; `task_wait` blocks until the run
finishes, a permission goes pending, or its timeout elapses.

There are deliberately **no approval tools** — an orchestrating agent must
never resolve a sub-agent's permission request; it surfaces it (via
`task_permissions`/`task_wait`) and the human approves in the web UI or
with `smind task approve`. For the same reason ([ADR-0019](docs/decisions/0019-provider-native-permission-modes.md)
decision 6), `task_send` rejects an auto-approving `permissionMode`
(bypass modes) or `autoAccept: true` chosen by the caller itself — to run
with such settings, pass a human-authored agent profile's `profileId`
instead.

## Orchestration skills

sMind bundles five agent skills (`smind`, `smind-leader`, `smind-handoff`,
`smind-advisor`, `smind-committee`) that teach an orchestrating agent the
MCP tool loop and the leader/coder pattern. They are plain `SKILL.md` files
embedded in the binary and installed only when you ask:

```sh
smind skill ls                       # bundled skills x targets: installed/modified/missing
smind skill install [--target claude|agents|codex|all] [--force]
smind skill uninstall
```

Install writes only under `~/.claude/skills`, `~/.agents/skills` and
`~/.codex/skills`, tracks what it wrote in a per-skill
`.smind-managed-files.json` manifest, never overwrites a file you edited
(without `--force`) and never touches other skills. No daemon is needed.
To let an agent drive sMind, add the MCP server: `smind mcp add smind stdio
--command smind --arg mcp --arg serve` (or run `smind mcp serve` as a stdio
server).

## Dev quickstart

```sh
task build      # build the web UI, then the smind binary (bin/smind)
task dev        # daemon + web UI with hot reload (one Ctrl+C)
task dev:go     # run the Go daemon with hot reload
task dev:web    # run the Vite dev server, proxying /healthz to :4648
task test       # go test ./... (plus the web UI's test suite)
task lint       # go vet + gofmt check
```

See [docs/](docs/) for architecture and design decisions.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branching model, Conventional
Commits convention, and spec-driven development practice this repo follows.

## License

[AGPL-3.0](LICENSE)
