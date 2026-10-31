---
title: Z.AI usage meter
description: See the 5-hour and weekly windows of a Z.AI GLM coding plan, and let a used-up window hold work back.
summary: "This page explains the Z.AI usage meter in sMind by Spacing Mind: plan windows for a Z.AI provider instance on the Usage page and the provider's detail page, how often smind calls Z.AI, which settings turn it off and how a used-up window acts like a rate-limit cooldown. It is for people who run GLM through a Z.AI coding plan."
order: 23
---

# Z.AI usage meter

If you added a Z.AI login with the `zai-glm` [recipe](provider-recipes.md) (or
an instance whose vendor is Z.AI), smind can show how much of your coding plan
you have used, the same way it shows plan windows for Claude and Codex accounts.

The meter reads Z.AI's plan-quota endpoint, which is taken from Z.AI's own
documentation and has not been confirmed against a live account. If a number
looks wrong, check it against Z.AI's own page.

## What it shows

For each Z.AI instance, one card with the plan name Z.AI reports (its level)
and:

- **5-hour**: the percent of the 5-hour window used and when it resets.
- **Weekly**: the same for the weekly window. Some plans have none, and then it
  is simply absent.
- **Tool calls (monthly)**: a balance of the monthly tool-call pool, shown as
  used, remaining and limit. It is a balance, not a window, and never affects
  routing.

If the instance has no key, the card says so. If Z.AI reports no coding plan, or
rejects the key, the card says that instead of showing numbers. Only the
instance's own key is used, and it is never shown.

Where to look:

- **Settings → Usage** lists your Z.AI instances below the accounts.
- The **provider's detail page** in **Settings → Providers** shows the same lines
  plus a "Rate limited until" line when the instance is cooling down.

The percentages setting (**Used** or **Remaining**) on the Usage page applies to
these cards too. The phone's usage screen lists accounts only.

## When smind calls Z.AI

smind never polls in the background. It calls Z.AI only when you act:

- opening Settings → Usage or the provider's detail page reads the cached
  reading, and fetches a new one only if the cache is older than the TTL
  (`accountUsage.ttlSeconds`, default 300 seconds, which is also the minimum);
- the refresh button asks for a fresh reading, with at least 30 seconds between
  forced attempts and at most 6 per hour;
- a failure backs off and honours Z.AI's retry hint, and never replaces the
  last good reading;
- only Z.AI's own hosts are ever called: `api.z.ai`, `open.bigmodel.cn` and
  `bigmodel.cn`, chosen from the instance's base URL.

Reading usage is for people: agents cannot trigger a fetch.

## A used-up window holds work back

A used-up cached window acts like a rate-limit cooldown. When the 5-hour or
weekly window reaches `accountUsage.exhaustThresholdPct` (default 100), smind
treats the instance as limited until the later of that window's reset and any
reset time from an actual rate-limit answer, capped at the maximum cooldown.
The decision reads the cache only; it never calls Z.AI. Your own sends are never
blocked by it, and a send from an agent fails at once naming the provider and
the time. See [Usage and limits](usage-and-limits.md#rate-limits-wait-or-fail-over).

## Turn it off

These keys in `config.yaml` (restart the daemon after changing them):

| Key | Default | Effect for Z.AI |
|---|---|---|
| `accountUsage.enabled` | `true` | `false` turns off every outbound usage call, Z.AI included |
| `accountUsage.ttlSeconds` | `300` | how long a reading stays fresh (minimum 300) |
| `accountUsage.routing` | `true` | `false` stops a used-up window from holding work back; the meter still shows |
| `accountUsage.exhaustThresholdPct` | `100` | the percent used (1 to 100) that counts as used up |

## Related

- [Provider recipes](provider-recipes.md): add the `zai-glm` instance.
- [Usage and limits](usage-and-limits.md): accounts, plan windows and rate limits.
- [Write-only secrets](write-only-secrets.md): where the Z.AI key is stored.
