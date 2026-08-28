# sMind

[![CI](https://github.com/spacingmind/smind/actions/workflows/ci.yml/badge.svg)](https://github.com/spacingmind/smind/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-blue.svg)](LICENSE)

Space for your agents, free for you.

sMind is a self-hosted platform for running coding agents, built around
per-workspace account routing so you can spread agent traffic across your own
provider accounts instead of a single shared pool. It ships as a single Go
binary that serves both the API and an embedded web UI — workspace/task
management, a live agent timeline, a file explorer with an editor, a diff
viewer, and a real terminal, all talking to the daemon over one WebSocket
connection.

## Install

```sh
go install github.com/spacingmind/smind/cmd/smind@latest
```

## Quickstart

```sh
smind serve                                        # start the daemon (http://localhost:4648)
smind workspace create /path/to/repo "my project" hard
smind task new <workspaceId> "fix the failing test"
smind task send <taskId> glm "fix the failing test"  # or open the web UI and use the Chat tab
```

## Dev quickstart

```sh
task build      # build the web UI, then the smind binary (bin/smind)
task dev:web    # run the Vite dev server, proxying /healthz to :4648
task dev:go     # run the Go daemon
task test       # go test ./... (plus the web UI's test suite)
task lint       # go vet + gofmt check
```

See [docs/](docs/) for architecture and design decisions.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branching model, Conventional
Commits convention, and spec-driven development practice this repo follows.

## License

[AGPL-3.0](LICENSE)
