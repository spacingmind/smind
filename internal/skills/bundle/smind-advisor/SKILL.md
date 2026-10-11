---
name: smind-advisor
description: Spin up a single smind agent as an advisor, a second opinion on the current task without delegating the work. Use when the user says "advisor", "second opinion", or wants an outside take.
user-invocable: true
argument-hint: "[--profile <name>] <question or topic>"
---

# Advisor

One agent reads the situation and gives a judgment. You decide what to do; the advisor does not drive the work.

**User's request:** $ARGUMENTS

## Prerequisites

Read the **smind** skill. Call `profile_list` first.

## Pick the advisor

1. A profile the user named (`--profile`) wins.
2. Otherwise the profile whose `notes` fit the question (design, audit, root cause).
3. Prefer a different provider family from your own, so the opinion is genuinely fresh.

An advisor must not edit files. smind cannot force that with a prompt alone, so choose a profile or mode that is read-only (a `plan` or read-only permission mode from `provider_list`, one that does **not** auto-approve). If no such mode exists, say so and rely on the suffix below.

## The briefing

Self-contained: the question, sharply; what you considered and ruled out; relevant files by path (do not paste, let the agent read); an explicit ask: "give me a recommendation, with reasoning." End with:

```
This is analysis only. Do NOT edit, create, or delete any files. Do NOT write code.
```

## Launch and read

1. `task_new` titled `[Advisor] <topic>` with `baseRef` of the repo's trunk (or `branch`/`prNumber` if the question is about that code).
2. `task_send` the briefing with the profile.
3. `task_wait`, then `task_logs { tail }` for the answer. Weigh it; do not adopt it blindly.
4. `task_archive` the advisor task when done.
