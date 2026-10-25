# sMind

[![CI](https://github.com/spacingmind/smind/actions/workflows/ci.yml/badge.svg)](https://github.com/spacingmind/smind/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/spacingmind/smind)](https://goreportcard.com/report/github.com/spacingmind/smind)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-blue.svg)](LICENSE)

**Space for your agents, free for you.**

sMind is a self-hosted platform for running coding agents, built around
per-workspace account routing so you can spread agent traffic across your own
provider accounts instead of a single shared pool. It ships as a single Go
binary that serves both the API and an embedded web UI. Every task runs in its
own git worktree, and a desktop app, a mobile app and a CLI work with the same
daemon.

> **Status: early / pre-1.0.** The core daemon, CLI, and web UI are usable
> day to day, but expect rough edges and breaking changes before `v1`. See
> [docs/ROADMAP.md](docs/ROADMAP.md) for what's done and what's next.

## Install

On Linux or macOS:

```sh
curl -fsSL https://spacingmind.com/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/spacingmind/smind/master/scripts/install.ps1 | iex
```

Then start the daemon and open <http://localhost:4648>:

```sh
smind serve
```

The desktop apps (macOS, Windows), pinning a version and the other install
routes are in [Getting started](public-docs/getting-started.md). Docker and
running the daemon as a service are in [Run the daemon](public-docs/running-the-daemon.md).

## Documentation

User documentation lives in [public-docs/](public-docs/index.md):

- [Getting started](public-docs/getting-started.md)
- [Providers](public-docs/providers.md)
- [Workspaces and tasks](public-docs/workspaces-and-tasks.md)
- [Git and pull requests](public-docs/git-and-prs.md)
- [Mobile and relay pairing](public-docs/mobile-and-relay.md)
- [Orchestration](public-docs/orchestration.md)
- [Scripting and permissions](public-docs/scripting-and-permissions.md)
- [Run the daemon](public-docs/running-the-daemon.md) (service, remote access, Docker, updates)
- [CLI reference](public-docs/cli-reference.md)
- [Configuration](public-docs/configuration.md)
- [Troubleshooting](public-docs/troubleshooting.md)

Architecture, decision records and plans for contributors are in
[docs/](docs/).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branching model, Conventional
Commits convention, spec-driven development practice, and how to build and
test from source.

## License

[AGPL-3.0](LICENSE)
