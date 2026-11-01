---
title: Usage and limits
description: Token usage, plan windows and what happens at a rate limit.
summary: "This page explains how sMind by Spacing Mind records token usage, shows plan usage windows per account, and keeps work moving when a provider reports a rate limit. It is for users watching cost and quota."
order: 9
---

# Usage and limits

smind records the tokens each run uses, shows how much of each account's plan
you have used, and has a few ways to keep work moving when a provider says
"rate limited".

## Usage in Settings

Open **Settings → Usage**. The page has two blocks.

**Token usage** lists tokens and cost per run and per proxy request. Three
rows of controls shape the table:

- **Scope**: **Runs** (the default), **Proxy** or **All**. Runs are the agent
  runs you started. Proxy is requests that went through smind's built-in proxy.
- **Group by**: **Task**, **Provider**, **Model**, **Workspace** or **Space**.
  With the **Proxy** scope only **Model** is available.
- **Range**: **Today**, **7 days** or **30 days**.

Each row shows a count (runs or requests), **Input**, **Cached**, **Output** and
**Reasoning** tokens, and **Cost**. A dash means the provider did not report
that number. Use **Refresh** to reload the table.

The cost is the figure the agent reported for the run. smind does not compute
it. For a provider you signed in to with a subscription plan, the cost is only
an estimate at API list prices, because a plan is not billed per token. Those
rows show **≈ API price** next to the amount, and the tooltip says "Estimated
at API list prices; subscription plans are not billed per token". Proxy rows
have no cost.

From the command line:

```sh
smind usage
smind usage --scope runs --by provider
smind usage --scope runs --by workspace --since 2026-10-01T00:00:00Z
smind usage --scope proxy --by day --until 2026-10-31T23:59:59Z
```

| Flag | What it does |
|---|---|
| `--scope proxy\|runs\|all` | Which usage to count. The default is `all`. |
| `--by <group>` | One of `account`, `model`, `day`, `workspace`, `task`, `chat`, `run` or `provider`. The default is `account`, or `task` with `--scope runs`. |
| `--since <RFC3339>` | Start of the range. |
| `--until <RFC3339>` | End of the range. |

`account` and `day` group proxy traffic. `workspace`, `task`, `chat`, `run` and
`provider` group runs. `model` groups both. A combination that makes no sense
is rejected, for example `--scope proxy --by task`.

## Plan windows

A subscription plan limits how much you can use in a window of time. smind
reads those windows for each account and shows them in the **Plan usage** block
at the top of **Settings → Usage** ("Plan windows and reset times for each
account").

Each account card shows its name, provider and plan, then one bar per window:

- **Session**: the 5-hour window.
- **Weekly**: the weekly window.
- **Weekly · `<model>`**: a weekly window that applies to one model only, when
  the provider reports one.

A bar shows how much is used and when the window resets. The **Percentages**
choice under **Settings** (the button in the block's header) switches between
**Used** and **Remaining**. **Show summary in sidebar** pins a short plan usage
summary to the sidebar footer. With more than one account, an **Account**
filter picks one. **Refresh all**, or the refresh button on a card, fetches
fresh numbers.

An account that has no plan windows, such as an API-key account, says so; that
is not an error. If the login has expired, the card says to sign in again.

smind keeps the number of calls to provider usage endpoints low, so a refresh
can be refused for a while.

From the command line:

```sh
smind account usage
smind account usage --refresh
smind account usage --json
```

It prints one block per account: the plan, each window with its percent used
and the time to reset, and whether a window comes "from recent traffic" rather
than from the provider, and marks a report as stale when it shows the last good one. `--refresh` asks for fresh numbers first and says when
that is not allowed. `--json` prints the raw result.

These keys in `config.yaml` tune the feature. Restart the daemon after you
change them.

| Key | Default | Meaning |
|---|---|---|
| `accountUsage.enabled` | `true` | Turns off every outbound usage call when `false`. |
| `accountUsage.ttlSeconds` | `300` | How long a reading stays fresh. 300 is also the minimum. |
| `accountUsage.routing` | `true` | Lets routing steer away from exhausted accounts. |
| `accountUsage.exhaustThresholdPct` | `100` | The percent used (1 to 100) at which an account counts as exhausted. |
| `accountUsage.passive` | `true` | Learns limits from rate-limit answers on proxied requests. |

## Rate limits: wait or fail over

When a provider rejects a request with a rate limit, smind notes it. The
account card then shows **Rate limited** with the time it lasts until, and a
failed run's footer in the task shows what you can do.

**By hand.** On the latest rate-limited run, the footer offers:

- **Wait until `<time>`**: queues the same prompt on the same chat, to start
  shortly after the limit lifts (30 seconds of slack). The footer then reads
  "Queued · starts after `<time>`". A time marked with ≈ is an estimate.
- **Retry on `<agent>`**: appears when the run's agent profile names a fallback.
  It continues the work in a new chat on the fallback agent.
- **I reset my quota, retry now**: appears while smind still holds a rate-limit
  cooldown for the provider. See "I reset my quota" below.

**Automatically.** An agent profile can say what to do on a rate limit. In
**Settings → Agents**, the **On rate limit** field has three choices:

| Choice | Behavior |
|---|---|
| **None** | Do nothing. This is the default. |
| **Wait** | Queue the same prompt on the same chat, to run after the limit lifts. |
| **Fallback** | Continue on the **Fallback agent** you pick, in a new chat on the same task. |

On the command line, use the same names on `smind profile add` (and
`smind profile edit`):

```sh
smind profile add main claude-native --on-rate-limit=wait
smind profile add alt codex-native
smind profile add steady claude-native --on-rate-limit=fallback --fallback-profile=2
smind profile ls
```

`--fallback-profile` takes the id of another profile, as `smind profile ls`
shows it. The fallback must use a different provider than the run that was
limited, and that provider must not be rate-limited itself. The continuation
gets a note that the earlier attempt stopped at a rate limit, the tail of its
output, and the task's worktree with its changes.

Some details:

- It acts only for runs started with an agent profile, and at most once per
  failed run. A retry that is limited again does not trigger another action.
- **Wait** needs a reset time in the future. Without one, nothing is queued.
- If the provider has several accounts, smind prefers one that is not
  exhausted when it routes a request. It judges this from the plan windows it
  already has. It never fetches usage to decide, and missing or stale data
  does not block a request.
- Your own sends are never blocked by a rate limit. A send that comes from an
  agent, for example from an orchestrating task, fails at once with a message
  naming the provider and the time it is limited until, so the agent can pick
  another provider.

## Why a run failed, and what to do

A failed run says why in one of a few classes, with a next step. The task
footer, the inbox, `smind task runs`, `smind task wait` and the MCP tools all
show the same class and step.

| Class | Meaning | Next step |
|---|---|---|
| `auth_expired` | The provider refused the login. | Sign in again to that account in **Settings → Providers**. Never retried and never failed over: it cannot work until you sign in. |
| `plan_window_exhausted` | The plan's usage window (5 hours, weekly) is used up. | Wait until it resets, or continue on the profile's fallback agent. |
| `model_not_in_plan` | The account's plan does not include the model you chose. | Pick another model. Never retried on the same account. |
| `transient_429` | A short throttle from the provider, not a quota. | A run from an agent profile or an orchestrating task is retried twice by itself (after 30 seconds, then 2 minutes; the provider's own wait hint wins, between 5 seconds and 5 minutes). A plain send of yours gets a **Retry** step instead. No retry starts while the provider is on cooldown. |
| `rate_limited` | A limit smind cannot tell apart (no window, no reset time). | Retry after a short wait. |
| `cli_exited` | The agent program ended without a result. | Continue the run. |

A failed run also appears in the inbox as a **Run failed** item, with its next
step as a button, until a later run replaces it; `auth_expired` and
`model_not_in_plan` are marked high attention because only you can fix them.
On the command line, `smind task wait` exits 6 for the limit classes and 7 when
you must fix setup (see [`task wait`](cli-reference.md#task-wait)).

The provider and model picker shows the same states before you send. See
[Why a provider or model is greyed out](model-picker-gates.md).

## I reset my quota

smind keeps a provider on cooldown until the reset time the provider gave, even
if you reset or top up the quota yourself in the meantime. To tell smind the
limit is gone, use **I reset my quota, retry now**, on a rate-limited run's
footer or in **Settings → Providers** (the provider shows "Rate limited until
`<time>`"). On the command line:

```sh
smind cooldown ls
smind cooldown clear glm
```

Clearing the cooldown lets agent sends to that provider start again, and wakes
the retries that **Wait** queued for it: they start now instead of at the old
reset time. Each retry starts once; clearing again does nothing. Only a person
can do this: agents cannot clear a cooldown, and the action is not available to
plugins or automations. Clearing does not check the provider: if the limit is
still in force, the retried run fails and a new cooldown starts.
