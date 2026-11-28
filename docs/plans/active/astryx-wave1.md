# Astryx Wave 1

Size: L (6) — scope 2, PRs 2, risk 1, decisions 0, verification 1 (frames)
Decision record: [ADR-0064](../../decisions/0064-migrate-web-and-desktop-ui-to-astryx.md). All Wave PRs target `wave/astryx`; the final PR is `wave/astryx` to `develop`.

Wave 1 brings the remaining form primitives onto Astryx and moves Input and Dialog call sites to direct use. It starts with the one open Wave 0 defect.

## W1-1: the Add project primary button returns to its "before" look

**Cause** (measured with a focus log in the e2e harness): the path field's React `autoFocus` (kept by the Astryx `TextInput`'s `hasAutoFocus`) focuses it at mount; opening the native `<dialog>` then moves focus to the first card; the field blurs; the call site's `onBlur` runs the folder check on an empty path ("empty" blocks submit); Astryx refocuses the field, too late.

**Acceptance criteria**
- AC1: with an empty path the submit button is enabled and solid (opacity 1, opaque fill) in light and dark, at 1280 and 390.
- AC2: no `focusout` reaches the path field between open and settle.
- AC3: `autoFocus` on an `Input` outside a dialog still focuses it on mount; inside a dialog the dialog focuses it after it opens.
- AC4: a light and a dark frame of the open dialog, before and after.

**Named tests**

| Test | File |
|---|---|
| `W1-01 the Add project submit is enabled and solid when the dialog opens with an empty path` | `web/e2e/specs/astryx-w1-add-project-button.spec.ts` |
| `W1-02 the path field keeps focus from open: no blur reaches the folder check` | same |
| `input_autofocus_inside_a_dialog_is_not_focused_at_mount_and_is_marked_for_the_dialog` | `web/packages/ui/src/components/ui/input.test.tsx` |
| `input_autofocus_outside_a_dialog_is_focused_on_mount` | same |

## Decisions

- Staffing (Human, 2026-11-29): Codex (`peer-codex`) and GLM (`peer-glm`) in parallel on independent steps, at most two; all to Codex while GLM is rate-limited; `peer-sonnet` only if both are out, with a logged reason; never `peer-opencode`.
- Step A (Input autofocus handoff) and step B (e2e spec and frames) both ran on `peer-codex`: GLM was rate-limited until 01:43.

## Progress

- W1-1: done. Frames: `/tmp/astryx-wave1/frames-before` and `frames-after`.

## Validation

(filled when the item is validated)
