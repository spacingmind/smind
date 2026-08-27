---
name: plan
description: Create or update a docs/plans/active/<slug>.md file for multi-session work, per AGENTS.md rule (c). Use when starting work that will span more than one session, or when asked to "make a plan", "track this work", or "write a plan doc".
---

Multi-session work gets tracked in `docs/plans/active/<slug>.md`, not just in
conversation. This skill creates or updates that file.

## Creating a new plan

1. Pick `<slug>` — short kebab-case, matching the work (e.g.
   `phase-1-routing-engine`, `accounts-registry`).
2. Write `docs/plans/active/<slug>.md` with these sections:

```markdown
# <Title>

## Decisions

Architectural or scope decisions made for this work, and why. Link to
docs/decisions/ ADRs where one was written.

## Progress

Running log of what's done, in progress, and not started. Update this as
work proceeds — don't let it go stale mid-session.

## Validation

How this was/will be verified: commands run, manual checks, what "done"
means concretely.
```

3. Keep the file updated as work proceeds — this is a living doc, not a
   one-time snapshot.

## Finishing a plan

When the work is done and validated, `git mv` the file from
`docs/plans/active/<slug>.md` to `docs/plans/completed/<slug>.md`, with the
Progress and Validation sections reflecting the final state.
