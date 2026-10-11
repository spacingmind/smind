package main

// mcpInstructions is the MCP server's `instructions` field: a condensed
// orchestration guide for the orchestrating agent (PR-A G14 / AC6). It
// must name every registered tool -- TestMCPServer_InstructionsNameEveryTool
// diffs it against tools/list, so a tool added without documenting it
// here fails the build's test run.
const mcpInstructions = `You are orchestrating smind tasks: each task is an agent workspace (a git
worktree) in which runs (prompt->answer turns) execute.

The loop:
1. workspace_list: find the workspace (id/title/path) to work in.
2. task_new: create a task in it; the result carries the task id, its
   worktree path, and its branch.
3. task_send: send the prompt; it returns {runId} immediately.
4. task_wait: block on the runId until the run finishes, a permission
   request goes pending, or it times out (a timeout is not a failure --
   re-issue task_wait; use task_status for a non-blocking check).
5. Review: task_logs for the transcript, or read the git state in the
   task's worktree path. task_list/task_get/chat_list/chat_new manage and
   inspect tasks and their conversation threads.
6. task_stop: stop a run that should not continue.

If a task is busy with a running run, task_send queues the prompt on that
chat by default -- you may keep sending and the daemon will deliver them
in order.

Permissions are answered by humans ONLY. When task_wait/task_status/
task_permissions report a pending permission request, surface it to the
human (they respond via ` + "`smind task approve`" + ` or the smind web UI).
There is no tool here that approves, denies, or responds to a permission
request, and you must never claim to have resolved one.

Auto-approving permission modes (marked autoApproves in provider_list)
and autoAccept may not be picked by you: they require a human-authored
profile -- see profile_list and pass its id as task_send's profileId.

If you lose a run id (e.g. after a crash), run_list {taskId} (or
{status: "running"}) recovers it, newest first.
`
