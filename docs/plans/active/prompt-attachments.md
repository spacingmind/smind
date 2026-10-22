# Prompt attachments

Size: XL (10) split into L/M items — scope 2 (daemon + web + desktop + an SDK repo), PRs 2 (about eight, with ordering), risk 2
(untrusted bytes, path containment, secrets, protocol), decisions 2 (ADR-0038 proposed, open questions O1–O7), verification 2 (live
provider calls, macOS app drop/paste). One Lead sequences the items; each PR is non-stacked off `develop`. The security-sensitive
PRs (2, 4, 5) get an independent `reviewer` pass on the exact commit before the Human is asked to merge.

Spec for [ADR-0038](../../decisions/0038-prompt-attachments.md) (read it first: wire shape, caps, delivery, security). Human decisions
D1 (inline base64, capped), D2 (`@file` is a path reference), D3 (web + desktop first, no `mobile/`) are fixed. Status: **ADR
accepted 2026-10-22; implementation starting.**

## Goal

A user attaches images (paste, drop, picker), files from the client machine, `@` files on the daemon machine, and GitHub PRs/issues to a
task prompt, and the agent actually receives them: natively where the runner can take images, as path/text context everywhere
else. History, queued prompts and reconnects show the same attachments.

## Scope and PRs

| PR | Content | Size | Peer | Live calls |
|---|---|---|---|---|
| 1 | **Docs**: ADR-0038 + this plan | S | — | none |
| 2 | **Daemon core**: `internal/attach` (validate, sniff, on-disk set, render), `attachments` on `task.prompt`/`run.start`, `runs.attachments`/`chat_queue.attachments`, metadata on `run.list`/`chat.queueList`/`run.logs`, `run.attachment`, `reuse`, `/ws` read limit, `daemon.info.attachments`; all four runners get the **text/path fallback** through a `PromptInput` value | L | peer-glm (attach pkg, store, render) + peer-sonnet (runs/taskrunner/wsapi plumbing) | none (fake backends) |
| 3 | **Native image delivery**: ACP `image` blocks gated on `promptCapabilities.image`, codex `localImage`, `provider.list` `images` field | M | peer-sonnet | one gated live call per runner |
| 3s | **SDK** (DEPENDENCY, repo `claude-agent-sdk-go`): `ContentBlock`, `QueryBlocks`/`PromptBlocks`, fake-CLI test, release v0.4.0. Then **3b** smind `go.mod` bump + claude-native native blocks | S + S | Lead drafts SDK PR if asked (O7) | one live call |
| 4 | **`@file`** (daemon half is small, folded into PR 2's wiring): exported `files.Resolve`, the `file_ref` resolver and its tests. No new RPC: `task.searchIndex` already lists the paths | S | with PR 2 | none |
| 5 | **GitHub**: `github.search`, accept-time `gh` fetch and snapshot, `<github-pr>`/`<github-issue>` rendering | M | peer-glm | none (`runGH` stub); one manual `gh` check |
| 6a | **Web composer + timeline**: store, queries, tray, attach menu, paste/drop/picker, submit, errors, thumbnails/chips in the timeline and queue, lightbox, `reuse` on Re-run/Continue/Retry | L | peer-glm | none |
| 6b | **Web `@` popover** | M | peer-glm | none |
| 6c | **Web GitHub picker** | M | peer-glm | none |
| 7 | **Desktop**: confirm paste/drop reach the webview in the macOS app; set `dragDropEnabled` only if they do not | S | Lead | macOS app, no model call |

Order: 1 → 2 → (3 ‖ 5, disjoint write scopes: runners / a new github package) → 6a (needs 2) → 6b (needs only `task.searchIndex`, already on develop; independent of the daemon PRs) → 6c
(needs 5) → 7. 3s runs in parallel from acceptance; 3b waits for the SDK release. PR 6a may start as soon as PR 2's wire contract is
merged (the contract is frozen in ADR §1).

Not in this plan: anything under `mobile/` (starts after the mobile Lead's M5; needs O1), auto-attaching a PR from a pasted URL, non-GitHub
forges, audio/video, restoring attachments on "Edit & resend" (O6), attachment quota (O2).

## Acceptance criteria

### PR 1 — docs

- AC1. ADR-0038 exists with status Accepted, records D1–D3, the wire JSON, caps with the measured real ceilings (direct `/ws` vs the 1 MiB
  E2EE frame), persistence/replay, per-runner delivery incl. GLM and the fallback, the SDK dependency, GitHub, security table, UI treatment,
  and open questions O1–O7.
- AC2. This plan has ACs, named tests and the PR split; the Human accepted the ADR 2026-10-22 before PR 2 started.

### Daemon core (PR 2)

- AC3. **Wire.** `task.prompt` and `run.start` accept `attachments`; omitted/empty behaves byte-for-byte as before (existing wsapi tests unchanged).
  `run.start` still returns `{runId}`.
- AC4. **Validation** exactly as ADR §1, all before a run, queue row or file exists; a failed request leaves no run, no queue item and no
  file on disk, and returns `{code, message, index}` with the codes listed in the ADR.
- AC5. **Images.** Allowed: png, jpeg, gif, webp by sniffed magic bytes, equal to the declared type. SVG/HEIC/other, a declared-vs-sniffed mismatch,
  invalid or padded-wrong base64, a `data:` prefix, dimensions over 8000 px: rejected with the right code.
- AC6. **Caps.** Over `maxCount`, an image over 5 MiB, a file over 10 MiB, or total over 10 MiB (decoded): rejected; sizes are computed from decoded
  bytes. Exactly at each cap: accepted.
- AC7. **Names and paths.** A hostile `name` (`../../x`, `a/b`, NUL, control chars, leading dots, 4 KB long, empty) is reduced to a safe base name; the file
  lands only under `$SMIND_HOME/attachments/<set>/` with mode `0600` in a `0700` dir; two attachments with the same name do not collide.
- AC8. **Persistence.** Metadata (never bytes) is stored on `runs.attachments` and `chat_queue.attachments`; `run.list`, `chat.queueList`,
  `chat.queueUpdated` and `run.logs` return it; a daemon restart and a second connection return the same metadata. A queued prompt delivered later
  runs with the same set.
- AC9. **`run.attachment`** returns the bytes of an `image`/`file` of that run, `not_found` for any other id, kind or run.
- AC10. **`reuse`** copies a stored attachment of an earlier run of the same task into a new set; a run of another task, an unknown id and a
  `file_ref`/`github_*` target are rejected; deleting the old run leaves the new one intact.
- AC11. **Fallback delivery.** With no native image path, every runner (claude-native, ACP, codex) receives the user text followed by rendered
  context blocks exactly as ADR §3.1 (golden-tested), image notes carry the absolute path, and claude-native gets `Delivery.AddDirs` for the set.
- AC12. **Never logged.** A sentinel inside a base64 payload appears in none of: daemon log output, the event bus, `run_events`, `request_log`,
  `run.logs`, error messages, and learning transcripts (which carry `[attachment: …]` markers only).
- AC13. **Limits.** `/ws` closes a message over 16 MiB with 1009. A relay-bridged connection rejects a request over 1,048,551 bytes with
  `attachment_too_large` naming the limit. `daemon.info.attachments` reports the per-transport `maxMessageBytes`.
- AC14. **Cleanup.** Deleting a task removes its sets; the start-up sweep removes unreferenced sets older than 24 h and keeps younger ones and
  referenced ones.
- AC15. **Queue and interrupt parity.** Attachments work with `whenBusy` `queue`, `interrupt` and `reject` identically to text.

### Native image delivery (PR 3, 3s, 3b)

- AC16. **ACP.** `acp.Client.SupportsImagePrompt()` reads `agentCapabilities.promptCapabilities.image`; with it, the prompt carries
  `{type:"image", data, mimeType}` blocks (text first, images after); without it (the real GLM `{}`), the fallback applies and no image block is sent.
  A fake ACP agent test asserts both on the wire.
- AC17. **Codex.** `turn/start` input carries `{type:"localImage", path}` entries pointing at readable set files, after the text.
- AC18. **Providers.** `provider.list` reports `images` as yes/no/unknown per ADR §8; the value for GLM is `no`.
- AC19. **SDK.** `claude-agent-sdk-go` exposes block prompts; its fake-CLI test asserts the exact stream-json user message
  (`content` array with `text` and `image/base64` blocks). After the bump, claude-native sends native blocks, keeps a slash-command text block last, and
  falls back to the path note for a type the CLI rejects.
- AC20. An agent echoing an image `user_message_chunk` neither crashes the run nor persists image data (`runner.go` drop kept, test added).

### `@file` (PR 4)

- AC21. The `@` popover lists paths from `task.searchIndex` (tracked + untracked-not-ignored), filtered and ranked client-side by `lib/fuzzy-match.ts`; at most 30 rows.
- AC22. A `file_ref` is accepted only for an existing path inside the worktree after symlink resolution; `..`, an outside absolute path and a symlink
  pointing out are `outside_workspace`; the stored path is normalised, relative, forward-slashed. Nothing reads the file's contents.
- AC23. A ref already present literally in the prompt text is not repeated in the prompt; one that is not present is listed under "Referenced paths".

### GitHub (PR 5)

- AC24. `github.search` runs `gh pr list` / `gh issue list` in the worktree with a fixed `--json` list, 30 results max; a numeric or URL query resolves by number;
  results are cached 10 s and single-flighted per (repo, kind, query); a missing/unauthenticated `gh` or a repo without a GitHub remote returns the
  typed error, not a crash.
- AC25. `github_pr` / `github_issue` attachments are fetched at accept time with argv-only `gh … view`, `repo` regex-validated, a 20 s timeout and the body
  clipped to 32 KiB; the snapshot is stored in metadata; a failed fetch rejects the request and creates no run or queue row.
- AC26. The agent sees `<github-pr …>…</github-pr>` / `<github-issue …>` with the third-party-content notice (ADR §3.1); the body is not in learning
  transcripts; no GitHub call is made on replay, reconnect or queued delivery.

### Web composer and timeline (PR 6a, 6b, 6c)

- AC27. An **Attach** button (`Icons.attach`, aria-label "Attach") opens a menu: Image or file…, Mention a file, GitHub pull request…, GitHub issue…. The old
  "no attachment affordance" assertion is replaced.
- AC28. Paste of an image, drop of files on the task pane, and the native picker (multiple) add pills to a tray inside the card; images become thumbnails,
  others two-line pills with registry icons; remove works by click and keyboard; the tray renders nothing when empty.
- AC29. Client checks mirror `daemon.info.attachments` (count, per-item and total size, image types) and show the reason on the pill; a daemon rejection
  shows its message inline and leaves the draft, text and attachments intact.
- AC30. Submit encodes attachments to base64 at submit time, sends them with `run.start` (both idle and `whenBusy:"queue"|"interrupt"`), clears the
  tray, and the new run entry shows the attachments immediately (optimistic object URLs) with no flicker when `run.list` metadata arrives.
- AC31. A finished run and a queued row show their attachments after reload and reconnect: thumbnails loaded lazily via `run.attachment` (TanStack Query,
  no bespoke fetch), chips for files/refs/PR/issue, lightbox for images, `file_ref` chip opens the file tab, PR/issue chip opens its URL.
- AC32. Re-run, Continue and Retry-with-higher-effort resend the run's attachments via `reuse`.
- AC33. `@` after whitespace opens the popover (not inside an email); results come from `task.searchIndex` via a TanStack `useQuery`, filtered client-side;
  ↑/↓/Enter/Tab/Esc work; selecting inserts `@path` (quoted when needed), records the mention, and submit sends a `file_ref` for each mention still in the text.
- AC34. The GitHub picker lists PRs/issues from `github.search`, shows loading, empty and error (incl. `gh` missing) states, and adds a PR/issue pill.
- AC35. A provider with `images: "no"` shows the pill hint and still sends. All query keys come from `lib/query-keys.ts`; icons come from `lib/icons.ts`;
  shared state lives in the zustand store; no per-component `client.call` effects.
- AC36. **Visuals.** Light and dark screenshots of: composer with tray (image + file + PR), drop-over state, `@` popover, attach menu, GitHub picker, timeline with
  thumbnails, queued row with chips, lightbox, and an error state; **transition frames** (tray add/remove, popover open/close, drop overlay on/off, lightbox
  open/close) are captured on the real app and reviewed by the Lead; no mid-animation glitch.

### Desktop (PR 7) and real-agent verification

- AC37. In the macOS app, pasting an image and dropping a file reach the composer; if Tauri swallows DOM drops, `dragDropEnabled` is set for the window and
  `cargo test` + `cargo clippy --all-targets -- -D warnings` pass in `desktop/src-tauri` and `desktop/daemon-client`.
- AC38. **Live, isolated daemon** (own `SMIND_HOME` and port, never `~/.smind` or 4648; cheap model; a few calls): (a) claude-native receives a small PNG and
  answers about it (fallback first, native after 3b); (b) codex receives a `localImage`; (c) GLM receives the fallback note and a text file and reads it;
  (d) a `@file` is read by the agent; (e) a GitHub PR is fetched with the daemon host's `gh` and quoted back. Results and call counts go into Validation.

## Test scenarios

Names are the contract; Peers use these exact names.

**attach package** (`internal/attach`): `TestValidate_ImageAllowListBySniff`, `TestValidate_ImageDeclaredTypeMustMatch`, `TestValidate_SVGAndHEICRejected`,
`TestValidate_DimensionCap`, `TestValidate_StrictBase64`, `TestValidate_DataURLPrefixRejected`, `TestValidate_CapsAtBoundary`,
`TestValidate_SizesFromDecodedBytes`, `TestValidate_UnknownKindAndCount`, `TestSafeName_Table`, `TestStore_WritesSetWithModes`,
`TestStore_NoCollisionOnSameName`, `TestStore_FailureLeavesNoFiles`, `TestStore_ReuseCopiesToNewSet`, `TestRender_GoldenFile`,
`TestRender_GoldenImageFallback`, `TestRender_GoldenFileRef`, `TestRender_GoldenGithubPR`, `TestRender_GoldenGithubIssue`,
`TestRender_FileRefInTextNotRepeated`, `TestRender_BodyTruncatedWithMarker`, `TestSweep_OrphansOlderThan24h`, `TestSweep_KeepsReferencedAndYoung`.

**store** (`internal/store`): `TestRunAttachments_RoundTrip`, `TestChatQueueAttachments_RoundTrip`, `TestMigrateAttachmentColumns_Idempotent`,
`TestDeleteTask_ReturnsAttachmentSets`.

**wsapi / runs** (`internal/wsapi`, `internal/runs`): `TestRunStart_AttachmentsAccepted`, `TestRunStart_NoAttachmentsUnchanged`,
`TestRunStart_RejectionCreatesNothing`, `TestTaskPrompt_AttachmentsAccepted`, `TestRunStart_QueueKeepsAttachments`,
`TestRunStart_InterruptKeepsAttachments`, `TestRunList_CarriesAttachmentMetadata`, `TestRunLogs_CarriesAttachmentMetadata`,
`TestChatQueueList_CarriesAttachmentMetadata`, `TestRunAttachment_ReturnsBytes`, `TestRunAttachment_NotFoundForOtherKinds`,
`TestReuse_SameTaskOnly`, `TestReuse_SurvivesOldRunDeletion`, `TestAttachmentPayloadNeverLogged`, `TestWSReadLimit_Closes1009`,
`TestRelayConnection_RejectsOverFrameCeiling`, `TestDaemonInfo_ReportsAttachmentLimits`, `TestLearningWindow_MarkersOnly`.

**taskrunner / runners** (`internal/taskrunner`, `internal/acp`, `internal/codex`): `TestPromptInput_FallbackAllRunners`,
`TestPromptInput_ClaudeAddDirsForSet`, `TestACP_ImageBlockWhenCapabilityAdvertised`, `TestACP_NoImageBlockWithoutCapability`,
`TestACP_SupportsImagePromptParsesCaps`, `TestCodex_LocalImageInput`, `TestProviderList_ImagesField`,
`TestACPEcho_ImageChunkNotPersisted`; SDK repo: `TestPromptBlocks_WritesContentArray`, `TestPromptBlocks_TextOnlyUnchanged`; smind after
bump: `TestClaudeNative_SendsImageBlocks`, `TestClaudeNative_SlashCommandTextLast`.

**files** (`internal/files`, `internal/wsapi`; no `file.search`): `TestResolve_InsideRoot`, `TestResolve_RejectsDotDotAndAbsoluteOutside`,
`TestResolve_RejectsSymlinkEscape`, `TestFileRef_AcceptedNormalised`, `TestFileRef_OutsideWorkspaceRejected`,
`TestFileRef_ContentsNeverRead`.

**GitHub** (`internal/…/github`, `internal/wsapi`, `runGH`-style stub): `TestGithubSearch_ArgvAndJSONFields`,
`TestGithubSearch_NumberAndURLResolve`, `TestGithubSearch_CachedAndSingleFlight`, `TestGithubSearch_GhMissingTypedError`,
`TestGithubSearch_NoGithubRemoteTypedError`, `TestGithubAttach_FetchedAtAcceptSnapshotStored`, `TestGithubAttach_RepoRegexAndArgvOnly`,
`TestGithubAttach_Timeout`, `TestGithubAttach_FailureRejectsRequest`, `TestGithubAttach_NoRefetchOnQueuedDelivery`,
`TestGithubAttach_BodyClippedAndNotInLearning`.

**Web** (vitest, `web/packages/ui/src`): `composer-attachments.test.tsx` (attach menu items, paste image, drop files, picker, remove by keyboard, tray empty
renders nothing, client cap pill reason, daemon error inline with draft intact, submit clears tray, queue/interrupt send attachments, provider-images-no hint),
`attachment-pill.test.tsx` (thumbnail vs two-line, close badge visibility rules), `file-mention.test.tsx` (`findActiveFileMention` table incl. email, popover
keys, insert quoted path, `file_ref` only for mentions still in text), `github-picker.test.tsx` (loading, empty, error, `gh` missing, select adds pill),
`run-attachments.test.tsx` (thumbnails via `run.attachment`, chips, lightbox, file-ref opens tab, queued chips, optimistic echo then metadata without
flicker, reload/reconnect shows them), `resubmit-reuse.test.tsx` (Re-run, Continue, Retry send `reuse`), `query-keys.test.ts` (new keys),
`icons.test.ts` (drift test passes with the four new icons), `composer.test.tsx` (the replaced affordance assertion).

**Live / gated** (`internal/wsapi` or `internal/runs`, build tag `liveattach`, isolated daemon, a few calls): `TestLive_ClaudeReceivesPNG`,
`TestLive_CodexReceivesLocalImage`, `TestLive_GLMReceivesFallbackAndTextFile`, `TestLive_AtFileRead`, `TestLive_GithubPRQuoted`.

**Desktop**: manual on the macOS app: paste image, drop file, drop image, attach picker; `cargo test` and clippy (AC37).

## Decisions

Taken from ADR-0038 (Accepted 2026-10-22).

| # | Decision | Status |
|---|---|---|
| D1–D3 | inline base64 (cap 10 MiB direct); `@file` is a path; web + desktop first | Human, fixed |
| A1 | one typed `attachments` list; `image`, `file`, `file_ref`, `github_pr`, `github_issue`, `reuse` | accepted |
| A2 | bytes on disk under `$SMIND_HOME/attachments/<set>`, metadata in SQLite | accepted (O2, O3) |
| A3 | claude-native native blocks wait for the SDK; fallback until then | accepted (O7: Lead drafts the SDK PR; Human merges and releases) |
| A4 | GitHub fetched on the daemon at accept time, snapshot stored | accepted (O5) |
| A5 | relay ceiling 1,048,551 bytes enforced per connection; mobile strategy deferred | accepted (O1 = b: mobile downscales to fit the relay frame) |
| O1–O7 | see ADR "Open questions" | answered 2026-10-22 (O1 b, O7 yes, O2–O6 as recommended) |

## Progress

- 2026-10-22: facts re-verified against the code (text-only wire, runners, 1 MiB E2EE frame, unlimited `/ws` read, GLM `promptCapabilities: {}`, SDK
  string-only content, `Delivery.AddDirs`); ADR-0038 and this plan written; PR 1 opened for review. Nothing else started.
- 2026-10-22: ADR accepted (O1 b, O7 yes, O2–O6 as recommended). The `CI` workflow is intentionally disabled, so nothing merges until the Human re-enables
  it; every PR carries its local check results instead, and PRs that need an earlier PR's code wait rather than stack. Independent starts: PR 2, the SDK PR (3s),
  and the code-independent halves of 4 (`file.search`, `files.Resolve`) and 5 (`gh` fetch/search library).

## Validation

How each AC gets satisfied is recorded here per PR as it lands (command output, screenshots, live-call counts). Empty until implementation.
