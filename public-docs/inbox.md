---
title: Inbox
description: One list of permissions, validator verdicts and stalled work that need you, on the web and on a phone.
summary: "This page explains the inbox in sMind by Spacing Mind: one list of permission requests, validator verdicts and stalled task trees that need a person, on the web and on a phone. It is for anyone who runs agents and wants one place to look instead of watching each task."
order: 8
---

# Inbox

The inbox is one list of what needs a person. It is derived live from the real
state of the daemon on every read: the permissions a run is waiting on, the
validator verdicts waiting for your decision, the task trees that have gone
quiet. Nothing is stored except what you did to an item: seen, snoozed or
resolved. An item that no longer derives (a permission answered, a verdict
superseded, a stall cleared) leaves the list on its own, and a restart cannot
lose or duplicate anything because there is no stored list to drift.

## What appears here

Three kinds of item:

| Kind | What it is | When it leaves |
|---|---|---|
| **permission** | A tool a run wants to use and is waiting for you to allow. The row shows a redacted line (the tool and, for a file, its path); the full request stays in the card. | When you answer it, or the run ends and retires it. It cannot be snoozed, resolved or hidden: it blocks a run. |
| **verdict** | The newest validator verdict of a task that needs you: a fail, a blocked verdict or a scope violation, or a pass at the current commit that is ready for your merge decision. The validator itself is described in [Orchestration](orchestration.md). | By itself when it is superseded: a push makes its commit stale, a newer validation replaces it. Or when you resolve it. |
| **stall** | A task tree with unfinished work but no run and no queued prompt for a while. | When the tree runs again, or when you resolve it. |

On the web the list is grouped **Needs you now** (permissions), **Validator
verdicts** and **Stalled**. The phone uses the same three groups.

## Open it

On the web, the sidebar footer has an **Inbox** button with a badge; it opens
the inbox screen over the app. On a phone, the Tasks screen's header has an
**Inbox** button with the same badge (see
[Mobile and relay pairing](mobile-and-relay.md)).

## Act on an item

A **permission** can only be answered, one request at a time. On the web the
row has **Review request**, which expands the same permission card the task
page shows, with the full request, path and diff, and you answer it there. On a
phone the row opens the task and you answer on the task screen. There is no
approve-all, and no snooze, resolve or hide for a permission.

A **verdict** or a **stall** can be:

- marked **seen**;
- **snoozed** — on the web for 1 hour, 4 hours or Tomorrow (24 hours); on a
  phone for 1 hour, 4 hours or until tomorrow. Every snooze is under 24 hours,
  and when the time has passed the item is new again;
- **resolved**, which takes it out of the list;
- opened: **Open task** goes to the task the item is about.

## Keyboard (web)

`j`/`k` and the arrow keys move between rows. `Enter` opens the focused row:
for a permission it expands the request, for anything else it opens the task.
`s` snoozes with the first option (1 hour) and `e` resolves. `s` and `e` never
act on a permission.

## Who can do what

Reading the inbox works over the relay too, so a paired phone sees the same
list. `seen`, `snooze` and `resolve` change only your own view, so they work
from a phone as well, but every inbox call is refused for agents and for tasks
an automation started: no run can touch your inbox. Answering a permission
from the inbox goes through the same path as on the task page, with the same
checks; the inbox adds no new way to answer one. The inbox never merges
anything, and a paused automation is not an inbox item (see
[Automations](automations.md)).

## Badge

The badge on the Inbox button counts the new items plus every pending
permission: a permission always counts, even once the row is marked seen. A
snoozed item is not counted until its time has passed. Over 99 the badge shows
99+.
