# Fixture sessions

Synthetic sessions in the layout the providers write (verified against real
sessions on a development machine, 2026-11-05); the content is made up.

- `claude/session.jsonl`: a Claude Code transcript, session id
  `11111111-2222-4333-8444-555555555555`, cwd `/work/repo`. Turns: "Summarize the
  repository" (09:00), "Fix the failing test" (09:10, with thinking, a heredoc
  command, a result that is an array, an error result, a sidechain record and
  noise record types), "continue" twice (09:20 and 09:25: a repeated prompt that only the window tells apart).
- `codex/rollout-...-01a1aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee.jsonl`: a Codex
  rollout with the same four turns.

All times are 2026-11-05 UTC.
