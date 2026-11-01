---
title: The validator seat and review rows
description: Have a fresh-context agent check a commit against a contract, and where its verdict reaches you.
summary: "This page explains the validator seat in sMind by Spacing Mind: the contract a Lead writes before coding, what a verdict contains and when it goes stale, what merge-ready means, and the verdict rows that show up in the inbox. It is for people who run a Lead and want an independent check before they merge."
order: 21
---

# The validator seat and review rows

The validator is an agent seat that judges **one commit** against a contract
written **before** the code, in a fresh context. It reports pass, fail or
blocked, with evidence. It never merges, and neither does smind: a passing
verdict is a label that helps you decide.

Create the seat with the other roles (the daemon must be running):

```sh
smind profile seed roles
```

It creates `lead`, `peer-sonnet`, `validator` and `steward`, skipping any that
exist. See [Orchestration](orchestration.md) for the seats and
[The Steward review seat](steward-review.md) for the fourth.

## The contract

A Lead writes a contract as a Markdown file with YAML front matter, then
stores it on the task:

```sh
smind validation contract submit <taskId> <file>
smind validation contract show <taskId>
```

Each submit stores a new version; the highest version is the current one. The
front matter lists:

- `acs`: the acceptance criteria. Each one must be covered by at least one test,
  mutation or frame, or the contract is refused with the field and reason.
- `tests`: named tests with the command to run.
- `mutations`: one change to a file, and the named tests that must then fail.
- `frames`: screens the validator must look at (for work with a UI).
- `commands`: build, lint or full-suite commands whose exit status is recorded.
- `limits`: a budget and a time limit for the validation.

Unknown keys are an error, and the commands in a contract are text the
validator runs under its own permission mode: a contract cannot widen what the
validator may do. Contract size limits are checked when you submit.

## What the validator sees

A Lead starts a validation with the `validation_request` tool. The daemon
creates a new task and chat with no history, in a scratch worktree at the exact
commit, so the validator never has the Lead's branch checked out. Its first
prompt is built by the daemon from the stored contract, the diff and fixed
instructions; the Lead adds no text. The validator does not see the Lead's
chat, the contract's prose body or the pull request description. It does see
the repository, so a change the contract does not mention becomes a finding.

## The verdict

A verdict is one of:

- **pass**: every test, mutation, command, frame and criterion in the contract
  has an item with evidence. The daemon rejects a `pass` with a missing item.
- **fail**: at least one item failed, with a finding or excerpt saying why.
- **blocked**: the validator could not finish. It reports one of `no_browser`,
  `no_toolchain`, `flaky_unresolved` or `contract_unrunnable`, and says in the
  summary what it lacks. Only the daemon writes the other reasons: `timeout`,
  `budget` or `crashed` when the run ends without a verdict, and
  `scope_violation` when the validator changed the Lead's worktree.

Only the validator's own run can write a verdict, with a one-time token the
daemon gives that run. A verdict names the commit and the contract version it
judged. It is **current** only while the branch head is that commit and the
contract is the task's newest; a push makes it **stale**.

**Merge-ready** is worked out each time you read, never stored: the latest
verdict is a pass, it is current, it has no scope violation, and (when a pull
request is known) the pull request head is that commit. It means "the contract
is satisfied at this commit", and it is for your decision.

Read validations from the command line:

```sh
smind validation list <taskId>
smind validation show <id>
```

Each line shows the validation, task, status, short commit, contract version
and `merge-ready` or `not merge-ready` with the reason. `show` adds the
summary, a blocked reason and the evidence folder. Add `--json` for the raw
result. Evidence files are copied under your smind home so they survive
archiving the task.

## Review rows in the inbox

The newest verdict of a task appears in the [Inbox](inbox.md) as a **verdict**
row, under **Validator verdicts**, when it needs you:

| Row text | Meaning |
|---|---|
| Validation failed | the newest current verdict is a fail |
| Validation blocked: `<reason>` | it is blocked, with the reason |
| The validator changed the lead's worktree | a scope violation |
| Validation passed at the current commit: ready for your merge decision | a pass that is merge-ready |

A pass that is not merge-ready is not listed, and neither is a stale verdict or
a validation still running. A push, or a newer validation, removes the row on
its own. You can mark it seen, snooze it, resolve it or open the task.

If the validator needs a permission, for example to run something outside its
scratch worktree, that is an ordinary **permission** row.

## Related

- [Orchestration](orchestration.md): seats, Leads, Peers and the frame harness.
- [Inbox](inbox.md): every kind of row and what you can do with it.
