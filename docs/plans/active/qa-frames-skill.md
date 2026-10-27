# QA frame harness as the validator's bundled skill (B5)

Status: active (2026-10-27). Binding text: [`docs/decisions/0045-validator-and-inbox.md`](../../decisions/0045-validator-and-inbox.md) §7 and question 11 ("its own later item", Human pre-approved). Related plan: [`validator-inbox.md`](validator-inbox.md).

Size: M (5) — scope 1, PRs 1, risk 1 (processes, browser, an LLM-send guard), decisions 0 (ADR accepted; entry-point choice below), verification 2 (real browser, frames). One Lead, solo.

## Why

Today a contract with `frames` gets `blocked: no_browser` from the validator. The Lead's by-hand Playwright scripts (isolated daemon, light/dark, 1280/390, transition frames, contact sheets) are the reference. This item productizes them so the validator can *run* them and attach an evidence report; where no browser exists the verdict stays `blocked: no_browser` exactly as today.

## Decisions

- **Entry point: a dev tool main, `go run ./internal/qaframes/cmd/qaframes`, not a new `smind` subcommand and not a shell script.** A subcommand widens the public CLI (usage, `docs:cli`, drift test, release notes) for a tool only the validator uses; a shell script cannot carry the unit tests the spec names (fakes for daemon, browser, signals). A Go package with a thin main follows `internal/relay/bridge/devharness` (a helper, not part of smind's CLI). The validator always works in a repo checkout, so `go run` is available. No new Go dependency, no binary in the repo.
- **Browser driving: an embedded Node driver over `playwright-core`/`playwright`, found, never installed.** Detection looks for `node`, a resolvable playwright module (`--playwright-dir`, `SMIND_QA_PLAYWRIGHT_DIR`, `web/node_modules`, `./node_modules`) and a Chromium/Chrome executable (`--chrome`, `SMIND_QA_CHROME`, the `ms-playwright` cache, system Chrome). Any missing piece is the typed result `no_browser` (exit code 3, report written, never a silent pass).
- **UI bundle:** in a checkout the tool runs `bun run --filter @smind/ui build` (into the gitignored `internal/server/dist`) and `go build`s a private `smind` into the run's temp dir; `--bin` uses a given binary instead. A binary whose `/` is still the placeholder is reported `blocked: no_toolchain`.
- **Process safety:** every child (daemon, node driver, browser PID reported by the driver, fake agent build) is started in its own process group and recorded; teardown (normal, error, ctx cancel, SIGINT/SIGTERM) kills only recorded PIDs/groups, then removes the temp home. No pkill.
- **Isolation:** `SMIND_HOME` and `HOME` are a fresh temp dir, the port comes from `:0`; the tool refuses a home equal to `~/.smind` / `$SMIND_HOME` of the caller and ports 4648 and 4870.
- **Content, no live LLM:** `--fake-agent` builds `internal/taskrunner/fakeagent` and sets `SMIND_ACP_COMMAND`; `--seed file.json` runs `[{method, params}]` RPCs against the isolated daemon.
- **Scripted sends are OFF by default.** A scenario step `send` is refused at plan time unless `--allow-send` is given and `--model` is exactly `claude-haiku-5-5`; otherwise the run exits 2 before starting any process.
- **Verdict wiring unchanged on the daemon side.** The only daemon-side change is the role pack: the `validator` seat lists the skill `smind-validator-frames` (delivered to that seat only, like `smind-leader` is to the Lead). Frame status in the report is `captured` / `blocked`; *looking* and the observation stay the validator's job, so a pass still needs a passing evidenced item per frame.
- **Platforms:** Unix-first (process groups). It builds and vets for windows (recorded-PID `taskkill` fallback, untested there); the report says `platform`.

## Acceptance criteria

- AC1 Diff mapping: changed `web/packages/ui/src/**` files map through a table to route targets; a file with no mapping is listed as `unmapped` in the plan and the report, never dropped; test files are listed `ignored: test file`.
- AC2 No browser/node/playwright/UI bundle → typed result (`no_browser` / `no_toolchain`), report written with every contract frame `blocked`, exit 3.
- AC3 The run uses a temp `SMIND_HOME`/`HOME` and a free port; refuses the real home and 4648/4870.
- AC4 Self-cleaning: on normal end, error, ctx cancel and SIGINT/SIGTERM every recorded PID (and its group) is dead and the temp dir is gone.
- AC5 Sends are off by default and refused for any model but `claude-haiku-5-5`.
- AC6 Contact sheets: ≤ 12 frames per sheet, each cell timestamped (ms since the step began), sheet manifest in the report.
- AC7 Report (json + markdown) in the evidence dir (`.validation-evidence/qa-frames/` by default): every contract frame maps to files, viewport, theme, steps, status, notes; an unmappable surface is `blocked` with a note.
- AC8 Skill `smind-validator-frames` is bundled, delivered to the `validator` seat only, and tells the validator when to run the tool and how to cite the report; no frames/no browser → `blocked: no_browser` as today.
- AC9 Real e2e: light+dark × 1280/390 captured on an isolated daemon, where a browser exists.

## Named tests

`TestFramesPlan_DiffToScreens_MappedAndUnmapped`, `TestFramesRun_NoBrowser_TypedBlocked`, `TestFramesRun_IsolatedHome_NeverTouchesDefaultHomeOrPorts`, `TestFramesRun_SelfCleaning_NoOrphans`, `TestFramesRun_ScriptedSend_RefusesNonHaiku_OffByDefault`, `TestFramesSheet_AtMost12PerSheet_Timestamped`, `TestFramesReport_MapsEveryContractFrame`, `TestSkillDelivery_ValidatorOnly`, `TestValidator_FramesItemBlockedNoBrowserUnchanged`, `TestFramesRun_E2E_RealBrowser` (skips with a message when there is no browser).

Mutations (each must fail its test): real home used; send allowed with non-haiku; orphan left on SIGTERM; sheet over 12; unmapped diff silently skipped; skill delivered to a non-validator seat.

## PR split

1. **PR0 (this file)** — docs only, `docs/plans/active/qa-frames-skill.md`.
2. **PR1** — `internal/qaframes/**` (package, driver, cmd), `internal/skills/bundle/smind-validator-frames/**`, `internal/profiles/rolepack*.go` (validator `Skills`), `public-docs/orchestration.md`, this plan's Progress/Validation. Write scopes do not overlap PR0 except this file.

## Progress

- [ ] PR0 plan
- [ ] PR1 implementation

## Validation

(filled in with PR1)
