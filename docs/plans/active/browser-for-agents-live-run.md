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
| R1 Haiku main flow | 0.10 | 0.60 | 0.01 (the first attempt was stopped by hand and recorded no cost; at most 0.02) |
| R2 Haiku refusals (a denied card; the kill switch) | 0.05 | 0.40 each | 0.01 + 0.01 |
| R3 GLM | plan quota, not billed per call | 0.40 | not reported (18k/123 tokens, "partial") |
| R4 Haiku close card (added) | 0.02 | 0.40 | 0.01 |
| Total | 0.17 | | 0.04 reported, at most 0.06 with the unmeasured attempt |

## Evidence paths (outside the repo)

`~/.smind/dogfood/browser-live/`: `run.log` (the script's own log), `home/daemon.log`, per-run `r1.send.out`, `r2.send.out`, `r3.send.out`, the run histories exported with `smind task attach`/RPC, `frames/` (cards, light and dark at 1280 and 390), `contact-sheet.png`, `README.md`.

## Progress

- [x] Plan committed (this file)
- [x] Binary built from develop, dogfood daemon up on its own port
- [x] R1, R2, R3 (and the added R4) run and logged
- [x] Card frames, contact sheet, README, pairs opened (the README lists which)
- [x] Cleanup (daemon and test server stopped by PID, no Chrome profile or socket left), Validation filled in

## Validation

Evidence: `~/.smind/dogfood/browser-live/` (`run.log`, `r*.runlogs.json`, `r*.timeline.txt`, `frames/`, `contact-sheet.png`, `README.md`). Binary: develop at `8e39da2e5` (PR 1 to 6). Dogfood daemon :4741, `SMIND_HOME` under the dogfood root, `HOME` real (the CLIs' logins; stated in the setup).

### Per run

| Run | Provider, model | Cost | Turns/tokens | Result |
|---|---|---|---|---|
| R1a | claude-native, `claude-haiku-5-5` | unmeasured (stopped run), at most 0.02 | 7 cards | aborted by me after about 3 minutes: my driver skipped a repeated card (same text as an earlier one), so the run waited on an unanswered card. Driver bug, not a product bug. |
| R1b | same, the REAL `claude` binary | 0.01 | 50 s, 1.4k in / 1.3k out | **worked end to end**: 10 cards for 8 tool calls (a provider-native prompt AND the daemon card per call, see below), open card, read-grant card, snapshot inside the untrusted-content envelope, a screenshot returned as a real JPEG image block (756x413, 17 KB), console line, `browser_wait` found the late element, the tunnel address refused with the policy's reason and no daemon card, the added LAN address passed the policy and the connection failed, `browser_close` covered by the open grant. The Claude path (unbound socket bound to the CLI pid, SDK v0.4.3) works against the real binary. |
| R2a | same | 0.01 | 14 s | the open card was denied: the tool answered "browser: the person said no", the timeline noted "browser_open refused by the person", the model stopped as told. |
| R2b | same | 0.01 | 39 s | the kill switch (`browser.disabled: true` in the config, flipped while the run was waiting on a provider prompt): the next call was refused "the browser is disabled by the daemon's configuration"; with it switched off again the same run's next `browser_open` raised the OPEN CARD AGAIN ("has used the browser 1 times"), so the open grant was dropped, and the tab id changed (the session was ended and a new one started). No Chrome left after the run. |
| R3 | glm (ACP) | not reported | 10 s, 18k/123 tokens | the row reached the agent over `session/new`; open and read cards raised; the screenshot answered with the text note "This provider cannot read images, so the image is not returned: use browser_snapshot." No provider-native prompts (the agent did not ask). `browser_close` raised no card (open grant). |
| R4 | claude-native, Haiku | 0.01 | 30 s | an open URL with a query and a fragment: the card says "(+query, 13 characters) (+fragment)" and shows no query text; with "Allow once" everywhere, `browser_close` raised its own card "Close a browser tab?". |

Not exercised, stated: Codex (no row by the reach table; a live turn costs money for a result the unit tests pin), the three-denials limiter, the `(path cut)` marker, an ACTING tool beyond `browser_open`/`browser_close` (none exists). The LAN host could not be a real second origin: the macOS application firewall (enabled, not changed) blocks inbound connections to a non-loopback listener, so the added host was shown passing the policy while the connection failed ("could not connect"); an allowed non-loopback page actually loading is NOT verified.

### Findings (follow-ups; nothing fixed in this item)

1. **UI, card option overflow (real defect, both themes):** the long run-grant option "Allow opening any page the browser policy allows (localhost and your added hosts) and closing tabs, for this run" does not wrap: at 390 px it is cut off at the card's right edge, and at 1280 px it runs past the card's rounded border (`r4-02`, `r4-06`, `r1b-02`).
2. **Copy:** the close card offers the same "Allow opening any page ... and closing tabs, for this run" label; on a close card it should say what it grants for a close ("Allow closing tabs, and opening pages, for this run").
3. **Double prompt:** every Claude-native tool call raised TWO cards (the CLI's own `run mcp__smind-browser__<tool>` prompt, then the daemon's), 10 cards for 8 calls; the daemon's is the one that holds (D-B), so the first is noise. A way to pre-allow the `mcp__smind-browser__*` tools in the provider (or a clearer label on the provider prompt) would halve the cards. GLM raised none.
4. **Page noise in the first tool result:** `[browser notices] blocked: www.google.com:443 ...` appears in the first `browser_open` result (Chrome's own background request, blocked by the proxy: the policy works) and the model repeated it in its summary. Disable Chrome's background networking (launch flags) so the notice means a page request.
5. **Snapshot verbosity:** the snapshot carries `InlineTextBox` lines under every text node; it eats the 40,000-character budget for nothing.
6. **Cost reporting:** a stopped run records no usage and no cost (`cost n/a`); a GLM run's cost shows "partial". The budget chip handled both without error.
7. **Primary action styling:** "Allow once" is drawn as the recommended (green) action on a security card.
8. **Timeline for GLM:** tool rows show the generic "other" kind for the browser tools (Claude's show the globe icon).
9. **Message prefix:** a refusal reads "browser_open: browser: address <tunnel address> is not allowed ...": the repeated "browser" label (the same one the Settings UI now strips) is also what the agent sees.
10. **Not verified here:** a non-loopback allowed host loading; Linux and Windows runs.

## Spend log

| Time | Run | Task id | Cost reported | Running total |
|---|---|---|---|---|
