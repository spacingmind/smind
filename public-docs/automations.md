---
title: Automations
description: Start a task by itself on a schedule, or when a GitHub workflow fails, with caps, de-duplication, auto-pause and a record of every decision.
summary: "This page explains automations in sMind by Spacing Mind: tasks that start by themselves on a schedule or when a GitHub workflow fails, with caps, de-duplication, auto-pause and a decision record. It is for people who want unattended agent runs under strict limits."
order: 16
---

# Automations

An automation starts a task for you when something happens: a time on a
schedule, or a GitHub workflow run that failed. It is off until you turn it on
twice (once for the whole daemon, once for the automation), and only you can do
that. No agent, and no task an automation started, can create, change, enable
or fire an automation.

## Turn them on

Automations are off until you set this in `config.yaml`:

```yaml
automations:
  enabled: true        # the engine runs; with false nothing polls and nothing fires
  paused: false        # true: signals are recorded but nothing fires
  maxBudgetUsd: 10     # ceiling for one run's budget
  allowRemoteManage: false
  allowUnattendedModes: false
```

The file is read on every tick, so an edit applies without a restart. An
unreadable file counts as off.

## Create one

```sh
smind automation create --name nightly-triage --workspace 1 --profile 2 \
  --model claude-sonnet-5-5 --cron "0 9 * * *" \
  --title "Daily triage" --prompt "Summarise what changed on develop yesterday."
smind automation enable 1
```

A new automation is always created disabled. `smind automation list` shows its
state and `smind automation show 1` shows the next fire times.

## Signals

- **cron**: five fields in UTC, for example `17 19 * * *`; `@hourly`, `@daily`
  and `@weekly` also work. A schedule that can fire more often than every five
  minutes is rejected. If the daemon was off at the scheduled time, it fires
  once when it comes back if that is within 15 minutes of the time, and
  otherwise records one "missed" entry and waits for the next time. It never
  catches up a backlog.
- **github.workflow_run**: a run of a named workflow on a branch (default
  `develop`) that ended in `failure` (the conclusions can also list `cancelled`
  and `timed_out`; `failure` is the default). The
  daemon asks GitHub through `gh`, by default every five minutes, so there is no
  public URL to set up. Enabling the automation never replays old runs.

## What starts, and how it is limited

The task uses the profile, model and permission mode you wrote in the
automation. It never auto-approves anything: a permission request waits for you
like any other task. A template that asks for `autoAccept` or a full-access
mode is refused unless `automations.allowUnattendedModes` is true.

| Limit | Default | What happens |
|---|---|---|
| Runs per hour | 2 (at most 12) | Further signals are recorded as capped. |
| Identical signal | 24 hours for CI signals | The same commit failing again is recorded as deduped. |
| One at a time | always | A signal while the last task still runs is capped. |
| Failed runs in a row | 3 | The automation pauses with a reason; `smind automation resume` clears it. |
| Budget per run | $3 | Set on the task's tree; the next firing is capped if the run reached it. |
| Wall clock | 45 minutes | The run is stopped and counts as failed. |

The budget stops the Lead from launching more Peers once reached; it does not
kill a running run, which is what the wall clock is for.

## Signal text is data

What a signal carries goes into the prompt as quoted data with a warning that it
may contain text that looks like instructions. Log text never goes in: the task
fetches it itself (for example `gh run view <id> --log-failed`) and reads it as
tool output. Only plain ids, the commit, the branch and the run URL can be used
as `{{signal.runId}}`-style placeholders.

## Memory

With `--memory` an automation keeps one notes file, `MEMORY.md` under
`automations/<id>/` in your smind home (16 KiB at most). The task reads it as
quoted data and updates it by ending its final message with a block:

````
```automation-memory
- the tests in internal/foo are flaky on Windows only
```
````

smind writes the file itself; a block that is too large, not text, or that looks
like it contains a token is dropped and the old notes stay. Read or clear it with
`smind automation memory <id> [--clear]`.

## History

Every signal is recorded, whether it fired or not: `smind automation audit <id>`
lists the decision (fire, deduped, capped, paused, missed), the reason and the
task it started. History older than 90 days is removed, but the last 100 entries
of each automation are always kept.

## Who can do what

- On the computer (a direct local connection) you can create, edit, enable,
  resume, fire now and delete.
- From a phone over the relay you can look at automations and history and
  disable one, but not create, enable, resume or fire.
- `automations.allowRemoteManage: true` lets a direct network connection manage
  them too.

## In the apps

On the web, automations live under **Settings → Automations**. The page lists
each automation with its signal, its state (active, paused or disabled), when
it last fired and, for an enabled cron automation that is not paused, its next
run in UTC. Each
row has an enable/disable switch, **Fire now**, **History**, **Edit** and
**Delete**; a paused row shows its reason with a **Resume** button. **New
automation** opens the form: workspace, agent profile, model and mode, the
signal (cron in UTC with a next-fire-times preview, or a GitHub workflow and
branch), the title and prompt templates with the placeholder list, the rails
with their defaults, and the memory switch. **History** is the audit table,
filterable by decision, and each `fire` row links to the task it started. The
page also says when `automations.enabled` or `automations.paused` is holding
everything back, but it has no control for them: those are `config.yaml` edits.

On a phone, the Connection sheet on the Tasks screen has an **Automations**
entry. The list is read-only (state, signal, last fired), and an automation's
screen shows its header and its run history, paged with **Load more**. The one
action is **Pause**: a confirm, then the automation is disabled. Create and
change automations on the computer.

## When one pauses

An automation pauses itself when its runs keep failing: after the configured
number of failed runs in a row (default 3) it stops firing and records the
reason, `paused after 3 consecutive failed runs; last: …`. The reason is shown
by `smind automation show <id>`, on the web row and on the phone's automation
screen. A run that reached its budget does not pause the automation; it blocks
the next firing, which is recorded as capped with the reason (see the table
above).

An auto-pause sends a push notification to paired phones when push is set up
(see [Mobile and relay pairing](mobile-and-relay.md#push-notifications)). A
tap opens the failed task; when there is no task to open — the template is no
longer valid, or the firing never started a task — the notification opens that
automation's own screen.

`smind automation resume <id>` clears the pause, the failure counter and the
budget block. A paused automation is not an inbox item: the inbox lists
permissions, validator verdicts and stalls only.

## Example: a failed nightly CI run starts a Lead

This is the example smind ships and tests. It is never created or enabled for
you: you copy it, point it at your workspace and profile, and turn it on.

When the workflow named `Windows full suite` fails on `develop`, an automation
starts one Lead task. The task fetches the failed log itself, finds the failing
tests, and either proposes a fix pull request or opens an issue. It runs in the
`default` permission mode, so every command it wants to run waits for you; it is
limited to $3 and 45 minutes, and two runs an hour.

```sh
cp docs/examples/automation-windows-full-suite-failed.json my-automation.json
# edit workspaceId and template.profileId (smind workspace ls, smind profile ls)
smind automation create --file my-automation.json
smind automation enable <id>
smind automation fire <id>      # rehearse it on the latest failed run
```

The prompt the task receives holds the run id, commit, branch and run URL as a
quoted JSON data block, plus the instruction to fetch the log with
`gh run view <run id> --log-failed`. The log text is never put in the prompt, so a
failing test whose name says "ignore previous instructions" is only ever read as
tool output. Anything the task writes to its memory is handed back to the next
run as quoted data too.

What you will see in `smind automation audit <id>`:

| Decision | When |
|---|---|
| `fire` | A new failed run; a task was started. |
| `deduped` | The same commit failed again (for example a re-run). |
| `capped` | A task from this automation is still running, or the hourly cap was reached. |
| `paused` | The automation paused itself after three failed runs in a row; `smind automation resume` clears it. |

## See also

- [Inbox](inbox.md): the list of permissions, validator verdicts and stalled work that need you.

