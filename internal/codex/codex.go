package codex

// DefaultCommand returns the command to spawn OpenAI's Codex CLI in
// app-server mode (see https://github.com/openai/codex, checked out
// locally at refs/codex), for use with New.
//
// Verified live 2026-09-11 against codex-cli 0.149.1 (brew install):
// bare `codex app-server` reads newline-delimited JSON-RPC directly from
// its own stdin/stdout. The earlier finding in provider-codex's plan —
// that 0.149.1 exits immediately and requires the daemon + Unix-socket
// proxy path — did not reproduce; initialize/thread/start/turn all
// work over the bare spawn. (The daemon subcommand does still exist and
// requires a "standalone" install; the proxy path remains an option for
// deployments that prefer it.)
func DefaultCommand() []string {
	return []string{"codex", "app-server"}
}
