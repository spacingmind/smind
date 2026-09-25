// Package version exposes the daemon's build-time version signal, so the
// HTTP API (GET /healthz) and the CLI (smind --version) can report the same
// stamped value. It is deliberately tiny and dependency-free, so the future
// desktop app and the relay binary can share it.
package version

// Version is stamped at build time via
//
//	-ldflags "-X github.com/spacingmind/smind/internal/version.Version=<v>"
//
// and stays "dev" under a plain go build / go test. See the build task in
// Taskfile.yml for how <v> is computed from .release-please-manifest.json.
var Version = "dev"

// Commit is the optional short commit hash, stamped the same way as
// Version. Empty when not provided.
var Commit = ""
