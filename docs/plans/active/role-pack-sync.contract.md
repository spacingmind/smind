---
v: 1
milestone: "ADR-0058 PR 2: role-pack check/apply RPCs, Profiles banner and review dialog, doctor line"
base: origin/develop
planRef: docs/plans/active/role-pack-sync.md
acs:
  - id: AC8
    text: "profile.rolePack.check returns the documented shape from the engine, changes nothing, and is not run-scoped."
    covers: { tests: [TestProfileRolePack_CheckShape, TestProfileRolePack_CheckIsReadOnly, TestProfileRolePack_NotRunScoped], frames: [] }
  - id: AC9
    text: "profile.rolePack.apply is human-only (table, drift test, plugin refusal, not run-scoped) and behaves as the CLI apply: stale prompt fields of untouched seats by default, a customised or capability field only through take, backup written before the first update, dry run writes nothing, changed_meanwhile reported, idempotent, a malformed take refused before any write."
    covers: { tests: [TestProfileRolePack_RPCHumanOnly, TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly, TestProfileRolePack_ApplyTakeChangesACapabilityField, TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate, TestProfileRolePack_ApplyDryRunWritesNothing, TestProfileRolePack_ApplyChangedMeanwhile, TestProfileRolePack_ApplyIsIdempotent, TestProfileRolePack_ApplyRejectsAMalformedTake], frames: [] }
  - id: AC10
    text: "lineDiff lives once in internal/profiles as LineDiff and the CLI diff output is unchanged."
    covers: { tests: [TestLineDiff, cli_diff_output_unchanged], frames: [] }
  - id: AC11
    text: "smind doctor prints one role pack line from the daemon check (updates, singular, all current), nothing when the daemon is not running, the finding is in --json."
    covers: { tests: [TestDoctorShowsRolePack], frames: [] }
  - id: AC12
    text: "The Profiles section shows the banner with the count and a Review button when updates exist, none when every seat is current (a quiet line instead), and an inline error with Retry when the check fails."
    covers: { tests: [rolepack_banner_hidden_when_every_seat_is_current, rolepack_banner_shows_the_count_and_a_review_button, rolepack_check_error_offers_retry], frames: [] }
  - id: AC13
    text: "The check runs when the section opens and nowhere else: no timer, no polling, no refetch on focus."
    covers: { tests: [rolepack_check_runs_once_when_the_section_opens_and_never_polls], frames: [] }
  - id: AC14
    text: "The review dialog lists stale and customised fields per seat with a unified diff marked by + and - characters, capability fields marked with stored and pack values, and changes nothing by itself."
    covers: { tests: [rolepack_review_lists_stale_and_customised_fields_with_their_diff, rolepack_capability_fields_are_marked, rolepack_nothing_is_applied_without_a_click], frames: [] }
  - id: AC15
    text: "Apply N updates sends apply with no take; each other field has a Take that sends take seat.field; a capability take asks for confirmation naming old and new values; after an apply the outcomes and the backup directory are shown and the banner refreshes."
    covers: { tests: [rolepack_apply_sends_no_take, rolepack_take_sends_the_field, rolepack_capability_take_asks_first, rolepack_backup_path_and_outcomes_are_shown], frames: [] }
  - id: AC16
    text: "A daemon refusal or failed seat is shown as received; changed_meanwhile is explained and refreshes the check; the dialog stays open on error."
    covers: { tests: [rolepack_changed_meanwhile_is_explained], frames: [] }
  - id: AC18
    text: "When nothing is applied by default there is no Apply button (only Close); the hint sits above the footer and is gone after an apply."
    covers: { tests: [rolepack_no_apply_button_when_nothing_is_applied_by_default, rolepack_hint_is_above_the_footer_and_gone_after_an_apply], frames: [] }
  - id: AC19
    text: "Check, apply and failed-result errors read as sentences: the daemon's method label is dropped, shared helper used by the Browser section too."
    covers: { tests: [rolepack_errors_drop_the_method_label, daemon_sentence_drops_the_method_and_package_labels], frames: [] }
  - id: AC20
    text: "State badges never wrap inside themselves."
    covers: { tests: [rolepack_state_badges_do_not_wrap], frames: [] }
  - id: AC17
    text: "No change under mobile/; public text says sMind and your workspace and names no other product (hygiene tests pass)."
    covers: { tests: [hygiene_and_mobile_untouched], frames: [] }
tests:
  - { name: TestProfileRolePack_CheckShape, run: "go test ./internal/wsapi -run '^TestProfileRolePack_CheckShape$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_CheckIsReadOnly, run: "go test ./internal/wsapi -run '^TestProfileRolePack_CheckIsReadOnly$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_NotRunScoped, run: "go test ./internal/wsapi -run '^TestProfileRolePack_NotRunScoped$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_RPCHumanOnly, run: "go test ./internal/wsapi -run '^TestProfileRolePack_RPCHumanOnly$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly, run: "go test ./internal/wsapi -run '^TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_ApplyTakeChangesACapabilityField, run: "go test ./internal/wsapi -run '^TestProfileRolePack_ApplyTakeChangesACapabilityField$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate, run: "go test ./internal/wsapi -run '^TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_ApplyDryRunWritesNothing, run: "go test ./internal/wsapi -run '^TestProfileRolePack_ApplyDryRunWritesNothing$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_ApplyChangedMeanwhile, run: "go test ./internal/wsapi -run '^TestProfileRolePack_ApplyChangedMeanwhile$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_ApplyIsIdempotent, run: "go test ./internal/wsapi -run '^TestProfileRolePack_ApplyIsIdempotent$' -count=1", timeoutSec: 900 }
  - { name: TestProfileRolePack_ApplyRejectsAMalformedTake, run: "go test ./internal/wsapi -run '^TestProfileRolePack_ApplyRejectsAMalformedTake$' -count=1", timeoutSec: 900 }
  - { name: TestLineDiff, run: "go test ./internal/profiles -run '^TestLineDiff$' -count=1", timeoutSec: 900 }
  - { name: cli_diff_output_unchanged, run: "go test ./cmd/smind -run 'TestProfileSeedRoles|RoleSync' -count=1", timeoutSec: 900 }
  - { name: TestDoctorShowsRolePack, run: "go test ./cmd/smind -run '^TestDoctorShowsRolePack$' -count=1", timeoutSec: 900 }
  - { name: rolepack_banner_hidden_when_every_seat_is_current, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_banner_hidden_when_every_seat_is_current", timeoutSec: 900 }
  - { name: rolepack_banner_shows_the_count_and_a_review_button, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_banner_shows_the_count_and_a_review_button", timeoutSec: 900 }
  - { name: rolepack_check_runs_once_when_the_section_opens_and_never_polls, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_check_runs_once_when_the_section_opens_and_never_polls", timeoutSec: 900 }
  - { name: rolepack_review_lists_stale_and_customised_fields_with_their_diff, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_review_lists_stale_and_customised_fields_with_their_diff", timeoutSec: 900 }
  - { name: rolepack_capability_fields_are_marked, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_capability_fields_are_marked", timeoutSec: 900 }
  - { name: rolepack_apply_sends_no_take, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_apply_sends_no_take", timeoutSec: 900 }
  - { name: rolepack_take_sends_the_field, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_take_sends_the_field", timeoutSec: 900 }
  - { name: rolepack_capability_take_asks_first, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_capability_take_asks_first", timeoutSec: 900 }
  - { name: rolepack_nothing_is_applied_without_a_click, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_nothing_is_applied_without_a_click", timeoutSec: 900 }
  - { name: rolepack_backup_path_and_outcomes_are_shown, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_backup_path_and_outcomes_are_shown", timeoutSec: 900 }
  - { name: rolepack_changed_meanwhile_is_explained, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_changed_meanwhile_is_explained", timeoutSec: 900 }
  - { name: rolepack_check_error_offers_retry, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_check_error_offers_retry", timeoutSec: 900 }
  - { name: rolepack_no_apply_button_when_nothing_is_applied_by_default, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_no_apply_button_when_nothing_is_applied_by_default", timeoutSec: 900 }
  - { name: rolepack_hint_is_above_the_footer_and_gone_after_an_apply, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_hint_is_above_the_footer_and_gone_after_an_apply", timeoutSec: 900 }
  - { name: rolepack_errors_drop_the_method_label, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_errors_drop_the_method_label", timeoutSec: 900 }
  - { name: rolepack_state_badges_do_not_wrap, run: "cd web/packages/ui && bunx vitest run src/components/settings/role-pack-review.test.tsx -t rolepack_state_badges_do_not_wrap", timeoutSec: 900 }
  - { name: daemon_sentence_drops_the_method_and_package_labels, run: "cd web/packages/ui && bunx vitest run src/lib/daemon-sentence.test.ts -t daemon_sentence_drops_the_method_and_package_labels", timeoutSec: 900 }
  - { name: hygiene_and_mobile_untouched, run: "go test ./internal/hygiene/ -count=1 && test -z \"$(git diff origin/develop --name-only -- mobile)\"", timeoutSec: 900 }
mutations:
  - { id: M9, file: web/packages/ui/src/components/settings/role-pack-banner.tsx, change: "the check error shows the raw daemon message", expectFail: [rolepack_errors_drop_the_method_label] }
  - { id: M1, file: internal/wsapi/session.go, change: "profile.rolePack.apply added to RunScopedMethods", expectFail: [TestProfileRolePack_NotRunScoped, TestProfileRolePack_RPCHumanOnly] }
  - { id: M2, file: internal/permpolicy/humanonly.go, change: "profile.rolePack.apply removed from HumanOnlyRPCs", expectFail: [TestProfileRolePack_RPCHumanOnly] }
  - { id: M3, file: internal/wsapi/profile_rolepack.go, change: "the apply handler skips the backup (updates without ApplyRoles backup)", expectFail: [TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate] }
  - { id: M4, file: internal/wsapi/profile_rolepack.go, change: "the apply handler writes capability fields without a take", expectFail: [TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly] }
  - { id: M5, file: web/packages/ui/src/components/settings/role-pack-banner.tsx, change: "the banner calls apply when it renders", expectFail: [rolepack_nothing_is_applied_without_a_click] }
  - { id: M6, file: web/packages/ui/src/hooks/use-role-pack.ts, change: "the check gets a refetch interval", expectFail: [rolepack_check_runs_once_when_the_section_opens_and_never_polls] }
  - { id: M7, file: web/packages/ui/src/components/settings/role-pack-review.tsx, change: "a capability Take applies without the confirmation", expectFail: [rolepack_capability_take_asks_first] }
  - { id: M8, file: cmd/smind/doctor.go, change: "the role pack line prints a count when the daemon call failed", expectFail: [TestDoctorShowsRolePack] }
frames: []
commands:
  - { id: C1, run: "go build ./... && go vet ./...", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C3, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
  - { id: C4, run: "go test ./internal/profiles ./internal/wsapi ./internal/permpolicy ./cmd/smind -count=1", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# Role-pack sync PR 2 validation contract

Spec: `docs/plans/active/role-pack-sync.md`, section PR 2 (wire, ACs, named tests, frames). The engine and the CLI are PR 1 (merged) and unchanged except `LineDiff` moving into `internal/profiles`. Frames are not validator frames: the UI states are captured by the Lead (frames `01-banner` to `07-capability-confirm`, light and dark at 1280 and 390) under the dogfood root; the validator checks the tests, the mutations and the commands.
