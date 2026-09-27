# 0019: Provider-native permission modes replace smind's ApprovalPolicy

## Status

Proposed (2026-09-28). The direction ("handle it like Paseo, don't build a
custom mode") was decided by the user on 2026-09-28. The design details
below, and the Open questions in
[`docs/plans/active/provider-native-permission-modes.md`](../plans/active/provider-native-permission-modes.md),
still need the user's sign-off before this moves to Accepted.

## Context

smind currently has its own three-tier `taskrunner.ApprovalPolicy`
(`internal/taskrunner/policy.go`): `manual`, `auto-safe`, and `full-access`.
`auto-safe` is smind's own invention. It is a hand-written shell-command
allowlist (`AllowlistedCommand` with `safeCommandPrefixes`), mirrored into
Claude Code `--allowedTools` rules (`SafeBashRules`). For ACP there is also
an in-worktree file-edit fast path (`autoAllowACPFileEdit`).

Three Opus review rounds on `fix/auto-safe-read-only-commands`
(2aff06e..fca5ae8) kept finding bypasses: redirection, quoting, brace/glob
expansion, `go test -exec`, `-coverprofile`, `gofmt -w`, `task --taskfile`,
and ugrep `--filter`. Deciding safety by matching or parsing command strings
can't be made airtight. Paseo, smind's UX reference, has no command
allowlist at all. It exposes each provider's own permission modes:

- **Claude Code**: `plan` / `default` / `acceptEdits` / `auto` (Claude
  Code's own model classifier) / `bypassPermissions`
  (`refs/paseo/.../providers/claude/agent.ts:327-353`). Modes switch
  mid-session via `setPermissionMode` (`:2415-2437`). Paseo always launches
  with `allowDangerouslySkipPermissions: true` so a later switch to bypass
  works (`:3297-3300`). `auto` is rejected on Bedrock/Vertex (`:941-952`).
- **ACP (GLM, Kimi, ...)**: modes come from the agent's own `session/new`
  `modes.availableModes`, falling back to a `category: "mode"` select config
  option (`deriveModesFromACP`, `acp-agent.ts:728-760`). Paseo switches them
  with `session/set_mode` (`:2002-2070`). On top of that, Paseo has one
  `auto_accept` toggle that approves every ACP permission prompt (`:129`,
  `:821-835`). It is auto-enabled for unattended child agents (`:837-852`).
- **Codex**: `auto` (on-request + workspace-write), `auto-review`
  (auto-reviewer subagent, version-gated), and `full-access` (never +
  danger-full-access). Paseo sends these as `approvalPolicy`/`sandbox`
  presets (`codex-app-server-agent.ts:226-300`).
- The only preapproval Paseo does is for MCP tools
  (`toolPolicy.preapproved` → `mcp__server__tool` in `allowedTools`,
  `providers/claude/options.ts:93-103`).

## Decision

1. **Delete `ApprovalPolicy` and every smind-invented auto-approval.** This
   removes `AllowlistedCommand`, `safeCommandPrefixes`, `SafeBashRules`,
   `autoAllowACPFileEdit`/`fileEditPathFromTitle`, `LivePolicyDecider`,
   runPermissionDecider's auto-safe branch, and the `command` argument to
   `PermissionDecider.Decide` (its only consumer was the allowlist). smind
   never again decides by itself that a tool call is safe.

2. **Replace it with one provider-scoped `permissionMode` string.** Its
   value is one of that provider's own mode ids, and the provider owns what
   each mode means:
   - `claude-native`: `default`, `acceptEdits`, `plan`, `auto`,
     `bypassPermissions`. These are passed through as `--permission-mode`
     (`claudecode.WithPermissionMode`). `auto` is only offered when Claude
     Code talks to the Anthropic API directly (not Bedrock/Vertex), the same
     rule Paseo uses.
   - `codex-native`: `auto` and `full-access`, sent as Codex's own
     `approvalPolicy` + `sandbox` on `thread/start`, using Paseo's presets.
     Today smind sends only `cwd`. `auto-review` is deferred (see Open
     questions).
   - ACP providers (`glm`, `kimi`): whatever the agent advertises in
     `session/new` (`modes.availableModes`, or a `category: "mode"` config
     option), applied with `session/set_mode` (or
     `session/set_config_option`). Separately, there is Paseo's per-run
     **`autoAccept`** boolean (approve every ACP `request_permission`,
     implemented by the existing `acp.AutoApprovePolicy`). It is the only
     unattended mechanism for an ACP agent that advertises no bypass mode.
     Unlike Paseo, smind does not auto-enable it for unattended/child runs
     (see point 6).

3. **Mode catalogs are served by the daemon, not hard-coded in clients.**
   `taskrunner.ProviderInfo` gains `Modes []ModeInfo{ID, Label,
   Description}` and `DefaultMode`. These are static for Claude and Codex.
   For ACP they come from a lazily cached probe `session/new` (Paseo's
   `listModes` probe pattern), with a static fallback. `provider.list`
   carries the catalog. The web composer, agent-profile form, and CLI render
   it verbatim, with provider-native labels.

4. **Surfaces are renamed rather than aliased.** `approvalPolicy` becomes
   `permissionMode` (plus `autoAccept` for ACP) on `task.prompt`,
   `run.start`, `profile.create`/`update`, and the run payloads.
   `run.setApprovalPolicy` becomes `run.setPermissionMode`. The CLI's
   `--approval-policy` becomes `--mode` on `task send` and `profile add`. A
   request that still sends the old field fails with an error that names
   the replacement. It is not silently ignored, matching `IsValid`'s
   existing no-silent-default stance.

5. **What survives:**
   - The human approval flow: `runPermissionDecider`, the pending map,
     `run.respondPermission`, the 5-minute timeout-deny, provider-cancellation
     recording, and CLI `task permissions`/`task approve`. Whatever a
     provider's mode still escalates reaches a human exactly as today.
   - Mid-run switching, which now uses the provider's own mechanism.
     Claude: `SetPermissionMode` control request, and spawn with
     `--allow-dangerously-skip-permissions` so bypass is reachable. ACP:
     `session/set_mode`, plus flipping `autoAccept` live on the decider.
     Codex has no mid-turn switch (mode applies at `thread/start`), so
     `run.setPermissionMode` rejects it with a clear error.
   - Historical `permission_resolved` events tagged `auto_safe` stay
     readable. The constant is kept, marked legacy, and the timeline keeps
     rendering it.

6. **ADR-0017's "the orchestrator must not approve" still holds.** No
   approve/deny MCP tools exist, and smind adds no auto-approval of its own.
   Choosing a mode is different from answering a prompt, but an
   auto-approving mode chosen by an agent would amount to self-approval.
   So `smind mcp serve`'s `task_send` (PR #223 / ADR-0017) may only pass a
   `permissionMode` from each provider's *asks-a-human* subset. For each
   ModeInfo, `AutoApproves bool` marks `bypassPermissions`, `auto`, Codex
   `full-access`, ACP bypass-style modes, and `autoAccept`. A human-authored
   agent profile (ADR-0014) selected by id may carry any mode. The profile
   is the human's standing decision, not the orchestrator's.

7. **Migration (fails toward asking a human):**

   | stored `approval_policy` | claude-native | codex-native | glm / kimi |
   |---|---|---|---|
   | `manual` / `''` | `acceptEdits` (today's actual manual behavior, `runner.go:521-531`) | `auto` | agent default mode, `autoAccept=false` |
   | `auto-safe` | `acceptEdits` | `auto` | `accept_edits` if advertised, else default; `autoAccept=false` |
   | `full-access` | `bypassPermissions` | `full-access` | `bypass_permissions` if advertised, else default; `autoAccept=true` |

   `runs` and `agent_profiles` get a `permission_mode` column, backfilled
   by this table in a `migrate.go` step. The old `approval_policy` column
   stays but is no longer read or written, since SQLite column drops aren't
   worth the risk. Browser-stored composer state (`smind:run-config:*`)
   with a legacy `approvalPolicy` is discarded to the provider's default
   mode instead of being mapped. That can only narrow access, never widen
   it.

## Alternatives considered

- **Keep hardening auto-safe's matcher (or parse with a real shell
  parser).** Rejected. Three review rounds produced a new bypass each time.
  The set of dangerous flags per tool (`-exec`, `-w`, `--taskfile`,
  `--filter`...) is unbounded, and the providers already ship real safety
  mechanisms (Claude's classifier, Codex's sandbox).
- **Keep smind's three generic tiers and map each onto provider modes.**
  Rejected. The user asked for provider-native modes, and generic tiers
  hide real differences. Claude has `plan`/`auto`, Codex has a sandbox, and
  ACP modes are agent-defined. The "full-access" per-provider label
  workaround in `web/packages/ui/src/lib/approval-policies.ts` already
  showed the strain.
- **Copy Paseo's auto-enable of `autoAccept` for unattended children.**
  Rejected. It conflicts with ADR-0017's rule that a human approves
  sub-agent actions.
- **Keep `approvalPolicy` as a deprecated alias on the wire.** Rejected.
  The only clients are smind's own bundled web UI, desktop, and CLI, which
  ship together, and a silently remapped alias is exactly the kind of
  quiet permission-behavior change this ADR is trying to remove.

## Rationale

Tool-call safety belongs to the layer that actually runs the tool. Claude
Code has a model classifier and managed rule syntax, Codex has an OS
sandbox, and ACP agents define their own modes. smind's value is surfacing
those choices consistently and routing whatever still escalates to a human,
not second-guessing shell strings. This also removes a large amount of
smind-specific code and doc surface (policy.go, the ACP edit fast path, and
live-policy plumbing) and brings smind to parity with Paseo, which the user
is moving off of.
