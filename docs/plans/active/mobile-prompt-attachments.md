# Mobile prompt attachments

Size: L (6) — scope 1, PRs 1, risk 1, decisions 0, verify 2 (web smoke; device/simulator not available to this Lead).

Mobile half of [ADR-0038](../../decisions/0038-prompt-attachments.md) (web/desktop shipped; O1 = (b): the phone downscales to fit the
relay frame). Scope: `mobile/` plus docs. No daemon change: `run.start` already takes `attachments`, `run.list` rows carry
`attachments` metadata, `run.attachment` refuses what would not fit the connection, `daemon.info.attachments` exposes limits and `kinds`.
If a daemon change turns out to be needed, stop and tell the Supervisor.

## Acceptance criteria

- AC1. The task composer has an attach button opening an image picker and a document picker. The button shows only when
  `daemon.info.attachments.kinds` includes `image` and/or `file` (field absent on an older daemon = `image,file,file_ref`); the menu lists only the kinds the daemon accepts.
- AC2. Pending attachments show as a tray of pills/thumbnails above the input, each with a remove button, on the M1 theme and
  primitives (light and dark). The tray appears/disappears with no height animation.
- AC3. Images are downscaled on device to JPEG, long edge <= 1568 px, quality from 0.8 stepping down to a floor, so the encoded
  `run.start` request is <= 1,048,551 bytes; if the floor still does not fit, the image is refused with a clear message. HEIC and any non-png/jpeg/gif/webp
  image is re-encoded to JPEG through the same path. An image already <= 1568 px that fits and is png/jpeg/gif/webp is sent as is.
- AC4. A non-image file is sent as is; if the request would not fit, it is refused inline naming file and limit
  ("report.pdf is 2.4 MB; over the relay a prompt can carry about 780 KB"). Nothing is silently dropped or truncated.
- AC5. The size is checked BEFORE sending, on the exact wire message, `{id, method, params}` as RelayConnection.request sends it (prompt + existing attachments + the new one), both when picking and again at Send.
- AC6. At most 10 attachments; the 11th is refused with a message.
- AC7. Daemon errors `{code,message,index}` (attachment_too_large, too_many_attachments, unsupported_image_type, mime_mismatch,
  invalid_base64, plus anything else) show as inline text under the composer; a failed send keeps the draft and the tray.
- AC8. The timeline shows a sent prompt's attachments read-only under the user message: pills with name and size; image
  thumbnails fetched lazily with `run.attachment` when the metadata size makes the result fit the 1 MiB frame, else a pill.
  A failed fetch falls back to the pill.
- AC9. New native modules documented: iOS permission strings via config-plugin entries in `app.json`; "Rebuild the dev client" note in the PR body and in the mobile docs where dev-client builds are described.
- AC10. `mobile/app/new-task.tsx` and `mobile/src/newTask*` untouched. (Its first-prompt field is its own `ExpoTextInput` in `NewTaskScreen.tsx`, not the task `Composer`; it stays text-only and the PR says so.)

## Test scenarios (vitest, `mobile/src/__tests__/attachments.test.ts`, pure module `mobile/src/attachments.ts`)

1. `fits as is` — small png <= 1568 and in budget is returned unchanged (no manipulate call).
2. `oversized image is downscaled to <= 1568 and fits` — injected manipulator; result is image/jpeg, long edge <= 1568, request fits.
3. `downscale cannot fit is refused` — manipulator output never fits even at the quality floor -> refusal naming the file.
4. `heic is re-encoded to jpeg`.
5. `oversized file is refused with message` — message names file, its size and the ~780 KB limit; nothing added.
6. `eleventh attachment is refused`.
7. `size is rechecked at send` — an attachment that fit alone no longer fits after the draft text grows -> `checkSend` refuses.
8. `daemon error code maps to inline text` — each typed code, with index -> attachment name.
9. `older daemon without kinds hides affordance or limits it` — absent `kinds` = image+file; `kinds` without image/file = hidden.
10. `request size is exact` — `requestBytes` equals the UTF-8 byte length of the JSON, including multi-byte prompt text; `fits` measures the whole `{id, method, params}` envelope (`wireBytes`), so params exactly at the limit are refused and the image ladder steps down.
11. api: `startRunWhenBusy` and `startRun` send `attachments` only when non-empty (omitted otherwise); `fetchDaemonInfo` reads `attachments`; `fetchRunAttachment` wrapper.
12. UI-free view helpers: timeline attachment view (name + human size; thumbnail decision `canThumbnail(size)`).

Web-smoke frames (light and dark, ~390 px): composer empty; tray with image pill; tray with file pill; over-limit refusal; daemon error;
sent prompt's pills in the timeline; before/after adding and removing a pill.

## Plan

- Peer 1 (no UI): `attachments.ts` + tests, `api.ts` wiring, daemon.info capability, `app.json` plugins and deps.
- Peer 2 (after Peer 1 accepted): Composer tray + attach menu, send wiring, timeline pills/thumbnails, smoke stage and frames, docs note.

## Decisions

- Item 5: pills always; thumbnails when `run.attachment` fits (decided in the PR once built).
- Relay budget uses the exact `JSON.stringify` UTF-8 length of the whole request envelope (worst-case 10-digit id), not an estimate and not just the params.

## Progress

- Peer 1 (task 59, peer-sonnet) candidate baafa983: ACCEPT. Reviewed attachments.ts/api.ts; tsc clean, 637 mobile tests pass. Error index is parsed from the message ("attachment N:"), no daemon change needed. One commit (8b21f26d) had a failing test fixed in the next; history kept.

## Validation

Peer 2 (task 60, peer-sonnet) candidate bb010acc: ACCEPT (diff reviewed; checks re-run by the Lead on the merged branch).

- `cd mobile && npx tsc --noEmit` clean; `npm test` 653 passed (69 files); `task lint` and `task test` exit 0; `go test ./internal/relay/bridge/devharness` ok.
- Web smoke `node scripts/smoke-web.mjs --full --stage attachments` passed (light + dark, 390 px); frames in /tmp/smind-dogfood/frames/mobile-attachments/ were each viewed by the Lead.
- AC1-AC8: web smoke (real relay + devharness daemon with an attachment store) and unit tests. AC9: app.json plugins + mobile/DEV_CLIENT.md. AC10: new-task untouched.
- NOT verified: a real iOS/Android device or simulator (native pickers, HEIC re-encode, expo-image-manipulator on device, expo-file-system read); the web smoke uses the browser file chooser and canvas manipulator. Not moved to completed/ for that reason.
- Item 5 choice: pills always; image thumbnails fetched lazily via run.attachment only when the estimated reply fits one E2EE frame; a failed fetch stays a pill.
