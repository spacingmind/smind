# Public docs: user documentation in `public-docs/`

Status: active (2026-10-25).

Size: L (6) — scope 1 (docs plus one Go test), PRs 2 (several, in sequence), risk 0 (docs and a test), decisions 1 (per-section homelab/README split, below), verification 2 (every statement checked against the binary and the real UI).

`public-docs/` holds the documentation a user reads: install, first task, providers, git, mobile, orchestration, CLI, configuration. `docs/` stays internal (ADRs, plans, the workspace protocol). The split follows `refs/paseo` (`public-docs/` for users, `docs/` for contributors). Plain Markdown with a small front matter; no site generator and no hosting (a later Human infrastructure decision).

## Pages

| order | file | content |
|---|---|---|
| 1 | `index.md` | what smind is, what to read next |
| 2 | `getting-started.md` | install the CLI, desktop app (macOS quarantine note), mobile app; start the daemon; first workspace and task |
| 3 | `providers.md` | claude-native, codex-native, glm, kimi: login, config dir, launch env (Settings → Providers); marked placeholder "Provider instances (coming)" |
| 4 | `workspaces-and-tasks.md` | workspaces, tasks, chat, attachments, queue / when-busy, Continue (only if on `develop`), spaces, pin, archive |
| 5 | `git-and-prs.md` | base ref, publish / push / update from base, Create PR dialog, PR panel, archive / restore, stacked sub-tasks, commit bar "Draft with agent" |
| 6 | `mobile-and-relay.md` | pairing, E2EE in user words, push notifications, read-only task tree |
| 7 | `orchestration.md` | Lead and Peer seats (role pack), briefing a Lead, tree budget, stalled-tree alert, git permission ask rules, per-tree concurrency guard |
| 8 | `usage-and-limits.md` | Settings → Usage, "≈ API price", plan windows, rate-limit wait / failover |
| 9 | `cli-reference.md` | generated from `smind --help` and `smind relay --help`, plus hand-listed commands the summary leaves out |
| 10 | `troubleshooting.md` | daemon not running, port in use, login, reconnect / resync, logs, `smind doctor` only if it exists (it does not on 2026-10-25) |
| 11 | `configuration.md` | user-facing `config.yaml` keys and defaults, checked in `internal/config` |
| 12 | `plugins.md` | stub: plugins are planned; link to the decision record's "Further reading" |
| 13 | `running-the-daemon.md` | added with the README move: service, direct and relay access overview, Docker, update, manual release install, backup |
| 14 | `scripting-and-permissions.md` | added with the README move: scripting `task send`, start points, permission modes, MCP server, worktree setup script |

## Acceptance criteria

AC1 `public-docs/` has the 14 pages above, each with front matter `title`, `description`, `order` (unique positive integer).
AC2 Every command, flag, config key, setting name and UI label in a page was checked against the current binary, code or a real UI on an isolated daemon (own `SMIND_HOME` and port, no live model sends). A statement that could not be checked is left out, or marked `<!-- unverified -->` and listed in the PR body.
AC3 No internal text in a page: no decision-record, PR or issue numbers, no plan names, no secrets, no personal paths, no internal hostnames. A "Further reading" section may link into `docs/decisions/`.
AC4 `task test` fails when the docs drift (named scenarios below).
AC5 `public-docs/cli-reference.md` summary blocks are generated (`task docs:cli`) and the test fails when they differ from `smind --help` / `smind relay --help`.
AC6 `README.md` is short (what smind is, install one-liner, link to `public-docs/`, contributing, license); user-facing sections moved to pages; `docs/homelab.md` keeps operator-only parts and says so at its top; links elsewhere to moved README anchors are fixed.
AC7 `plugins.md` promises nothing beyond "planned"; `providers.md` describes provider instances only as "coming".
AC8 Each PR body carries a per-page "verified against" table and the path of the verification log.

## Test scenarios (drift check, `cmd/smind/docs_drift_test.go`)

The test lives in `package main` so it can call `printUsage`/`printRelayUsage` and read the dispatch code (`run` and the `cmd*` functions) with `go/parser`: no network, no daemon, no built binary.

| test | rule | mutation that must fail it |
|---|---|---|
| `TestDocsLinksResolve` | every relative link in `public-docs/*.md` and `README.md` resolves to a file or directory; a `#fragment` into a `.md` file must match a heading | link `getting-started.md` → `missing-page.md`; link `providers.md#no-such-heading` |
| `TestDocsFrontMatter` | each page has `title`, `description`, positive unique `order`; `index.md` exists | delete the front matter of a page |
| `TestDocsCommandsExist` | every `smind <cmd> [<sub> [<sub>]]` in inline code or a code fence (not in a ```` ```text ```` fence) exists in the command tree; help lines in the generated blocks too; every top-level command appears on the CLI page | document `smind nosuchcmd`; document `smind task nosuchsub`; rename `case "push"` in `run` |
| `TestCommandProblemRules` | unit check of the command-tree matcher (accepts real, rejects fake) | — |
| `TestDocsForbiddenStrings` | no `/Users/`, `sk-` word start, `ADR-`, `PR #`, `#NNN` outside a "Further reading" section | add each string to a page |
| `TestCLIReferenceMatchesHelp` | generated blocks equal `printUsage` / `printRelayUsage` output | change one help line without `task docs:cli` |

Limits, stated on purpose: flags and arguments are not checked (only command words); the hand-listed commands on the CLI page copy the binary's own usage lines and are reviewed by hand.

## Decisions

1. **Front matter**: `title`, `description`, `order`. No category or nav key; the page list is flat.
2. **File names**: lowercase, hyphenated, flat directory (`public-docs/<slug>.md`); links are relative and extension-bearing so they work on GitHub and in a future generator.
3. **Order**: reading order for a new user: orientation, install, providers, daily work, git, mobile, orchestration, usage, then reference pages.
4. **Command extraction** reads the dispatch code with `go/parser` rather than running help: `smind <cmd> --help` is inconsistent across commands and some invocations would reach a live daemon.
5. **```` ```text ```` fences are program output**, not commands; hand-written command lists use ```` ```sh ````.
6. **README** joins the forbidden-string rule in the README PR (`readmeForbidden` in the test), because today's README still carries decision-record links.
7. **README and homelab.md** (decided in the README PR). README sections: Features, Install, Quickstart, Docker, manual release install, Updating, scripting, starting point, permission modes, MCP, child updates, seats, skills: all moved to pages (13 and 14 added because no planned page fitted service/Docker/update and scripting/permissions; orchestration owns seats, child updates, skills); Dev quickstart, task-worktree dependencies and the desktop-updater developer notes moved to `CONTRIBUTING.md`. `docs/homelab.md` per section: service units, threat model ("What is protected"), relay hosting, Docker details, backups and WAL notes STAY (operator material; the new pages carry the short user how-to and link here); pairing and push user flows are summarised in `mobile-and-relay.md`. A note at the top of homelab.md says so.
8. **No new CLI help text**: the summary page documents what `smind --help` prints today, even where it lags the dispatch table; gaps are listed on the CLI page and reported to the Human.

## PR sequence

PR1 plan, skeleton pages, drift check, CLI generator. PR2, PR3 page batches (at most four pages each, by two parallel authors). PR4 remaining pages, README and homelab move. Every PR targets `develop`; later PRs contain PR1 until it merges and need a rebase afterwards.

## Progress

- 2026-10-25 PR1 (#621): plan, 12 pages (11 stubs with verified outlines, CLI reference complete), drift check with six mutations run, `task docs:cli`.

## Validation

PR1 mutation results: `/tmp/pd/mutations.log`.
- 2026-10-25 PR2 (batch A: getting-started, providers, troubleshooting, configuration), PR3 (batch B: workspaces-and-tasks, git-and-prs, usage-and-limits), PR4 (mobile-and-relay, orchestration, index, pages 13 and 14, README and homelab move; `readmeForbidden` on).
