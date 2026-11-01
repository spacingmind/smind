---
title: The Steward review seat
description: Ask a read-only agent seat for a second opinion on a finished task's head commit, read its verdict and gates in the inbox, and set its limits.
summary: "This page explains the Steward review in sMind by Spacing Mind: what the review seat reads and cannot do, the objective gates the daemon checks itself, the ACCEPTED, CHANGES and BLOCKED verdicts, the review card in the inbox, the smind steward commands and the limits that keep it cheap. It is for people who run a Lead and want a second look at a finished task before they merge."
order: 25
---

# The Steward review seat

A **Steward review** is a second, read-only judgement of one finished task. The
`steward` seat reads the task's head commit in its own detached scratch
worktree, never in the Lead's, and compares it with a digest of objective gates
that the daemon computes itself. It reports a verdict and findings. It never
merges and cannot change a file: a review is advice for you, and you read it
and merge. smind does not.

It sits next to the [validator seat](validator-and-review.md). The validator
checks a commit against a contract written before the code. The Steward looks at
the finished item as a whole: does the evidence hold up, and is the change
clean enough to merge.

## Turn it on

The review engine is **off by default**. Turn it on by editing `config.yaml`,
then create the seat with the other roles:

```yaml
steward:
  enabled: true
```

```sh
smind profile seed roles
```

The seed creates `lead`, `peer-sonnet`, `validator` and `steward`, skipping any
that exist. The daemon rereads `steward.enabled` on every decision, so no
restart is needed. Nothing under `steward:` can be changed by an agent, a plugin
or a relay session: only a person editing the file.

## Ask for a review

```sh
smind steward review 42                 # the task's head commit
smind steward review 42 --sha 0123456789abcdef0123456789abcdef01234567
smind steward list                      # newest first
smind steward list 42 --limit 5         # one task's reviews
smind steward show 7                    # one review: verdict, summary, gates
smind steward cancel 7                  # stop an open review
```

Each command takes `--json`. Only a person can start a review: a Lead cannot
ask for one, read one as authority or act on one by itself. A review reaches a
Lead only when you send it.

To have finished work reviewed without asking, list workspace titles under
`steward.autoReview`. A finished root Lead run with a pushed branch in one of
those workspaces then gets one review.

## What the seat does and cannot do

- It has the tools Read, Grep and Glob, plus two of smind's own:
  `steward_digest_get` to read the digest and `steward_verdict_submit` to hand
  in its verdict. It cannot run commands, write files, merge, approve or message
  anyone.
- Everything in the digest that an agent or a user wrote (a PR body, a commit
  subject, a handoff note) is marked untrusted and is data, not instructions.
- It looks at every evidence frame (screenshot) the task attached and reports an
  observation for each.

## The gates

The daemon computes these from the repository and CI, not from what anyone says:

| Gate | What it checks |
|---|---|
| `ci` | CI at the head commit: pass, or fail with the failing checks. Still running counts as undecided. |
| `validator` | Only for a task with a [contract](validator-and-review.md): a passing validator verdict at the head commit, with no scope violation. |
| `frames_fresh` | Only when the change touches `web/` or `mobile/`: evidence frames captured at the head commit, at 1280 and 390 pixels wide, light and dark. |
| `no_binaries` | No binary file in the diff. |
| `no_churn` | No `gen/schemas`, `node_modules` or `.DS_Store` files, no `dist`, `build` or `out` output, and no lockfile changed without its `package.json`. |
| `no_secrets` | No secret-looking text in the diff. |
| `not_stacked` | The pull request targets `develop`, carries no commit of another pull request, and its base is reachable from `origin/develop`. |
| `pr_hygiene` | A pull request exists, its title follows Conventional Commits, and its body has a validated section and a not done or not verified section. |
| `scope` | Only for a task with a contract: every changed file is inside the contract's write scope (plan files are always allowed). |
| `mutations_committed` | Only for a task with a contract: the validator's mutation checks ran at the head commit and every file they touched was restored. |
| `peer_claims` | No Peer handoff reports `blocked` or `failed`, and what a Peer claims matches the pull request. |

A gate that does not apply to the task shows as not applicable.

## Verdicts

| Verdict | Meaning |
|---|---|
| **ACCEPTED** | The seat said ACCEPTED and no gate failed. |
| **CHANGES** | The seat has findings (each names a file and line, why, and the check that would show it fixed), or a gate failed. |
| **BLOCKED** | `not_stacked` or `no_secrets` failed, the seat blocked, or the daemon ended the review: `timeout`, `budget`, `crashed`, `cancelled` or `scope_violation`. The reason is shown. |

The daemon clamps the verdict to the gates: a failing gate lowers the cap to
CHANGES (to BLOCKED for `not_stacked` and `no_secrets`), and the seat can lower
a verdict but never raise one. While a gate is still undecided, for example CI
is running, the review is held and publishes nothing. A review goes **stale**
when the head commit moves, when a newer contract version appears, or when a
newer review replaces it. A stale review leaves your inbox. A Steward review has
no merge-ready flag: ACCEPTED is one input to your decision, not the decision.

## In the inbox

A finished review shows up in the [Inbox](inbox.md) as a **review** row with the
verdict as its chip. An open review (no verdict yet) and a stale one do not show.
Open the row for the review card:

- the verdict, with a **Stale** note if it no longer applies;
- the reason, for a BLOCKED review;
- **Gates**: each gate as pass or fail;
- **Findings**, and **Evidence frames** when the task has them;
- **Copy findings**, to paste them into a message to the Lead.

## Limits

The reviews are rationed so a bad run cannot cost much:

| Setting under `steward:` | Default | Ceiling |
|---|---|---|
| `budgetUsd`: cost of one review | 1.5 | `maxBudgetUsd`, itself at most 5 |
| `maxMinutes`: time for one review | 20 | 60 |
| `maxReviewsPerHour`: all tasks together | 6 | 24 |
| `maxReviewsPerTask` | 5 | none |

Only one review per task is open at a time, and a review identical to an earlier
one (same head, same contract, same CI) is not repeated. After three
consecutive reviews that crashed or timed out, automatic reviews pause until you
run one by hand and it completes. `0` or an absent key means the default.

A related switch, `steward.annotate`, is off by default: when on, the seat may
attach an advisory recommendation to a permission request that was escalated to
you. The standing permission rules are a separate feature, described in
[Permission policy](permission-policy.md).

## See also

- [Orchestration](orchestration.md): the seats and how a Lead launches Peers.
- [The validator seat and review rows](validator-and-review.md).
- [Permission policy](permission-policy.md): `steward.policy.enabled`, the floor
  of always-denied commands and the **Permission policy** settings page.
- [Configuration reference](configuration.md): the `steward` keys.
