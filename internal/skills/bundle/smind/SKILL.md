---
name: smind
description: smind reference for orchestrating coding agents through the smind MCP tools: discover workspaces and providers, create tasks (git worktrees), send prompts, wait, review, stop and archive.
---

smind is a local daemon that runs coding agents (Claude Code, Codex, GLM, Kimi) in isolated git worktrees called **tasks**. You control it through the MCP tools of `smind mcp serve` (or the `smind task ...` CLI). The daemon must already be running.

A **task** is a worktree plus a branch. A task has one or more **chats** (conversation threads). Sending a prompt to a chat starts a **run**. Runs are what you wait on and read.

## The loop

1. **Discover.** `workspace_list` for the `workspaceId`. `provider_list` for provider ids and their permission modes. `profile_list` for human-authored launch bundles and their `notes`.
2. **Create a task.** `task_new { workspaceId, title, baseRef: "origin/develop" }` branches the worktree off a fresh ref instead of whatever the checkout has. Use `branch` to continue an existing branch, `prNumber` to review or fix a pull request. Pass `parentTaskId` with your own task id when you are a task yourself (hierarchy depth is limited to 2).
3. **Send.** `task_send { taskId, prompt, provider | profileId }`. It returns `{ runId }` immediately, or `{ queued: true, queueItemId }` when the chat is already busy. It never blocks.
4. **Wait.** `task_wait { runId }` blocks until the run is `done`, `error` or `stopped`, a permission request goes pending, or the timeout passes (default 120 s). A timeout is not a failure: call it again with the same `runId`. With several runs, `task_wait { runIds: [...] }` returns as soon as any one needs you. A pending permission returns immediately every time; once you have told the human about it, pass its id in `ignorePermissionIds` so the next wait blocks until something new happens instead of spinning. Do not poll `task_status` in a loop.
5. **Review.** `task_logs { runId, tail }` for the transcript. Read the change with `task_diff { taskId }` (read-only, capped) or with git in the task's `worktreePath` (`git log origin/develop..HEAD`, `git diff origin/develop...HEAD`). Run the repo's checks yourself. A run saying "done" is a claim, not evidence.
6. **Iterate or finish.** Send follow-ups with `task_send` to the same task. `task_stop { runId }` stops a run. When the work is pushed or merged, `task_archive { taskId }`.

Lost a run id (for example after your context was compacted)? `run_list { taskId }` finds it. `task_list` and `task_get` show tasks, `chat_list` shows a task's chats, `chat_new` opens a fresh conversation in the same worktree.

## Queueing

`task_send` defaults to `whenBusy: "queue"`: the prompt is appended and delivered at the next turn boundary. `"interrupt"` stops the running run and delivers first (use it to redirect, not for polite follow-ups). `"reject"` returns an error if the chat is busy. A long brief can live in a file inside the task's worktree: `promptFile` (a relative path, at most 64 KiB) replaces `prompt` so you don't pay for the text twice. `queue_list { chatId }` shows what is queued and `queue_cancel { chatId, itemId }` removes a prompt you queued yourself (never a human's). Pass `fromTaskId`/`fromChatId` (your own ids) so the delivered prompt carries a `[message from task #T, chat #C]` header and the receiving agent knows who is speaking.

## Permissions are human-only

There is **no tool to approve or deny** a permission request, by design. If `task_wait` returns `pendingPermission`, or `task_permissions { runId }` lists something, stop and tell the human what is being asked (tool, summary) and which run. The human answers in the smind UI or with `smind task approve` / `smind task deny`. Never try to route around this.

You also cannot choose an auto-approving mode yourself: `task_send` rejects `bypassPermissions`, `autoAccept` and similar. If the work needs one, use a profile the human wrote (`profile_list`, then `profileId`).

## Choosing how to launch

Read every profile's `notes` and pick the one that fits the work; pass `profileId`. If none fits, pick a provider from `provider_list` and one of its modes that does not `autoApproves`, and tell the user you fell back. A profile is launch configuration only: do not assume it stays in force later.

Change a running task's settings with `task_set_mode { runId, modeId }` (same rule: no auto-approving modes) and, for ACP providers (GLM, Kimi), `task_options { runId }` then `task_set_option { runId, configId, value }` for thinking level.

## Parallel work

Give each parallel worker its own task (its own worktree). Two chats in one task share a worktree and will step on each other's files. Keep at most two or three workers running; each one is a paid agent session.

## Nested orchestration

A task agent can orchestrate too if it has this MCP server. Add it once as an agent MCP server: `smind mcp add smind stdio --command smind --arg mcp --arg serve`. Then children must be created with `parentTaskId` set to the parent task, so the depth guard applies.

## Rules of thumb

- Briefing prompts are self-contained: files, numbered steps, exact test names, the checks to run, the commit granularity, what not to touch.
- Never merge or push on a worker's say-so. Verify the diff and the checks yourself first.
- Uncommitted work is safe on archive: it is committed to the task branch as a checkpoint commit first. Ignored files (`.env`, `node_modules`) are not kept.
- Archiving a task with a running or queued run is refused: `task_stop` first.
- Report back with what was verified and what was not.

## Tool index

Discovery: `workspace_list`, `provider_list`, `profile_list`, `task_list`, `task_get`, `run_list`, `chat_list`.
Lifecycle: `task_new`, `chat_new`, `task_send`, `task_wait`, `task_stop`, `task_archive`.
Inspection: `task_status`, `task_logs`, `task_permissions`, `task_diff`, `usage_summary`.
Queue: `queue_list`, `queue_cancel`.
Live settings: `task_set_mode`, `task_options`, `task_set_option`.
