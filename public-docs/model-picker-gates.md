---
title: Why a provider or model is greyed out
description: What the Signed out, Plan used up, Cooling down, Not in plan and Unsupported labels in the provider and model picker mean, which ones block you and which only warn.
summary: "This page explains the gate reasons in the provider and model picker of sMind by Spacing Mind: the five reasons, the way out each one offers, the difference between a hard gate that blocks a send and an advisory gate that only warns, and how a Lead or automation sees the same refusal. It is for people who pick providers and models by hand and for people who script agents."
order: 24
---

# Why a provider or model is greyed out

The provider and model picker tells you before you send that a row cannot run,
and what to do about it. It is the early twin of a failed run: the
[run failure classes](usage-and-limits.md#why-a-run-failed-and-what-to-do) tell
you after the fact, the gate reasons tell you before. The daemon works the
reason out; the web app, the phone app and the MCP tools only show it.

## The five reasons

| Badge | Reason | What it means | Way out |
|---|---|---|---|
| **Signed out** | `not_signed_in` | No usable login for the provider, or a required API key is not set. | **Sign in** (opens **Settings → Providers**), or add the key. |
| **Plan used up** | `out_of_plan` | The plan's usage window is spent. The row shows when it resets. | Wait for the reset, or **Switch** to the profile's fallback if it has one. |
| **Cooling down** | `cooling_down` | An earlier run was rate limited, and the provider is on cooldown until the time shown. | Wait, or **Clear** it if you have reset the quota yourself (see [I reset my quota](usage-and-limits.md#i-reset-my-quota)). Only a person can clear it. |
| **Not in plan** | `model_not_in_plan` | The account's plan does not include this model. Shown on the model row. | **Pick model**: choose one the plan allows. |
| **Unsupported** | `unsupported_by_harness` | The agent harness cannot run this row, for example a chat-completions-only endpoint behind the Codex harness. | **Pick provider**. |

A row has at most one reason. When several apply, the most permanent wins, in
this order: unsupported, signed out, not in plan, plan used up, cooling down.
A provider with several accounts is gated only when no account can serve it:
one healthy account ungates the provider, and the gate then appears on the
models that account excludes.

The picker never calls the provider to find out. It reads what smind already
knows: stored accounts, cached plan windows, cooldowns and recent failed runs.

## Hard gates block, advisory gates warn

| Kind | When | What happens |
|---|---|---|
| **Hard** | A fact smind can enforce: a required provider key is not set, the harness cannot run the endpoint, the provider is on cooldown, or a provider instance's own plan window is used up. | The row is disabled in the picker. A send from an agent or an automation is refused. |
| **Advisory** | A guess from cached or inferred evidence: an account's cached usage window, or a recent failed run that looked like a sign-in or model problem. | The row shows a warning and stays selectable. Nothing is refused. |

Advisory exists because smind runs on the agent CLI's own login. The evidence
may be about a different account than the one the CLI is signed in to, so
smind warns but never gets in your way.

In the web app a gated row stays visible, shows its badge and reason, and
offers its way out as a button. Press Enter on it to run that action instead of
selecting the row. The picker's trigger shows a warning dot when the provider or
model you have selected is gated. On a phone, the new-task picker adds the
reason to the row label and a notice line with the action, and blocks hard
gates.

## What a Lead or an automation sees

Your own sends from the app are never blocked by a cooldown. A send from an
agent (the MCP `task_new` and `task_send` tools) or from an automation to a
hard-gated provider fails at once, without creating a run, with the error code
`gated` and the gate as data: the reason, the end time when there is one, and
the action. The message says what not to do: do not retry and do not fall back
to another provider on your own. A Lead can read `provider_list` to see every
gate and route on the reason.

```json
{
  "reason": "cooling_down",
  "until": "2026-11-01T15:30:00Z",
  "action": { "kind": "clear_cooldown", "target": "glm", "label": "Cooling down until 15:30: clear it if you reset the quota" }
}
```

## How a gate relates to a failed run

Each reason has a twin among the run failure classes, so the next step reads
the same before and after a send:

| Gate reason | Failure class |
|---|---|
| `not_signed_in` | `auth_expired` |
| `out_of_plan` and `cooling_down` | `plan_window_exhausted` |
| `model_not_in_plan` | `model_not_in_plan` |
| `unsupported_by_harness` | none |

A short throttle (`transient_429`), an unclassified rate limit and an agent
that exited (`cli_exited`) never gate a row: nothing can predict them.

## See also

- [Usage and limits](usage-and-limits.md): the failure classes, rate limits and
  the cooldown.
- [Providers](providers.md): add or sign in to accounts.
- [Orchestration](orchestration.md): how a Lead picks providers for its Peers.
