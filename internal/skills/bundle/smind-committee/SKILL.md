---
name: smind-committee
description: Form a committee of two high-reasoning smind agents from contrasting providers to do root-cause analysis and produce a plan. Use when stuck, looping, or facing a hard planning problem.
user-invocable: true
---

# Committee

Two agents from contrasting profiles, fresh context, analyzing the same problem in parallel.

**User's additional context:** $ARGUMENTS

## Prerequisites

Read the **smind** skill. Call `profile_list` and `provider_list` before choosing members.

## Composition

Two members with different reasoning styles, ideally from different provider families:

- one whose profile notes fit planning, research or root-cause analysis;
- one contrasting high-reasoning profile from another provider.

If the user names profiles, use those. If fewer than two fit, fall back to `provider_list` for the missing member and tell the user.

## Hard rules

- **No edits.** Choose a read-only or `plan` permission mode that does not auto-approve, and end every prompt with:

  ```
  This is analysis only. Do NOT edit, create, or delete any files. Do NOT write code.
  ```

- **Do not hurry them.** Reasoning can take a long time. `task_wait` with a long timeout and re-issue it; do not poll or interrupt.

## Workflow

1. Write one problem-level prompt (the problem, what is known, what is ruled out, relevant paths).
2. `task_new` twice (`[Committee] <topic>`, one task each) and `task_send` the same prompt to both.
3. `task_wait { runIds: [a, b] }` until both are done.
4. Read both answers with `task_logs`. Where they disagree, send each one the other's argument with `task_send` and ask for a response.
5. Repeat until they converge.
6. Report the consensus to the user, where the members diverged and how it resolved. `task_archive` both tasks.
