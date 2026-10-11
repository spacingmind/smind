---
name: smind-handoff
description: Hand off the current task to another agent on smind with full context. Use when the user says "handoff", "hand off", "hand this to", or wants to pass work to another agent.
user-invocable: true
---

# Handoff

Transfer the task — context, decisions, failed attempts, constraints — to a fresh smind agent. The receiver starts with **zero context**, so the prompt must be a self-contained briefing.

**User's arguments:** $ARGUMENTS

## Prerequisites

Read the **smind** skill. Call `profile_list` and read each profile's `notes` before choosing the receiver. If none fits, use `provider_list` and say you fell back.

## Parse the arguments

1. **Profile**: an explicit profile name first; otherwise the one whose notes fit the work.
2. **Isolation**: always a new task (its own worktree). "In a branch/PR" means pass `branch` or `prNumber` to `task_new`; otherwise `baseRef` of the repo's trunk.
3. **Task description**: whatever else the user said.

## The briefing

```
## Task
[Imperative description.]

## Context
[Why this exists, required background.]

## Relevant files
- `path/to/file` — [what it is and why it matters]

## Current state
[What is done, what works, what does not.]

## What was tried
- [Approach] — [why it failed or was dropped]

## Decisions
- [Decision — rationale]

## Acceptance criteria
- [ ] [Criterion]

## Constraints
- [Must-not / must-preserve]
```

Preserve task semantics. Investigate-only means "DO NOT edit files". A fix means implement it. A refactor is not a rewrite.

## Launch

1. `task_new` with a `[Handoff] <task>` title.
2. `task_send` with the briefing and the chosen `profileId` (or `provider`). Pass your own `fromTaskId`/`fromChatId` if you are a task.
3. Tell the user the task id and that it keeps running without you. Do not wait unless asked; if asked, `task_wait`.
