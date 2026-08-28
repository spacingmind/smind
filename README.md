# Spacing Mind (smind)

Space for your agents, free for you.

Spacing Mind is a self-hosted platform for running coding agents, built around
per-workspace account routing so you can spread agent traffic across your own
provider accounts instead of a single shared pool. It ships as a single Go
daemon that serves both the API and an embedded web UI.

## Dev quickstart

```sh
task build      # build the web UI, then the smind binary (bin/smind)
task dev:web    # run the Vite dev server, proxying /healthz to :4648
task dev:go     # run the Go daemon
task test        # go test ./...
task lint        # go vet + gofmt check
```

See [docs/](docs/) for architecture and design decisions.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branching model, Conventional
Commits convention, and spec-driven development practice this repo follows.

## License

[AGPL-3.0](LICENSE) — see
[docs/decisions/0003-agpl-license-no-repo-split.md](docs/decisions/0003-agpl-license-no-repo-split.md)
for the rationale (permissive licenses are reserved for
protocol/infrastructure pieces meant for broad reuse, e.g.
[claude-agent-sdk-go](https://github.com/spacingmind/claude-agent-sdk-go)
(MIT); this repo — the daemon, CLI, and web UI — is AGPL-3.0).
