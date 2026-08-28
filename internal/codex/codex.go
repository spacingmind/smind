package codex

// DefaultCommand returns the command to spawn OpenAI's Codex CLI in
// app-server mode (see https://github.com/openai/codex, checked out
// locally at refs/codex), for use with New.
//
// UNVERIFIED against a real, currently-installed Codex CLI -- flagging this
// plainly rather than asserting it works: refs/codex/codex-rs/cli/src/
// main.rs's AppServerCommand has an *optional* subcommand
// (daemon/proxy/generate-ts/...), which reads as "bare `codex app-server`
// starts listening directly" (the code path this package's rpc.go/client.go
// are built against, and the shape a real production TypeScript client
// drives). But the actual installed binary tested in this session (`codex
// app-server`, v0.149.1) exits immediately with no output instead, and its
// daemon/proxy subcommands require an officially "standalone"-installed
// Codex (this environment's brew install explicitly isn't one -- `codex
// app-server daemon start` fails with "managed standalone Codex install not
// found"). This package's message protocol itself (method names, request/
// notification shapes) was verified field-by-field against
// refs/codex/codex-rs/app-server-protocol's real schema, independent of
// which transport/spawn mode delivers it -- only the spawn command here is
// unconfirmed. Adjust if a real deployment target's `codex` needs a
// different invocation (e.g. requiring `codex app-server daemon start` once
// out of band, then `codex app-server proxy --sock <path>` per turn).
func DefaultCommand() []string {
	return []string{"codex", "app-server"}
}
