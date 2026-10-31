---
v: 1
milestone: "Office mode O2: concise tool cards, office greeting, suggested prompts, interface-mode store"
base: origin/develop
planRef: docs/plans/active/office-mode.md
acs:
  - id: AC8
    text: "In office mode a terminal- or edit-intent tool card shows one plain line and a status (running/done/failed) and no command text, output, diff, +/- counts or expand control; a failed card adds the one-line error summary; the file link still opens the file. In coding mode the same fixtures render as before."
    covers: { tests: [terminal_and_edit_hide_command_output_diff, failed_shows_error_summary, coding_mode_dom_unchanged], frames: [] }
  - id: AC9
    text: "Read/search/fetch/MCP/generic cards and permission rows render identically in both modes; the DetailLevel grouping still works in office mode."
    covers: { tests: [read_search_fetch_unchanged, permission_and_error_cards_never_simplified], frames: [] }
  - id: AC10
    text: "The greeting hook picks from OFFICE_GREETINGS (at least 3 own-copy lines per bucket) in office mode and from today's pool in coding mode; the line changes only when the bucket or the mode changes."
    covers: { tests: [office_pool_per_bucket], frames: [] }
  - id: AC11
    text: "Office mode shows exactly four suggestion chips on the empty home; clicking one fills the composer and sends nothing; coding mode shows none (none existed)."
    covers: { tests: [four_chips_fill_never_send], frames: [] }
  - id: S2
    text: "The mode is stored as the bare string office under smind:interface-mode; anything else, a corrupt value or a throwing storage reads coding; a write never throws; another tab's change is followed; the mode is never read by a transport or call-site module."
    covers: { tests: [reads_only_office, corrupt_and_throwing_storage_fall_back, write_never_throws, storage_event_from_other_tab, interface_mode_never_on_the_wire], frames: [] }
tests:
  - { name: terminal_and_edit_hide_command_output_diff, run: "cd web/packages/ui && bunx vitest run src/components/timeline/tool-card-concise.test.tsx -t terminal_and_edit_hide_command_output_diff", timeoutSec: 900 }
  - { name: failed_shows_error_summary, run: "cd web/packages/ui && bunx vitest run src/components/timeline/tool-card-concise.test.tsx -t failed_shows_error_summary", timeoutSec: 900 }
  - { name: coding_mode_dom_unchanged, run: "cd web/packages/ui && bunx vitest run src/components/timeline/tool-card-concise.test.tsx -t coding_mode_dom_unchanged", timeoutSec: 900 }
  - { name: read_search_fetch_unchanged, run: "cd web/packages/ui && bunx vitest run src/components/timeline/tool-card-concise.test.tsx -t read_search_fetch_unchanged", timeoutSec: 900 }
  - { name: permission_and_error_cards_never_simplified, run: "cd web/packages/ui && bunx vitest run src/components/timeline/tool-card-concise.test.tsx -t permission_and_error_cards_never_simplified", timeoutSec: 900 }
  - { name: office_pool_per_bucket, run: "cd web/packages/ui && bunx vitest run src/lib/greeting.office.test.ts -t office_pool_per_bucket", timeoutSec: 900 }
  - { name: four_chips_fill_never_send, run: "cd web/packages/ui && bunx vitest run src/components/home/home-landing.test.tsx -t four_chips_fill_never_send", timeoutSec: 900 }
  - { name: reads_only_office, run: "cd web/packages/ui && bunx vitest run src/lib/interface-mode.test.ts -t reads_only_office", timeoutSec: 900 }
  - { name: corrupt_and_throwing_storage_fall_back, run: "cd web/packages/ui && bunx vitest run src/lib/interface-mode.test.ts -t corrupt_and_throwing_storage_fall_back", timeoutSec: 900 }
  - { name: write_never_throws, run: "cd web/packages/ui && bunx vitest run src/lib/interface-mode.test.ts -t write_never_throws", timeoutSec: 900 }
  - { name: storage_event_from_other_tab, run: "cd web/packages/ui && bunx vitest run src/lib/interface-mode.test.ts -t storage_event_from_other_tab", timeoutSec: 900 }
  - { name: interface_mode_never_on_the_wire, run: "cd web/packages/ui && bunx vitest run src/lib/interface-mode.wire.test.ts -t interface_mode_never_on_the_wire", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/timeline/tool-call.tsx, change: "the concise branch is never taken", expectFail: [terminal_and_edit_hide_command_output_diff, failed_shows_error_summary] }
  - { id: M2, file: web/packages/ui/src/components/timeline/tool-call.tsx, change: "the concise branch ignores the mode (applies in coding too)", expectFail: [coding_mode_dom_unchanged] }
  - { id: M3, file: web/packages/ui/src/components/timeline/concise-tool-card.tsx, change: "every non-generic intent is concise", expectFail: [read_search_fetch_unchanged] }
  - { id: M4, file: web/packages/ui/src/components/timeline/concise-tool-card.tsx, change: "the concise title also renders the command text", expectFail: [terminal_and_edit_hide_command_output_diff] }
  - { id: M5, file: web/packages/ui/src/components/timeline/concise-tool-card.tsx, change: "a failed card shows no error summary", expectFail: [failed_shows_error_summary] }
  - { id: M6, file: web/packages/ui/src/lib/interface-mode.ts, change: "any stored string reads as office", expectFail: [reads_only_office] }
  - { id: M7, file: web/packages/ui/src/lib/interface-mode.ts, change: "a throwing storage reads as office", expectFail: [corrupt_and_throwing_storage_fall_back] }
  - { id: M8, file: web/packages/ui/src/hooks/use-greeting.ts, change: "office mode keeps the coding pool", expectFail: ["office mode picks from the office pool"] }
  - { id: M9, file: web/packages/ui/src/hooks/use-greeting.ts, change: "the line is not re-picked when only the mode changes", expectFail: ["office mode picks from the office pool"] }
  - { id: M10, file: web/packages/ui/src/lib/greeting.ts, change: "an office bucket drops to two lines", expectFail: [office_pool_per_bucket] }
  - { id: M11, file: web/packages/ui/src/components/home/home-landing.tsx, change: "a chip click also creates the task", expectFail: [four_chips_fill_never_send] }
  - { id: M12, file: web/packages/ui/src/components/home/home-landing.tsx, change: "chips show in coding mode too", expectFail: ["coding mode shows no suggestion chips"] }
  - { id: M13, file: web/packages/ui/src/lib/suggested-prompts.ts, change: "a fifth chip", expectFail: ["is exactly four distinct"] }
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 45 }
---

# Office mode O2 validation contract

Web only (`web/packages/ui`); no daemon, wire or desktop change. The mode is client-only: `interface_mode_never_on_the_wire` pins the set of source files that may read it.
The mutation `permission_and_error_cards_never_simplified` is a same-HTML comparison of the permission row across the two modes; the permission and error rows live in files O2 does not touch.
Frames are not validator frames: the real-app frames (both modes, light and dark, 1280 and 390, against an isolated daemon with a scripted run and the home screen) are captured by the Lead with `playwright-core`; their folder is named in the PR body.
`desktop-chrome.test.tsx` and `open-in-editor.test.tsx` can time out at their 5 s default on a loaded machine (cold `import("@/App")`); both pass with `--testTimeout=30000`.
