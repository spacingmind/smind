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

## Install

```sh
go install github.com/spacingmind/smind/cmd/smind@latest
```

### Installing the daemon from a release

Each [GitHub Release](https://github.com/spacingmind/smind/releases) ships
prebuilt `smind` daemon binaries for linux/amd64, linux/arm64, darwin/amd64
and darwin/arm64, plus a `checksums.txt`. To install one:

```sh
version=0.7.0   # match the release you're installing
os=linux        # or darwin
arch=amd64      # or arm64

curl -fsSLO "https://github.com/spacingmind/smind/releases/download/v${version}/smind_${version}_${os}_${arch}.tar.gz"
curl -fsSLO "https://github.com/spacingmind/smind/releases/download/v${version}/checksums.txt"
sha256sum --ignore-missing -c checksums.txt
tar -xzf "smind_${version}_${os}_${arch}.tar.gz"
./smind --version
```

There is no native Windows daemon binary yet (blocked on `internal/terminal`,
see ADR-0013); Windows users run the desktop app, which manages the daemon
via WSL2.

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
