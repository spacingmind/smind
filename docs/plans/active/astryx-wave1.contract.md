---
v: 1
milestone: "ADR-0064 Wave 1, item 1: the Add project primary button returns to its before look"
base: origin/wave/astryx
planRef: docs/plans/active/astryx-wave1.md
acs:
  - id: AC1
    text: "With an empty path the Add project submit button is enabled and solid (light and dark, 1280 and 390)."
    covers: { tests: [W1-01-submit-solid], frames: [] }
  - id: AC2
    text: "No focusout reaches the path field between open and settle."
    covers: { tests: [W1-02-no-blur], frames: [] }
  - id: AC3
    text: "Input autoFocus: outside a dialog focuses on mount; inside a dialog waits for the dialog."
    covers: { tests: [input_autofocus_inside_a_dialog_is_not_focused_at_mount_and_is_marked_for_the_dialog, input_autofocus_outside_a_dialog_is_focused_on_mount], frames: [] }
  - id: AC4
    text: "The existing Add project and unit suites still pass."
    covers: { tests: [], frames: [], commands: [C1, C2, C3] }
tests:
  - { name: W1-01-submit-solid, run: "cd web/packages/ui && bun run build && cd ../../.. && go build -o bin/smind ./cmd/smind && cd web/e2e && E2E_FULL=1 PW_CHROMIUM_PATH=$HOME/Library/Caches/ms-playwright/chromium-1148/chrome-mac/Chromium.app/Contents/MacOS/Chromium bunx playwright test specs/astryx-w1-add-project-button.spec.ts -g W1-01", timeoutSec: 1200 }
  - { name: W1-02-no-blur, run: "cd web/e2e && E2E_FULL=1 PW_CHROMIUM_PATH=$HOME/Library/Caches/ms-playwright/chromium-1148/chrome-mac/Chromium.app/Contents/MacOS/Chromium bunx playwright test specs/astryx-w1-add-project-button.spec.ts -g W1-02", timeoutSec: 900 }
  - { name: input_autofocus_inside_a_dialog_is_not_focused_at_mount_and_is_marked_for_the_dialog, run: "cd web/packages/ui && bunx vitest run src/components/ui/input.test.tsx -t input_autofocus_inside_a_dialog_is_not_focused_at_mount_and_is_marked_for_the_dialog", timeoutSec: 900 }
  - { name: input_autofocus_outside_a_dialog_is_focused_on_mount, run: "cd web/packages/ui && bunx vitest run src/components/ui/input.test.tsx -t input_autofocus_outside_a_dialog_is_focused_on_mount", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/ui/input.tsx, change: "el.focus() runs unconditionally at mount (also inside a dialog)", expectFail: [input_autofocus_inside_a_dialog_is_not_focused_at_mount_and_is_marked_for_the_dialog] }
  - { id: M2, file: web/packages/ui/src/components/ui/input.tsx, change: "hasAutoFocus is passed to the Astryx TextInput again", expectFail: [W1-02-no-blur] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
  - { id: C3, run: "go test ./internal/hygiene/ -count=1", timeoutSec: 900 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# Wave 1 item 1 validation contract

Spec: `docs/plans/active/astryx-wave1.md`. Frames (light and dark, before and after) are captured by the Lead; the validator checks tests, mutations and commands.
