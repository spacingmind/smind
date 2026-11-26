# Human verification backlog

Verifications that the plans in `docs/plans/completed/` never got (a real window, a real device, an Apple or Windows machine, a live model, a manual pass). The plans moved because their acceptance criteria are validated; these items were stated there as not verified and need a person or a machine the agents do not have. Remove a row when it is done and say where the evidence is.

Open: every row below.

| Plan (in completed/) | Not verified (from the plan, shortened) | Kind | Who can verify |
| --- | --- | --- | --- |
| `acp-session-load.md` | What a given agent does on `session/load` beyond replaying is the agent's own and is not verified here | live model | a live run with each provider's real agent |
| `browser-claude-preallow.md` | Live Haiku check: not verified | live model | Human with a paid live run on the real CLI |
| `browser-claude-preallow.md` | user-scope and local-scope clashes (a scratch `CLAUDE_CONFIG_DIR` has no login) | live account | Human with the CLI's real login state |
| `browser-for-agents-designs.md` | Linux and Windows Chrome were not measured | other | Linux machine; Windows machine with Chrome |
| `browser-for-agents-designs.md` | the Windows pipe/Job Object path is unchanged and still unrun; none of the Windows code has been run | Windows | Windows machine |
| `browser-for-agents-designs.md` | a real daemon end to end (nothing can reach the tools while dark) | manual | Human with a real daemon, the tools left dark |
| `browser-for-agents-designs.md` | snapshot password detection covers `input[type=password]` found through the DOM, not a password field inside a shadow root or an iframe of another origin | other | a real page with a shadow-root or cross-origin password field |
| `browser-for-agents-designs.md` | that every Chrome version raises `Fetch.authRequired` for a CONNECT 407 (measured on the installed Chrome only) | other | machines with other Chrome versions |
| `browser-for-agents-designs.md` | the macOS `LOCAL_PEERPID` semantics for a descriptor passed this way were not tested | manual | a crafted macOS run with a passed descriptor |
| `browser-for-agents-headless.md` | the pipe handle inheritance (`--remote-debugging-io-pipes`) and the Job Object kill are written but not run on a real Windows machine | Windows | Windows machine |
| `browser-for-agents-live-findings.md` | NOT verified on a device or simulator (no native build in this item); the mobile layout on a device | device | a device or simulator |
| `browser-for-agents-live-findings.md` | a run with a real model | live model | Human with a paid live run |
| `browser-for-agents-live-run.md` | Codex is not run: it takes no per-run MCP servers, the reach table gives it no row, and a live Codex turn costs real money | live model | Human with a paid live turn |
| `browser-for-agents-live-run.md` | the three-denials limiter, the `(path cut)` marker | manual | a live run that trips the three-denials limiter |
| `browser-for-agents-live-run.md` | an allowed non-loopback page actually loading is NOT verified | manual | Human with the firewall open to a LAN host |
| `browser-for-agents-live-run.md` | Linux and Windows runs | other | Linux machine; Windows machine |
| `browser-for-agents-pr3.md` | a real daemon's relay path for the three methods (the E2EE channel follows the saved-grants precedent but was not exercised) | manual | the desktop relay against a real daemon |
| `browser-for-agents-pr3b2.md` | its first real run is the Windows CI job plus a manual run the Human must do | Windows | Windows machine (the CI job or the Human's run) |
| `browser-for-agents-pr4.md` | Windows (not offered) | Windows | Windows machine |
| `browser-for-agents-pr5.md` | a live run with a real model | live model | Human with a paid live run |
| `browser-for-agents-pr5.md` | Windows CI | Windows | the Windows CI job |
| `browser-for-agents-pr6.md` | a real DNS server or a fleet link over a real name (lookups are faked) | other | a real DNS server or a live fleet link |
| `browser-for-agents-pr6.md` | Windows CI | Windows | the Windows CI job |
| `browser-for-agents-pr6.md` | a live-model run | live model | Human with a paid live run |
| `browser-for-agents-settings-ui.md` | a run really using the browser (the live state is a stubbed view; the Go field is unit-tested) | live model | a live run with the browser on, watched in Settings |
| `browser-for-agents-settings-ui.md` | the desktop shell and the relay-refusal path in a browser | manual | the desktop shell, or the relay in a browser |
| `browser-for-agents-settings-ui.md` | Windows | Windows | Windows machine |
| `crash-capture.md` | a real daemon crash end to end (no live panic was provoked in a running daemon) | manual | a real crash provoked in a running daemon |
| `crash-capture.md` | Windows file-permission behaviour (platform default ACL, not asserted) | Windows | Windows machine |
| `crash-capture-desktop.md` | anything on Windows (the `cfg(windows)` reparse-point branch and the `USERPROFILE`/verbatim-path handling are read and unit-tested only as pure functions | Windows | Windows machine |
| `crash-capture-desktop.md` | a real crash of the running app (no live panic was provoked in the Tauri app) | manual | a real crash provoked in the desktop app |
| `crash-capture-desktop.md` | a UNC or non-`Users` profile directory (documented gap); a hung disk during a report (documented) | other | a machine with a UNC profile path or a hung disk |
| `daemon-memory.md` | claim is from the synthetic database, not the Human's own | manual | the real running daemon |
| `desktop-attention.md` | a real OS notification and a real click (no real-window run); macOS's own permission prompt; a real window and a real OS banner (focus, typing, coalescing end to end) | real window | Human with an unlocked screen and notifications on |
| `desktop-attention.md` | Windows and Linux click handlers (the same `navigate` path, compiled for macOS only); the Windows/Linux paths (macOS compiled only) | other | Windows machine; Linux machine |
| `desktop-attention.md` | the scheduled sweeps and the command wrappers against a real app handle (the rules they call are unit-tested; the wrappers are thin) | real window | Human with the app running |
| `desktop-audit-app-guard.md` | Windows (reasoned in the PR body; CI is the only Windows check); Linux | other | Windows machine; Linux machine |
| `desktop-audit-app-guard.md` | an end-to-end run of a rebuilt audit app (waits for AC6) | manual | a rebuilt audit app, run end to end |
| `desktop-audit-build.md` | an Intel host; CI does not run the build (only the dry-run test and shellcheck) | other | an Intel Mac |
| `desktop-audit-guard-wiring.md` | an Intel host; Windows and Linux for the build step (macOS only; the dry-run test runs anywhere with `jq`) | other | an Intel Mac; Windows machine; Linux machine |
| `desktop-generated-schemas.md` | a Windows or Linux build from a clean tree (reasoned from the same `tauri-build` behaviour and from the Tauri template, which ignores the same path) | other | a Windows or Linux clean build |
| `desktop-multi-window.md` | everything on a real window (screen locked): that the glob matches the pool labels at runtime, the overlay title bar and vibrancy on a second window, the first-paint show, destroy-on-close, focus | real window | Human with an unlocked screen |
| `desktop-multi-window.md` | Windows and Linux builds | other | Windows machine; Linux machine |
| `desktop-multi-window.md` | macOS window tabbing (probe after Part 1, real window) | real window | Human with an unlocked screen |
| `desktop-notarization-prep.md` | anything against Apple (no account); Tauri's own signing path (option A of the ADR); how Apple treats empty entitlements, the sidecar as nested code | Apple account | Apple developer account (Developer ID signing, notarization) |
| `desktop-notarization-prep.md` | the first update from an ad-hoc to a Developer ID app | Apple account | Apple developer account (a signed first update) |
| `desktop-notarization-prep.md` | the workflow has never run (it is disabled, and the Environment policy cannot be tested from here); the provenance and release checks have never run against a real GitHub release in the job | manual | Human dispatching the workflow with the Environment set |
| `desktop-notarization-prep.md` | the CODEOWNERS and Environment settings are the Human's and cannot be checked from here; the first signed run's baseline commit does not exist yet | manual | Human checking the repo settings; the first signed run |
| `desktop-release-hygiene.md` | the dmg internals and the Windows installer payload (neither can be opened portably in CI; the app tarball is the same bundle the dmg wraps) | other | macOS for the dmg; a Windows machine for the installer |
| `desktop-release-hygiene.md` | a real release run (the workflow step runs only on `develop` pushes and dry-run dispatches; no dispatch was made from this branch); the wait paths against a really running CI | other | the next release on develop, or a dispatched dry run |
| `desktop-require-signed-version.md` | a real in-app update from a published release to the next with the flag on (needs the next release; two flag-off releases exist) | other | a second release, with the flag on |
| `desktop-require-signed-version.md` | the Windows updater path (the hermetic e2e is macOS and Windows in CI, run locally on macOS only) | Windows | Windows machine (or the Windows CI e2e) |
| `desktop-tab-reorder.md` | real-window frames (screen locked) | real window | Human with an unlocked screen |
| `desktop-tab-reorder.md` | touch drag | device | a touchscreen or trackpad |
| `desktop-tab-reorder.md` | Windows and Linux | other | Windows machine; Linux machine |
| `desktop-tabs.md` | real-window frames (screen locked) | real window | Human with an unlocked screen |
| `desktop-tabs.md` | Windows/Linux | other | Windows machine; Linux machine |
| `desktop-tabs.md` | multi-window (a second window shares the same `localStorage`, so it follows the same map, untested) | real window | Human with an unlocked screen (two windows) |
| `dt3-terminal-workbench.md` | a real daemon, the desktop webview, `file_ref` against a real daemon; wrapped long lines | manual | the desktop shell against a real daemon |
| `e9-plan-hygiene.md` | the PR-merge claims of the 14 Closed lines rest on `git log origin/develop --grep '(#N)'` plus the plans' own text, not on re-running their tests | manual | Human or CI re-running the referenced tests |
| `first-run-get-started.md` | a real daemon (demo scenario with scripted provider health only) | manual | a real daemon with real provider health |
| `first-run-get-started.md` | the Windows desktop shell | Windows | Windows machine |
| `home-and-sidebar-ui.md` | a real skeleton run with a genuinely large tool result (the marker is injected into the attach result) | manual | a real run with a large tool result |
| `home-and-sidebar-ui.md` | the thinking-only copy in a frame; real-window desktop; long-title rows with a diffstat in frames (unit-tested) | real window | Human with an unlocked screen |
| `never-list-floor.md` | Windows behaviour on a real Windows host (fake-platform tests and `GOOS=windows go vet` only) | Windows | Windows machine |
| `plugins-pa5-mobile.md` | a native iOS/Android device | device | a native iOS or Android device |
| `pr-panel.md` | the real-GitHub check of `task.prDetails` named in the status line was never run (unverified by decision, scripted `gh` only) | live account | a real repo on GitHub |
| `public-docs.md` | A statement that could not be checked is left out, or marked `<!-- unverified -->` and listed in the PR body | manual | Human reading the PR bodies' unverified lists |
| `tree-budget.md` | a real model run crossing the cap (no model call made) | live model | a paid model run crossing the cap |
| `ux-audit-small-fixes-c.md` | sending the attachment through a real daemon (the move is tested with a stubbed `chat.list`; the task composer's send of those items is the existing path) | manual | an attachment sent through a real daemon |
| `ux-audit-small-fixes-c.md` | a long model name at 390; the disabled and attachment-error states in frames; transition frames | real window | Human with an unlocked screen |
| `ux-audit-small-fixes-d.md` | the mobile field (no device or simulator: logic tested, layout not seen) | device | a device or simulator |
| `ux-audit-small-fixes-d.md` | a real daemon send with a multi-line reason | manual | a real daemon send |
| `ux-audit-small-fixes-d.md` | the field disabled mid-response in frames; transition frames | real window | Human with an unlocked screen |
| `web-demo-mode.md` | the workflow's first real run is the Human's manual dispatch | live account | Human with the deploy token, dispatching the workflow |
