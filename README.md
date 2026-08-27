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
