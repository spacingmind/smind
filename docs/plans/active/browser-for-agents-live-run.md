# Browser for agents: the live run (real models, a dogfood daemon)

Governing: ADR-0061 and `desktop-browser-for-agents.md` ("Pre-flip checklist items NOT done": a live run with a real model, the Claude path against a real `claude` binary, card frames). Human decision 2026-11-25: one live run, **budget USD 2 total, hard stop**. Everything here is evidence gathering: nothing in this item changes behaviour; a bug found is a follow-up (a separate PR the Steward reviews), not a fix inside this item unless trivial.

## What is exercised

| # | Run | Provider and model | Path it closes |
|---|---|---|---|
| R1 | the main flow | `claude-native`, `claude-haiku-5-5`, the REAL `claude` binary | the unbound socket bound to the CLI pid (SDK v0.4.3), the read grant card, the open card, snapshot, screenshot (an image), wait, console, `browser_close`, a refused host |
| R2 | the refusals | `claude-native`, Haiku | a card that is DENIED (the model is told "the person said no"), the kill switch (calls refused, grants dropped, session ended), a host that is not allowlisted (policy reason, no card) |
| R3 | the ACP path | `glm` (a model that cannot read images) | the row over `session/new` `mcpServers`, a screenshot answered with the text note instead of an image |

Codex is not run: it takes no per-run MCP servers, the reach table gives it no row, and a live Codex turn costs real money for a result the unit tests already pin (`TestACPPath_ReachTableDecidesTheSocket` and the codex branch). Stated, not skipped silently.

Acting tools: the catalogue today has only `browser_open` and `browser_close` as acting tools (the click/fill tools do not exist yet), so "an acting-tool card" is exercised with `browser_close` (asks, covered by the run's open grant when granted, asks otherwise) and `browser_open` (asks every call unless granted). No other acting tool is claimed.

## Setup (all of it in a dogfood root, nothing of the Human's)

- Root: `~/.smind/dogfood/browser-live/`. `SMIND_HOME=<root>/home` (its own config, db, token, listener files), its own port (checked free with `lsof` before starting; never :4648, never the ports other dogfoods use), `SMIND_LISTEN=127.0.0.1:<port>`. Browser profiles go under `<root>/home/browser/`.
- `HOME` stays the real one: the `claude` CLI and the GLM agent keep their logins there (`~/.claude`, `~/.glm-acp-agent`); a fresh HOME would have no login and no run could start. This deviates from "a fresh HOME" on purpose; the daemon's own data (db, config, token, scratch, browser profiles) is entirely under the dogfood root, and nothing of the real `~/.smind` is read or written. Same arrangement as the earlier live dogfoods (`e1-2-live`).
- Binary: built from `origin/develop` (PR 1 to PR 6 merged) with `task build`, copied into the root.
- Target pages: a tiny loopback HTTP server started and stopped by the run script (one page with a heading, a link, a form with a password field, a console message, a delayed element). One origin is `127.0.0.1:<port>` (loopback, allowed by default); the ADDED host is this machine's LAN address (a private range, refused unless the exact host is added), with a second small server bound to that address only (not a wildcard). A third address (the machine's tunnel address, also a private range) is never added: it is the refused host. If the LAN address is absent, the run says exactly what was used instead.
- The browser is enabled **as the human**: `smind browser enable --workspace <id>` and `smind browser allow add <host> --workspace <id>` from the CLI, never through an agent.
- Cards: the run script drives the real web UI of the dogfood daemon (an isolated Chromium pointed at it) to capture each permission card in light and dark at 1280 and 390, then answers it as the human (the UI button or the answer RPC of the local connection).

## Expected observations (per step)

1. R1 first tool call: a **read grant card** ("Let this run read browser pages?", options Allow reads for this run / Allow once / Deny, the "daemon's own check" line). Answered "for this run": `browser_tabs`, `browser_snapshot`, `browser_screenshot`, `browser_console`, `browser_wait` then run with no further card.
2. `browser_open` on the loopback page: an **open card** (the URL without query or userinfo, `(+query, N characters)` and `(+fragment)` markers, the host's status, "This run has used the browser N times", the breadth of "Allow opening any page the browser policy allows ... for this run"). Answered "for this run".
3. `browser_snapshot` returns the page inside the untrusted-content envelope; `browser_screenshot` returns an **image** for Haiku; `browser_console` returns the page's console line; `browser_wait` returns within its cap.
4. `browser_open` on the added LAN host: allowed (the open grant covers it); on the tunnel address: **refused with the policy's reason and no card**.
5. `browser_close`: covered by the open grant, or a **close card** ("Close a browser tab?") when not.
6. R2: a card is **denied**; the model's tool result says the person said no; three denials in a minute would pause cards (not exercised: costs turns). The kill switch (`SMIND_BROWSER_DISABLED` or `browser.disabled`, daemon restarted): the next call is refused with "disabled by the daemon's configuration", the grants are gone, no session is left running.
7. R3: the same flow reduced to open, snapshot, screenshot; the screenshot answer is the **text note**, not an image.
8. Every call leaves one line in the run timeline (URL redacted, outcome, no page content).

## Abort rules

- Stop at **USD 2.00 total** across all runs; stop earlier (and do not start the next run) when the running total plus the next run's ceiling would pass it, or when costs run ahead of the table below. Each task is created with `--budget` as a cap.
- Abort a run on: a request to any host the run did not name, a call the agent answers for itself, a card that cannot be answered, the daemon failing, the page or the agent looping. `smind task stop`, then kill only the PIDs the run script started.
- Tiny prompts, few turns (the prompts say how many tool calls to make and to stop), Haiku pinned by id, GLM pinned by profile default; no retries without reading the previous run's log first.
- Cleanup at the end, whatever happened: stop the daemon and the two test servers by PID, remove any Chrome profile and socket under the root, leave only logs, frames and the README.

## Budget table (USD; estimates before the run, actuals filled in after)

| Run | Estimate | Cap (`--budget`) | Actual (`smind task runs` / usage) |
|---|---|---|---|
| R1 Haiku main flow | 0.10 | 0.60 | |
| R2 Haiku refusals | 0.05 | 0.40 | |
| R3 GLM | plan quota, not billed per call | 0.40 | |
| Total | 0.15 | 1.40 | |

## Evidence paths (outside the repo)

`~/.smind/dogfood/browser-live/`: `run.log` (the script's own log), `home/daemon.log`, per-run `r1.send.out`, `r2.send.out`, `r3.send.out`, the run histories exported with `smind task attach`/RPC, `frames/` (cards, light and dark at 1280 and 390), `contact-sheet.png`, `README.md`.

## Progress

- [ ] Plan committed (this file)
- [ ] Binary built from develop, dogfood daemon up on its own port
- [ ] R1, R2, R3 run and logged
- [ ] Card frames, contact sheet, README, every pair opened
- [ ] Cleanup, Validation filled in

## Validation

(filled in after the runs: per run provider, model, cost, turns, what worked, what failed or was odd, evidence paths; bugs found as follow-ups)

## Spend log

| Time | Run | Task id | Cost reported | Running total |
|---|---|---|---|---|
