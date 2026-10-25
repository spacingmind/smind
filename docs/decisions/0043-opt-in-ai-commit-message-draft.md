# 0043: Opt-in AI-drafted commit message (narrow amendment of ADR-0006 decision 2)

## Status

Proposed (the Human must accept before release).

## Context

ADR-0006 decision 2 says: "The daemon never generates commit messages via a
model call." Users still want a draft to start from (G17 in the git/GitHub UX
gap list), and the commit bar already has a non-LLM heuristic suggestion
(`lib/commit-suggestion.ts`) that is too weak for real changes.

Designs that avoid the exception exist: ask the task's *agent* to draft one in
the task chat. That puts a run in the task's history, can use tools, bills the
task, and is slow to fill a textarea. It is also not "one click in the commit
bar". We recommend the exception below instead, because it is bounded and
auditable; if the Human prefers no exception, the agent-in-chat route is the
fallback and this ADR is rejected.

## Decision

Amend **only** the sentence above. A daemon-side model call to draft a commit
message is allowed when, and only when, all of these hold:

1. **Opt-in per click.** Nothing drafts automatically (not on stage, not on
   open). There is no setting that turns it on globally.
2. **Human-triggered only.** The RPC `task.draftCommitMessage` is refused for
   run-scoped (agent) callers; there is no MCP tool and no agent-facing path.
3. **Isolated, no-tools run.** It uses `RunPromptIsolated` (no tools, no MCP,
   no resume, empty cwd) as a hidden system run (purpose `commitmsg`), pinned
   to the cheapest Claude model. It creates no chat, no queue item, no
   notification and no entry in a task's run history.
4. **Output only fills a textarea.** The reply is returned to the caller as
   text. The daemon never stages, commits, edits the index or the worktree,
   or caches the text on disk. Committing stays the explicit `task.commit`
   action, with ADR-0006's own trailers.
5. **Bounded input and cost.** The prompt carries the staged diff only (byte
   budget, likely-secret files omitted), the stat summary and recent subjects.
   Single-flight per task, a per-task rate floor, an hourly workspace cap and a
   hard timeout. Usage is metered on the run (`run_usage`) like any other
   system run.

ADR-0006 decisions 1 and 3 and the "daemon does not commit on its own" rule are
unchanged.

## Consequences

- `internal/wsapi` still depends on no model code: it holds an interface and
  the daemon wiring supplies the drafter.
- Cost is visible under the workspace's hidden system task, purpose
  `commitmsg`, and does not count against the learning budget.
- A workspace may set free-text style guidance; it is sent as untrusted
  guidance, never as instructions that outrank the system prompt.
- If the Human rejects this ADR the feature is removed (one RPC, one button).
