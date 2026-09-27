# Re-evaluating smind's release model

Date: 2026-09-27. This report reads smind's current release setup,
cross-references it against the survey CatchM (spacingmind/catchm) used
to decide against copying release-please + master/develop, and then
recommends a release model for smind itself. Read-only analysis of the
repo and GitHub; no code/workflow changes.

## 1. Current release setup (evidence)

### 1.1 Components

| Component | File / location | Role |
|---|---|---|
| release-please | `.github/workflows/release-please.yml` | On push to `master`: reads Conventional Commits, opens/merges release PR → creates tag + GitHub Release → builds binaries (linux/darwin × amd64/arm64, tar.gz) + Windows installers (calls `desktop-windows.yml`) → `checksums.txt` → attaches to the Release |
| sync-back | `.github/workflows/sync-develop.yml` | On push to `master`: if develop doesn't contain master's commit, opens a true-merge PR (`merge`, not squash/rebase) master → develop |
| CI guard | `ci.yml` "Guard release metadata" step + `scripts/check-release-metadata.sh` | For PRs based on `master` *only*: fails if the manifest version regresses or CHANGELOG entries present on master go missing |
| Config | `release-please-config.json` | 1 package at root; version bumps also land in `desktop/package.json`, `desktop/src-tauri/tauri.conf.json`, 2 `Cargo.toml` (`extra-files`) |
| Manifest | `.release-please-manifest.json` | `{"." : "0.7.0"}` |
| Rulesets | GitHub API (public repo — free) | `master`: required linear history, rebase-only merges, required check `ci`, no force-push/delete. `develop`: merge/squash/rebase allowed, required `ci`, no force-push/delete |
| Docs | `CONTRIBUTING.md` | Describes the branching model + the incident warning |

### 1.2 Actual release history

- Tags: `v0.2.0 … v0.7.0` (6 releases, latest v0.7.0 on 2026-09-15).
- **All 6 releases shipped 0 assets** (`gh api releases` → `assets: []`
  for every tag) even though the README's "Installing the daemon from a
  release" section tells users to download
  `smind_<v>_<os>_<arch>.tar.gz`. The build+attach job was only added
  by PR #198, merged 2026-09-25 — after every release to date. Three
  `workflow_dispatch` runs on 2026-09-25 (dry runs) all succeeded, so
  the binary pipeline is proven to work but has never attached to a
  real release. Users today get the binary via
  `go install …@latest` or building from source.
- The real incident (correctly reported by CatchM's survey):
  v0.6.0 → v0.7.0. PR #135 (promote develop→master via rebase, syncing
  master's tree to develop's) clobbered
  `.release-please-manifest.json` + `CHANGELOG.md` back to their v0.5.0
  state because develop hadn't absorbed the release commit;
  release-please proposed re-releasing everything in #136 (closed);
  #137 fixed it by hand; #138 released v0.7.0.

### 1.3 New finding: the release pipeline has been stalled since 2026-09-17

This is data CatchM's survey did not have, and it is worse than the
metadata incident:

1. After v0.7.0, sync-develop (#139) and contributors' branch-update
   merges put **true merge commits** into `develop`'s history (exactly
   as sync-develop is designed to do — it deliberately uses true
   merges so its own ancestor check stays correct).
2. The `master` ruleset only allows **rebase-merge** + linear history.
   GitHub cannot replay merge commits → PR #154 (promotion) could not
   be merged and was closed.
3. PR #155 had to substitute: a single-commit branch whose **tree is
   byte-identical to develop's tip**, rebase-merged — squashing 125
   commits into one `chore(master): release develop into master (125
   commits since v0.7.0)` (mergeCommit `1c45c08`, exactly 1 parent
   `8a29705`).
4. Consequence: release-please on master sees exactly one `chore:`
   commit — run log 35261251101 says it plainly: `No user facing
   commits found since 8a29705… skipping`. No release PR has been
   opened since.
5. Current state: `develop` is ahead of v0.7.0 by **187 commits**,
   `master` has one orphan promotion commit, and nothing has released
   for 10 days despite major changes (multi-chat ADR-0016, desktop
   managed daemon, ZCode parity, …).

The crux: **the three guard layers (sync-develop,
check-release-metadata, rulesets) did not detect this stall.** They
only protect against *metadata regression*; the new failure is that
squash promotions destroy the Conventional Commit messages
release-please needs to exist. The model contradicts itself
structurally: sync-develop creates merge commits into develop (to keep
its ancestor check correct) → those very merge commits make the next
promotion un-rebaseable → forcing a squash → blinding release-please.
This is a self-defeating loop, not a one-off operational mistake.

## 2. Cross-reference with CatchM's survey

| CatchM's claim | True/false for smind | Notes |
|---|---|---|
| "3 guard layers only patch the sync-back hole; single-branch can't develop that hole" | **True, and understated** | The patches cover only 1 of 2 breaks. The second break (the post-#155 stall) comes from the master/develop + rebase-only structure itself, not from forgetting to sync |
| "Caused a real incident (v0.6.0→v0.7.0, #135/#136/#137)" | True | Confirmed via PR bodies and git history |
| "rulesets are paid (private repo)" | **Doesn't apply to smind** | smind is public → rulesets are free and actively used (2 active rulesets, confirmed via API). That's a cost CatchM bears, not smind |
| "smind is the weakest model in the survey" | True by evidence | Of the 3 surveyed projects, smind is the only one that broke twice, the second time stalling the pipeline entirely |
| (not mentioned by CatchM) smind has no binary publishing | **Outdated** | True at survey time; a build+attach workflow now exists (PR #198, dry-run green) but has never run for a real release — because the pipeline is stalled (§1.3). The two problems compound |
| "Auto-changelog isn't worth the risk" (for 1-maintainer CatchM) | **Differs for smind** | smind is public, has real users (README install instructions, `go install @latest`), and release-please also syncs version across 4 desktop files (`package.json`, `tauri.conf.json`, 2 `Cargo.toml`) — value CatchM doesn't need and smind uses daily |

Cross-reference verdict: CatchM's survey was directionally right and is
*further corroborated* by smind's data (the ongoing stall). But
CatchM's specific reasons and trade-offs don't transfer wholesale:
CatchM dropped release-please as a 1-maintainer repo that doesn't need
a changelog; smind is the opposite — auto-changelog + multi-file
version sync + binary attach are real value here.

## 3. Recommendation

**Proposal: option (c) — keep release-please, drop the `master` branch
as a release branch. Single integration branch (`develop`),
release-please runs directly on it, and `master` becomes a
fast-forward pointer to the released tag (the same role master plays
in CatchM's model).**

Reasons, by evidence:

1. **The current model is dead in practice, not a "theoretical
   risk".** The last release was 12 days ago, 187 commits are stuck,
   and the only ruleset-legal promotion path (rebase-merge) is proven
   impossible whenever develop contains merge commits — which
   sync-develop guarantees. Keeping it (option a) means accepting
   squash promotions → release-please blind forever, or banning merge
   commits on develop (breaking sync-develop and contributors'
   branch-update workflow).
2. **The `master` branch carries no value for smind.** It isn't a
   regular merge target (CONTRIBUTING forbids it), its tree always
   equals develop's tip at promotion time, and the whole mechanism
   exists only to give release-please a place to commit. But
   release-please **does not need a second branch** — its standard
   design (googleapis and most repos using it) runs on a single
   branch: the release PR opens against that branch, merging it tags +
   releases. The two-branch shape was smind's choice, not the tool's
   requirement.
3. **Copying CatchM's model wholesale (option b) would lose what smind
   has.** A pure bot-tag flow (validate + tag + build) doesn't bump
   the 4 desktop files' versions or generate CHANGELOG.md from
   Conventional Commits. For 1-maintainer CatchM that's dead weight;
   for smind — public, pre-1.0 but with users, with a desktop app
   needing version sync (`task check:versions` runs in CI) — dropping
   it loses real functionality, and the Conventional Commits habit is
   deep in the repo's culture (CONTRIBUTING, all 216 PRs follow it).
4. **The proposed model removes both failure modes at the root** with
   no patches needed: the release commit (bump + changelog) lives on
   `develop` → it cannot be clobbered, no sync-back, no
   check-metadata, no rebase-only ruleset. Exactly CatchM's argument
   #2: single-branch cannot develop this class of failure.

## 4. Migration plan (if approved)

Favorable starting state: `master` is currently an ancestor of
`develop` (in sync, verified with `git merge-base --is-ancestor`), and
both branches' manifests read 0.7.0 — no metadata debt at transition.

1. **Decision (ADR or discussion)**: confirm dropping `master`'s
   release-only role; `develop` becomes the single integration branch;
   `master` remains as the "released code" pointer, fast-forwarded to
   the tag after each successful release, never a merge target. (Keep
   the `develop` name to avoid breaking others'
   workflows/clones; renaming to `main` is optional cleanup, not
   required.)
2. **Edit `release-please.yml`**: trigger `push: branches: [develop]`;
   add `"target-branch": "develop"` to the config (or run the action
   against the default branch). Release PRs open against `develop`;
   merging → tag on `develop` → the binary build + attach flow stays
   as-is.
3. **Add a fast-forward `master` job** (after publish succeeds):
   `git push origin <tag-sha>:refs/heads/master` — safe because master
   is an ancestor of develop; no force-push rights needed.
4. **Delete**: `sync-develop.yml` (whole file), the "Guard release
   metadata" step in `ci.yml` + `scripts/check-release-metadata.sh`,
   the `github.base_ref == 'master'` condition.
5. **Rulesets**: remove the `master` ruleset (or reduce it to
   deletion + non-fast-forward protection if master should be
   immutable); keep the `develop` ruleset as-is. No new rulesets.
6. **Update docs**: `CONTRIBUTING.md` (the new, mostly simpler
   branching model), README if any wording implies master is the
   release branch.
7. **Trial run**: after the change merges, let release-please open
   the release PR for the 187 stuck commits (expected v0.8.0) and
   verify: full changelog, version bump across the 4 desktop files,
   tag on develop, binaries actually appear in the Release's assets
   (for the first time), master fast-forwards to the tag.

**Risks + mitigations:**

- *New tags on `develop`, old tags (v0.2–v0.7) on the master line*:
  the two lines converge at `1c45c08` (master is an ancestor of
  develop), so `git describe`/release-please's last-tag comparison
  still sees correct history. No practical obstruction expected, but
  step 7 must verify `compare vX..vY` renders correctly.
- *Users who follow `master`*: master still updates (fast-forwarded to
  each tag), so "master = released code" behavior is preserved — which
  is exactly the semantics CONTRIBUTING already promises.
- *Missing a release PR via a careless merge*: the release PR is an
  ordinary PR on develop with full CI; no mechanism auto-publishes a
  release other than merging that PR — same safety as the current
  model.
- *Switching release-please's `on: push` to develop*: every push to
  develop now runs the action (previously only master, less often).
  Small cost (the first job is a light action); add `paths-ignore` if
  it ever matters.

**Not recommended**: keeping the model as-is (the pipeline is stalled
now — evidence in §1.3), or copying CatchM's bot-tag verbatim (loses
the changelog + 4-file version sync smind uses daily).

## 5. Missing / unverified data

- Not verified why the README's "Installing the daemon from a release"
  section predates PR #198 (build+attach) — plausibly updated in the
  same line of work while no real release ever ran the job; recorded
  here as "README promises, Release doesn't have".
- No survey beyond the 3 projects in CatchM's report (Syncthing,
  GoReleaser, smind); the claim "release-please runs fine on a single
  branch" rests on googleapis/release-please's documentation/design,
  not a fresh survey.
- The `git push <sha>:refs/heads/master` fast-forward assumes master
  is always an ancestor of the tag; true at writing time (in sync) and
  structurally preserved after migration, but the assumption should be
  recorded in an ADR as an invariant.
