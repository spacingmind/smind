---
name: smind-validator-frames
description: Shoot a contract's frame items in a real browser on an isolated daemon (light and dark, 1280 and 390, transition frames, contact sheets of at most 12) and attach the evidence report. Use as the validator when the contract has frames items.
user-invocable: false
---

# Validator frames

For each `frames` item of the contract you must LOOK at the real UI. This skill runs the harness that produces the pictures; the looking and the observation stay yours.

## Run it (only when the contract has frames)

1. Write the contract's `frames` array (from `validation_contract_get`) to a JSON file in your worktree, for example `.validation-evidence/frames.json`. Optionally write `scenarios.json`, `{"F1": [steps]}`, to reach the state a frame names. Steps: `goto`, `reload`, `wait {ms}`, `waitFor {sel}`, `click {sel}`, `clickText {text}`, `fill {sel,text}`, `type {text}`, `press {key}`, `expectText {text}`, `shot {name}`, `burst {name, frames<=12, gapMs, then:{step}}` (timed screenshots while the step runs: the transition frames). A scenario with no steps gets: load, one still, a 10-frame reload burst.
2. Run, from your worktree root, in the foreground with a timeout of 15 minutes:
   `go run ./internal/qaframes/cmd/qaframes --frames .validation-evidence/frames.json --diff-base <contract base> [--scenarios ...] [--seed seed.json --fake-agent]`
   It starts its own daemon (temp home, free port; never the Human's daemon or ports 4648/4870), builds the web UI and a private binary, drives one browser, and removes everything it started when it ends or is interrupted. Do not start a daemon or browser yourself and never kill processes by name.
3. Read `.validation-evidence/qa-frames/report.md`: the diff-to-screens table (an `UNMAPPED` file means you pick the screen and shoot it), each frame's status, and its contact sheets. Open every sheet (at most 12 images each) and the stills.
4. For every frame item in your verdict: evidence = `file` entries for the sheet/still PNGs you looked at (paths relative to your worktree, under `.validation-evidence/`), and a `note` saying what you OBSERVED (state, overlaps, clipping, theme, a transition glitch). `captured` is not a pass: an observation that is not true to the picture is a fail.

## When it cannot run

Exit code 3 means the report says `blocked` with `no_browser` (no node, no playwright-core, no Chromium/Chrome) or `no_toolchain` (no bun/go, UI not built). Then report each affected frame item `blocked` with that reason and make the verdict `blocked` with `blockedReason: no_browser` (or `no_toolchain`), exactly as without the harness. Never guess a frame. Exit 2 is a refused or malformed run: fix the input.

## Rules

- Prompts are never sent. Content comes from `--fake-agent` and `--seed`. A scenario `send` step is refused unless `--allow-send` and `--model claude-haiku-5-5` are both given; do not use it unless the contract asks, and check the model in the usage.
- One browser at a time; do not run it twice in parallel.
- The harness is Unix-first (process groups); on Windows it is untested.
