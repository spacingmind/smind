# UX audit small fixes, PR C: home composer Attach and effort, labelled chips at 390

Last of three PRs from the UX audit (A and B are in `ux-audit-small-fixes.md`). The home composer is a reduced copy of the task composer: it has the model and mode chips but no Attach and no effort chip, and at 390 the chips are bare icons.

## Acceptance criteria

1. **Effort chip on home.** The home composer shows the same effort (thinking-level) chip as the task composer (`RunConfigToolbar.Thinking`). The chosen level reaches the first run through the existing pending-send handoff; a provider with no levels shows no chip, as in the task composer.
2. **Attach on home.** When the daemon reports attachment support, the home composer shows the Attach control and the attachment tray (files and images; the `@` mention and forge pickers need a task and are not offered). Attachments picked on home are sent with the first prompt: after the task is created they move to that task's default chat draft and the task composer sends them through its ordinary submit. A failed move or send leaves the attachments in the task's tray and the prompt in its draft; nothing is lost silently. With no attachment support nothing is rendered.
3. **Labelled chips at 390.** Below the `sm` breakpoint the home composer's chips (model, effort, mode) show a text label each and wrap onto their own row above the send button; they are no longer bare icons. The task composer's chips are unchanged unless the same shared component change makes them labelled for free.
4. No new store or picker: the existing attachment store, `AttachMenu`, `AttachmentTray`, `RunConfigToolbar` pieces and pending-send store are reused (ADR-0032).

## Test scenarios (named)

- `home_effort_chip_renders_and_level_reaches_pending_send`
- `home_effort_chip_hidden_when_provider_has_no_levels`
- `home_attach_hidden_without_daemon_support`
- `home_attachments_move_to_the_new_tasks_draft_key`
- `attachment_store_move_transfers_items_and_clears_source`
- `home_chip_labels_visible_below_sm` (class/DOM level; frames cover the pixels)
- existing home-landing and composer tests unchanged and green

## Decisions

- No `@` mention or forge attach on home: they need a task and its worktree.
- The move targets the task's default (oldest non-archived) chat, read with `chat.list` after `task.create`.

## Validation

- Web: full vitest (451 files, 5098 tests, includes `src/demo`), `tsc -b` and the UI build pass. All named scenarios exist; mutations on committed code: dropping the draft `move` fails `home_attachments_move_to_the_new_tasks_draft_key`; dropping the effort chip fails `home_effort_chip_renders_and_level_reaches_pending_send` and `home_chip_labels_visible_below_sm`.
- Frames (isolated daemon, web 1280 and 390, light and dark, luminance and no horizontal overflow asserted): home default, home with an attached file and a typed prompt. Chips read "Claude Code", "Standard", "Accept File Edits" at 390 and wrap onto their own row above attach and send.
- Not verified: sending the attachment through a real daemon (the move is tested with a stubbed `chat.list`; the task composer's send of those items is the existing path); a long model name at 390; the disabled and attachment-error states in frames; transition frames.
