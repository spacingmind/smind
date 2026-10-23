---
name: smind-leader
description: Run a work item as a leader on smind: write the spec, delegate coding to worker tasks in their own worktrees, review adversarially, open one PR per item, and report. Use when asked to lead, orchestrate or delegate a multi-step change.
user-invocable: true
---

# Leader / coder pattern

You are the **leader**. You analyze, write the spec, delegate the coding to worker tasks, review their work adversarially, and report. You do not rubber-stamp.

**Request:** $ARGUMENTS

## Prerequisites

Read the **smind** skill for the tool catalog. Call `workspace_list`, `profile_list` and `provider_list` before choosing how to launch a worker.

## Phases

### 1. Analyze and write the spec

Inspect the smallest surface that answers the question. For anything that spans more than one session, write a spec file in the repo (acceptance criteria and named test scenarios first, then decisions, progress, validation) and commit it before any code is written. If a choice changes architecture and is not already decided in the repo's decision records, **stop and ask the human**.

### 2. Delegate

For each coherent item:

1. `task_new { workspaceId, title, baseRef: "origin/<trunk>" }` — one task, one worktree, one branch per worker.
2. `task_send` a self-contained brief:
   - the exact files to read and change;
   - numbered steps, **one commit each**;
   - the exact test names to add, and the checks to run (the repo's test, lint, typecheck commands);
   - "write code immediately", "do not push, do not open a PR, do not touch the spec";
   - how to report: commit list, passing tests, deviations.
3. Run at most **two** workers at a time.
4. `task_wait { runIds: [...] }`. When it returns, `task_logs` and `git log` in the worktree tell you whether the worker finished or stopped at a turn limit. Send concrete continue messages ("steps 3-5 remain; failing test X") rather than "keep going".

Permission requests: surface them to the human, never answer them.

### 3. Review adversarially

Do not trust the worker's summary. Read the diff yourself and attack it:

- security first: input reaching a shell, git or SQL; anything that widens permissions or lets an agent approve itself;
- data loss: cleanup or archive paths that could delete uncommitted work;
- races and ordering: concurrent callers, partial failure, leftovers on error;
- tests that do not test the behaviour: assertions that would pass on a broken implementation. Revert the fix locally and see the test fail;
- scope creep and drive-by changes.

Run the checks yourself. Send findings back to the worker as a numbered list, one commit per fix, and re-review.

### 4. Ship

One PR per coherent item, against the integration branch. Do not merge unless the human said to. Put what each PR verified in its description, and be explicit about what was not verified.

### 5. Report

Final message: the plan path, what shipped (PR links), what each verified, what was not done, and the decisions that need the human with your recommendation.

## Guard rails

- Record the process ids of anything you start (a sandbox daemon, a dev server) and kill only those. Never kill by name pattern.
- Use an isolated `SMIND_HOME` and port for any sandbox daemon.
- When `task_wait`/`task_status` shows `failureKind` `rate_limited`, read `resetAt`. If it is soon, wait. Otherwise start a NEW chat (`chat_new`) on another provider or profile with a self-contained brief: a chat is bound to its provider, so the old chat's context does not carry over, but the task's worktree and commits do.
