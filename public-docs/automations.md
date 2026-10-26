---
title: Automations
description: Start a task by itself on a schedule, or when a GitHub workflow fails, with caps, de-duplication, auto-pause and a record of every decision.
order: 15
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
  `develop`) that ended in `failure` (also `cancelled`, `timed_out`). The
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
