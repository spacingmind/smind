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

On macOS, add `--desktop` to install the sMind desktop app instead of the CLI;
see [macOS desktop app](#macos-desktop-app) (the app is not notarized yet).

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

**The macOS app is not notarized yet.** It is ad-hoc signed only (there is no
Apple Developer ID behind it), so a DMG or `.app` downloaded in a browser is
quarantined and Gatekeeper blocks it, or warns that it "cannot be verified", on
first open. That is expected, not a corrupt download. Either:

- drag `sMind.app` to `/Applications`, then **right-click it → Open** (and
  confirm **Open** in the dialog); or
- clear the quarantine flag once from Terminal:

  ```sh
  xattr -dr com.apple.quarantine /Applications/sMind.app
  ```

Why: Gatekeeper checks apps that carry the `com.apple.quarantine` attribute,
which browsers set on downloads, and an app without a notarized Developer ID
signature fails that check. Developer ID signing and notarization are deferred
until there are users who need them
([plan](docs/plans/active/desktop-macos-app.md)).

Or skip the prompt altogether and install with the script, which downloads the
app with `curl` (that sets no quarantine flag), verifies it against
`checksums.txt` like the CLI install, and swaps it into `/Applications`
(`~/Applications` if that isn't writable) without touching a copy that is
running:

```sh
curl -fsSL https://spacingmind.com/install.sh | sh -s -- --desktop
```

(`SMIND_APP_DIR` picks another folder; `--version` pins a release; quit sMind
first, because the script refuses to replace a running app and never closes it
for you.) The command-line install above is unaffected by any of this: `curl`
downloads aren't quarantined, so `smind` itself opens without a prompt.

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
- Developers: `daemon_survives_app_update` (`desktop/src-tauri/src/daemon_survival_test.rs`)
  proves a managed daemon keeps its pid, port and in-flight terminal across an
  app update; it runs against a real daemon with
  `SMIND_REAL_DAEMON_BIN=<built smind> cargo test --lib daemon_survi`.
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

### Docker (homelab)

`ghcr.io/spacingmind/smind` (amd64 and arm64) runs the daemon with the web UI. It listens on all interfaces, so it **requires a
password** (`SMIND_PASSWORD`, at least 12 characters) and refuses to start without one:

```sh
docker run -d -p 4648:4648 -e SMIND_PASSWORD="$(openssl rand -base64 24)" \
  -v smind-data:/data -v "$PWD/workspaces:/workspaces" ghcr.io/spacingmind/smind
```

See `docker/docker-compose.example.yml` and [docs/homelab.md](docs/homelab.md#docker) for volumes, the threat model, and adding
the Claude/Codex agent CLIs with a child image.

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

### Driving agents from a script

`smind task send` streams the run and blocks until it ends. For orchestration,
start it and come back later:

```sh
RUN=$(smind task send <taskId> glm "<prompt>" --auto-accept --detach)   # prints just the run id
smind task logs "$RUN" -f      # follow it (also: smind task attach "$RUN")
smind task runs <taskId>       # status, and the MODEL each run used
```

Output stays readable by default: raw ACP session updates (such as the skill
list an agent advertises, several KB of JSON) are hidden, and with
`--auto-accept` each permission the daemon approves prints as one line
(`[permission] auto-accepted Run command: go test ./... -> allow`) instead of
the approve/deny block. Pass `--verbose` to `send`, `attach` or `logs` to see
the raw events. A prompt that needs a human still prints in full.

**Unattended Claude agents.** `--auto-accept` is for ACP agents (GLM, Kimi) only;
Claude Code has its own permission modes ([ADR-0019](docs/decisions/0019-provider-native-permission-modes.md)).
To run a claude-native agent without anyone approving each command, pick its
classifier mode, and always pin the model (an unpinned run uses the CLI's own
default, which may be Opus):

```sh
smind task send <taskId> claude-native "<prompt>" --model claude-sonnet-5-5 --mode auto --detach
```

`--mode auto` lets Claude Code's own model classifier approve each prompt, so no
`task approve` is needed. It is not a sandbox: the classifier follows what the
prompt asks for, so an explicit instruction to do something destructive will
probably be approved. Keep briefs narrow and run agents in their own task
worktree. `--mode bypassPermissions` skips prompts with no classifier at all.
With the default `acceptEdits` mode a command that needs approval becomes a
pending permission instead: `smind task permissions <runId>`, then
`smind task approve|deny <runId>`.

### Choosing a task's starting point

By default a task forks from the workspace checkout's current `HEAD`, so a stale
local `develop` gives a stale base. Pick the start point explicitly instead; at
most one of these:

```sh
smind task new <ws> "title" --base origin/develop   # new branch at a fresh remote ref (recommended for agents)
smind task new <ws> "title" --branch some-branch    # check out an existing branch (e.g. an unmerged PR branch), no new branch
smind task new <ws> "title" --pr 123                # build from a GitHub pull request
```

`--base origin/<branch>` fetches that branch first (best effort), so the main
checkout is never touched. Agents that create tasks should use it.

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

A human who trusts a run can change its settings while it runs, instead of
approving every tool call one by one:

```sh
smind task mode <runId> --auto-accept on     # asks you to type "yes" at a terminal
smind task mode <runId> bypassPermissions    # an auto-approving mode asks the same
smind task mode <runId> acceptEdits          # switch to another mode the provider advertises
smind task mode <runId> --auto-accept off
```

This is human-only: the MCP server has no tool that can turn auto-accept on or
pick an auto-approving mode, and the daemon refuses both for any caller that is
not a human ([ADR-0019](docs/decisions/0019-provider-native-permission-modes.md)
decision 6). The command declares itself as the human surface, and turning
auto-accept on, or picking a mode that approves tool calls itself, needs a person
at a terminal; that is a speed bump, not a proof,
because every client of the daemon holds the same token. Against an agent that
can run shell commands as you, the boundary is the provider's own permission
mode or sandbox, as for `smind task approve`.

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

### Child updates

When an orchestrating agent launches children with `task_send` from its own
task, the daemon wakes the parent chat with one `[child event]` message when
a child's whole subtree is quiet (done, failed or rate-limited), so the
parent never has to poll. Updates are coalesced: children finishing together
arrive as a single message after a short debounce. The child's last message
is included as untrusted data, never as instructions. Pass
`notifyOnFinish: false` to `task_send` for a fire-and-forget child. Config
(`~/.smind/config.yaml`): `orchestration.notifyParent` (default `true`) and
`orchestration.notifyDebounceSeconds` (default `20`; `0` delivers at once).

A tree that goes silent with unfinished work is reported once: when an
orchestrating task's whole subtree has had no run or queued prompt for
`orchestration.stallAlertMinutes` (default `15`; `0` turns it off), the daemon
sends a "Tree stalled" notification (sealed push and desktop/browser) and marks
the task `stalled` in `smind task tree`. It never wakes or archives anything.

## Orchestrating with seats

Agent profiles can carry a seat contract (ADR-0041): role instructions, bundled
skills, whether the run gets the orchestration MCP tools, and which profiles it
may launch. `smind profile seed roles` creates the standard pack (`lead`,
`peer-sonnet`, `reviewer`) and is safe to re-run; it never changes a profile
that already exists. Brief a Lead yourself with
`smind task send <task> <prompt> --profile <id>`; the Lead does the item itself
and launches Peers only for parallel parts or independent verification, and
parents are woken by `[child event]` messages instead of polling. See [Running the roles inside smind](docs/WORKSPACE_PROTOCOL.md#running-the-roles-inside-smind).

## Orchestration skills

sMind bundles six agent skills (`smind`, `smind-leader`, `smind-handoff`,
`smind-advisor`, `smind-committee`, `smind-peer`) that teach an orchestrating agent the
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

### Dependencies in task worktrees

Each task gets a fresh git worktree, which has no `node_modules`. A workspace's
setup script (ADR-0024) runs once, in the background, when the task is created;
[`scripts/task-setup.sh`](scripts/task-setup.sh) is a sample for this repo that
installs `web/` (`bun install --frozen-lockfile`) and `mobile/` (`npm ci`):

```sh
smind workspace setup set <workspaceId> --setup-file scripts/task-setup.sh
smind workspace setup show <workspaceId>
```

The script runs with `sh -c` using the daemon's `PATH`, so `bun` and `npm` must
be on it (a missing tool is a warning, not a failed task). Only newly created
tasks run it. While it runs the task counts as busy, so a `smind task send`
right after `smind task new` is rejected unless it passes `--when-busy queue`
(the prompt is delivered when setup finishes). The web UI shows the setup
status.

See [docs/](docs/) for architecture and design decisions.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branching model, Conventional
Commits convention, and spec-driven development practice this repo follows.

## License

[AGPL-3.0](LICENSE)
