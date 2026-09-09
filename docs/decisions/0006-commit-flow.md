# 0006: Commit flow — per-file staging, human-written messages, agent commits are marked

## Status

Accepted

## Decision

Resolves open questions 4, 6, and 7 of `docs/research/dual-mode-ui.md`.

1. **Staging granularity: per-file.** The commit flow exposes
   stage/unstage per file (a file list with checkboxes over the task's
   changed files), not a coarse `git add -A` and not per-hunk. A commit
   stages exactly the selected files. Per-hunk staging stays out of
   scope for the foreseeable future — `task.diff` is deliberately a
   single base→worktree diff, and hunk-level staging would require a
   second diff mode plus staging RPCs that contradict that design.
2. **Commit messages are written by the human** (in the UI) or passed
   explicitly by the agent's caller. The daemon never generates commit
   messages via a model call — `internal/wsapi` gains no dependency on
   `internal/routing`.
3. **Agents may commit** their own task's worktree branch, via the same
   commit primitive the UI uses. Every commit made through smind carries
   a machine-readable marker distinguishing its author kind:
   - human-initiated commits: standard identity, no marker
   - agent-initiated commits: trailer `Smind-Agent: <provider>` (plus
     the task id, e.g. also `Smind-Task: <id>`) so review tooling can
     filter "what the agent committed that I haven't seen".

## Alternatives considered

- **Coarse `git add -A` (Paseo's model, VS Code-like).** Rejected by the
  maintainer: review-then-commit with per-file selection is the point of
  the editor-mode review surface; coarse staging makes selective commits
  impossible.
- **Per-hunk staging.** Rejected as follow-up scope: contradicts
  `task.diff`'s single-diff design (see above); revisit only if daily
  use proves per-file insufficient.
- **Daemon-generated messages** (Aider/VS Code style). Rejected: adds
  the `wsapi → routing` dependency direction and a model call inside a
  git operation; a human (or the prompting context) can write the
  message.
- **Agents never commit.** Rejected by the maintainer: long autonomous
  runs benefit from checkpointing their own work; the worktree is
  already isolated, and the trailer marker keeps review honest.

## Rationale

- Per-file selection over a base→worktree diff maps cleanly onto a
  per-file diff view (the diff viewer already needs per-file grouping);
  the git mechanics are `git add <path>` / `git restore --staged <path>`
  against the snapshot-index machinery `task.diff` already uses.
- The trailer (rather than a separate identity/committer, or a store
  table) travels with the commit into every clone and tool; no schema
  or protocol state to keep in sync.
