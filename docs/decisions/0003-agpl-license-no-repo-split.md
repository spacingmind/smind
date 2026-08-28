# 0003: AGPL-3.0 license, no repo split (yet)

## Status

Accepted

## Decision

This repo (`spacingmind/smind` — the daemon, CLI, and embedded web UI) is
licensed **AGPL-3.0**. `internal/accounts`/`internal/routing` (the
multi-account routing engine) stay in this repo rather than being split
into a separate `smind-routing` repo right now.

## Alternatives considered

- **MIT or Apache 2.0 for the whole repo.** Simpler, no copyleft friction
  for users/contributors. Passed over in favor of AGPL-3.0's stronger
  guarantee that improvements to a network-deployed fork stay open —
  the standard rationale self-hosted server software reaches for (e.g.
  Nextcloud, Mastodon) when the deployment model is "someone runs this as
  a service," not just "someone links this into their own program."
- **Splitting `internal/accounts`/`internal/routing` into a
  `smind-routing` repo now.** Considered because
  `claude-agent-sdk-go` (this project's other public repo) was split out
  successfully. Passed over: that split had a concrete, specific reason
  (no official Go SDK for Claude Code existed yet — a real external gap
  to fill). The routing engine has no comparable external justification
  today — it's tightly coupled to `internal/store`'s schema and still
  evolving (`docs/ROADMAP.md`'s Phase 1 notes it's "functionally
  complete but not yet exercised with a real provider account"), and no
  second consumer exists to justify a second repo/CI/release pipeline —
  see `docs/ROADMAP.md`'s guiding principle 2, "no feature without real
  demand."

## Rationale

Splitting license strategy by what a piece of code *is for* — permissive
for anything meant for broad, frictionless reuse (protocol clients,
SDKs), copyleft for the deployable product itself — is common practice
among comparable self-hosted tools. `claude-agent-sdk-go` already follows
the permissive half of that pattern (MIT, already split out, already
public, since it's a clean protocol-client library with real value to
anyone building against Claude Code's headless protocol, not just
smind). This repo is the deployable product half, so AGPL-3.0 fits.

A future `smind-relay` (Phase 3, not yet built) is expected to land on
the permissive side of that same split when it exists — a relay's value
comes from other implementations being able to interoperate with it
freely, which copyleft would work against rather than protect. Noted
here as the expected default for that future repo, not decided as a live
thing today.
