# 0043: Opt-in AI-drafted commit message (daemon-side) — Rejected

## Status

Rejected (Human, 2026-10-25).

## Context

G17 (git/GitHub UX gap list) wanted a one-click AI draft of the commit message.
The proposal amended ADR-0006 decision 2 ("the daemon never generates commit
messages via a model call") with a narrow exception: a human-triggered,
isolated, no-tools Haiku run behind a new `task.draftCommitMessage` RPC that
filled the commit textarea, with a per-task rate floor, an hourly workspace
cap, single-flight, a per-workspace style setting and its own metered run
purpose.

## Decision

Rejected. ADR-0006 decision 2 stays exactly as written: the daemon never makes
a model call for commit messages. A daemon-side model call contradicts that
decision, and the surface it needed (a new RPC, a system-run purpose on the
shared learning executor, guards and caps, a settings column, a CLI command)
was a lot of cost and risk for little value.

## What replaced it

The commit bar has a **Draft with agent** button. It sends a normal prompt to
the task's own chat through the existing web send path (the same one "Submit
review" uses: the chat's current provider, model and mode, `whenBusy: queue`
per ADR-0024, nothing pinned or hidden). The agent's reply is an ordinary chat
turn; nothing fills the textarea. No daemon change, no new RPC, no new
settings. ADR-0006 is unchanged.
