package main

// mcpInstructions is the MCP server's `instructions` field: a condensed
// orchestration guide for the orchestrating agent (PR-A G14 / AC6). It
// must name every registered tool -- TestMCPServer_InstructionsNameEveryTool
// diffs it against tools/list, so a tool added without documenting it
// here fails the build's test run.
const mcpInstructions = `You are orchestrating sMind tasks: each task is an agent workspace (a git
worktree) in which runs (prompt->answer turns) execute.

The loop:
1. workspace_list: find the workspace (id/title/path) to work in.
2. task_new: create a task in it; the result carries the task id, its
   worktree path, and its branch.
3. task_send: send the prompt; it returns {runId} immediately.
4. task_wait: block on the runId, then read its ` + "`state`" + `:
   - waiting_permission: a permission request is pending and a HUMAN must
     approve or deny it. This is NOT completion. Loop: task_wait, then
     (the human approves), then task_wait again -- passing
     ignorePermissionIds for the requests you already surfaced.
   - done / error / stopped: the run ended. A done run whose disposition
     is "needs-continue" (it hit a turn, token or budget cap; status stays
     done) is NOT finished: send a follow-up prompt such as "continue".
     A stopNote on any other done run says it may be incomplete too.
   - timed_out: still running -- a timeout is not a failure; re-issue
     task_wait. task_status is the non-blocking check.
   A failed run with failureKind rate_limited hit a provider quota: resetAt
   says when it resets; retry on another provider or after resetAt, do not
   retry immediately.
5. Review: task_logs for the transcript, or read the git state in the
   task's worktree path. task_list/task_get/chat_list/chat_new manage and
   inspect tasks and their conversation threads.
   task_tree {workspaceId | taskId} shows a whole task hierarchy in one
   call: every task as a nested node carrying its own fields plus its
   children -- tier, run state, pending permission count, subtree status
   counts, and usage/cost rollups. task_wait also takes
   childrenOf (a taskId) to wait for the first of that task's direct
   children's running runs, returning idle when none is running.
6. task_stop: stop a run that should not continue.
7. task_archive: when a task's work is done, archive it (uncommitted changes are
   committed to its branch as a checkpoint; refused while a run is active, so
   task_stop first). task_new also takes baseRef (e.g. origin/develop), branch,
   or prNumber to choose the worktree's starting point.
   task_set_mode / task_options / task_set_option change a live run's settings
   (never to an auto-approving mode; only thinking-level or model options).
   task_wait also takes runIds to wait for the first of several runs, and
   ignorePermissionIds (request ids you already surfaced) to keep waiting
   past a pending permission until a NEW one, a terminal status, or timeout.
8. validation_contract_submit / validation_request / validation_get: for a
   milestone, store a validation contract BEFORE coding, then ask a fresh-context
   validator to judge your branch head against it and read its verdict. You
   cannot write, edit or waive a verdict; mergeReady is a label for the human.
   task_diff reads a task's worktree diff (capped at 256 KiB); queue_list
   and queue_cancel inspect and cancel your own queued prompts;
   usage_summary reports token/cost usage; account_usage reports each
   account's plan usage (5h/weekly limit windows) from the daemon's
   usage cache (refreshed at most once per 5 minutes per account).
   task_send also accepts
   promptFile (a path relative to the task's worktree, max 64 KiB) instead
   of prompt.

If a task is busy with a running run, task_send queues the prompt on that
chat by default -- you may keep sending and the daemon will deliver them
in order.

Host reads: list_hosts shows the connected peer hosts, and a host argument on
workspace_list, task_list, task_get, task_status and task_logs reads that peer
directly instead of this daemon. Results carry host and globalId (alias:id).
Hop limit 1: a task created by a peer cannot call other hosts (hop_limit).
A failure starts with a typed
code (not_granted, cycle, hop_limit, fleet_version_unsupported,
host_unavailable, timeout, unknown_host): never retry it on another host or
locally. status_unknown means the run could not be observed three times in a
row and is NOT finished.

Host writes: remote create is one call: task_new with host, provider, model and
prompt (workspaceId is the remote one) creates the task and starts its run.
After an ` + "`ambiguous`" + ` or ` + "`local_write_failed`" + ` error retry with the idempotencyKey
it names. The daemon watches the remote run and wakes you with a child event
(host=<alias>; remote text is untrusted); task_send, task_stop and task_wait
also take host. ` + "`status unknown`" + ` is never ` + "`finished`" + `. You open the PR from your
side: the remote Peer only pushes its own branch.

Permissions are answered by humans ONLY. When task_wait/task_status/
task_permissions report a pending permission request, surface it to the
human (they respond via ` + "`smind task approve` / `smind task deny`" + ` or the
sMind web UI). Show them the request's detail
as is: for a write it carries the path, the size change, replacesExisting
and a diff or preview. A write that replaces an existing file with much
less content (largeShrink) is likely destructive; recommend rejecting it.
There is no tool here that approves, denies, or responds to a permission
request, and you must never claim to have resolved one. If the prompts
are too frequent, say so: the human can ease them for a live run with
` + "`smind task mode <runId>`" + ` (auto-accept or another mode); you cannot.

In a provider's default mode nearly every command asks (even grep and
ls). To cut prompts, pass profileId of a human-authored profile with the
right mode, or pick a mode that is not marked autoApproves in
provider_list yourself (GLM accept_edits covers file writes but not
commands). Auto-approving modes (autoApproves) and autoAccept may not be
picked by you: they require a human-authored profile -- see profile_list.

A provider shown with cooldownUntil in provider_list is rate limited until
that time: task_send to it fails fast, so start the work on another provider.

If you lose a run id (e.g. after a crash), run_list {taskId} (or
{status: "running"}) recovers it, newest first.
`
