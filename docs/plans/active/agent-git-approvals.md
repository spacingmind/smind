# Agent git-operation approval defaults (G13)

Size: M (5) — scope 2, PRs 1, risk 1, decisions 1, verify 1 (daemon policy + card line, screenshots)

Owner: Lead G13. Inputs: ADR-0019 (provider-native permission modes), ADR-0017/0041
(orchestration D9: no PR creation through MCP), local research G13 / H6.

## Goal

Make the git posture explicit and visible. smind classifies a git/gh command
inside a permission request, surfaces the matched rule on the card, and refuses
to offer a standing "always allow" for the risky ones.

## Supervisor defaults (the Human may veto)

| Class | Commands | Posture |
|---|---|---|
| read-only | `git status, diff, log, show, branch -l/--list, fetch, remote -v, rev-parse, ls-files` | free (never labelled) |
| commit | `git commit` (not an amend of another's commit) | asks in default mode; allowed in acceptEdits/auto and above — **see D1** |
| always-ask | `git push`, `gh pr create`, `gh pr merge`, `push --force/-f/--force-with-lease`, `reset --hard`, `branch -D`, `clean -fd/-fdx`, `checkout --`, `restore` of the whole tree, `stash drop`, `commit --amend` | ask in every mode except bypass |
| wholesale | "always allow git" | never offered |
| MCP | PR create/merge/push tools | never exist |

## Decisions

- **D1 (needs the Human — conflicts with ADR-0019 decision 1).** "git commit
  allowed in acceptEdits/auto" would be a smind-side auto-approval, which
  ADR-0019 deleted because matching command strings was bypassable. This plan
  does NOT add one: commit keeps whatever the provider's own mode does
  (claude acceptEdits still escalates Bash to the card), and smind only
  annotates it. Recommendation: keep it that way; if the Human wants commits
  silent, express it provider-natively (`--allowedTools Bash(git commit:*)` for
  claude-native, only after the compound-command behaviour below is verified).
- **D2 matcher = parse, not prefix.** New `internal/taskrunner/gitrules.go`:
  tokenise with quote/escape handling; split on `; & && || | \n`, subshell
  `( )`, `$( )` and backticks (recursing); unwrap `sh|bash|zsh -c '<cmd>'`,
  `env`, `command`, `sudo`, `time`, `nohup`, `exec`, leading `VAR=val`; skip git
  global options (`-C dir`, `-c k=v`, `--git-dir`, `--work-tree`, `--no-pager`,
  `-P`); resolve inline aliases (`git -c alias.p=push p`). Fail closed: if the
  command mentions `git`/`gh` as a word but cannot be parsed (unbalanced
  quote, `eval`, `$VAR` as command or subcommand, `xargs git ...`), it is
  classified `unparsed-git` = always-ask.
  Not matched, documented: aliases from the user's `~/.gitconfig` or
  `.git/config`, scripts and Makefile targets that run `git push`, a binary
  renamed/symlinked to something else, `git` reached through `find -exec`.
  Look-alikes (`git pushd`, `echo git push`, `git log --grep push`) are not
  matches.
- **D3 enforcement is only where smind controls policy.** The decider sees only
  requests the provider already escalated. So smind can (a) annotate, (b) drop
  `allow_always` options, (c) treat the rule as per-call. It cannot make a
  provider ask that would not. Per provider:
  - ACP (glm, kimi): request_permission with kind `execute` → classified; the
    agent's own bypass-style mode or an in-agent allow-list never reaches
    smind (gap, documented). `autoAccept` is bypass-equivalent and stays exempt
    (the Human's own per-run choice, ADR-0019).
  - claude-native: `can_use_tool` for Bash → classified. In `auto` the CLI runs
    `git push` with no prompt at all, so smind also passes the always-ask set as
    `permissions.ask` rules on this run's command line (`--settings '<inline
    json>'`, `claudeGitAskSettings`, generated from the `claudeAskRules` table in
    `gitrules.go`; no settings file is written). Every mode except
    `bypassPermissions` gets it. Patterns per class: the plain prefix
    (`Bash(git push:*)`), `Bash(*git push*)` (reaches `sh -c '...'` and
    `bash -lc "..."`, which the CLI does not unwrap) and `Bash(*git * push*)`
    (reaches `-C dir`/`-c k=v`/`--no-pager` globals; `Bash(git * push:*)` is
    rejected by the CLI as never matching, `:*` is only valid at the end). Force
    variants are all `git push`. Limits: `--settings` is fixed at launch, and the
    CLI applies ask rules in bypass too, so a live switch into bypass keeps
    asking (safe) until the next turn respawns the CLI, and a live switch out of
    bypass has no rules until then (the decider still annotates what the CLI
    escalates). Not covered: git aliases from gitconfig or `-c alias.x=push`,
    scripts/Makefile targets that run `git push`, a renamed binary. Over-asking:
    the CLI patterns are broader than the matcher (`git restore --staged f`,
    `git clean -n` ask too).
  - codex-native: smind has no policy hook beyond the `approvalPolicy`/sandbox
    presets; its own sandbox escalates `.git` writes and network. Classification
    is applied to the card text only. Gap documented.
- **D4 wire is additive.** `PermissionDetail` gains `gitRule` (id, label, class,
  `perCall: true`) plus `git` target fields (parsed argv string, remote, branch,
  staged files for commits). No removed or renamed field. Old clients ignore it.
- **D5 MCP stays PR-free.** A test enumerates the MCP tool catalog and asserts no
  name or description can create/merge a PR or push.

## Acceptance criteria

1. `ClassifyGitCommand` returns {none, read-only, commit, always-ask,
   unparsed-git} with the parsed argv, for every form in the scenarios below.
2. For an always-ask or commit rule, `allow_always` and `reject_always` options
   are removed from the offered options (card cannot offer a standing allow);
   the one-shot allow/deny remain.
3. ACP execute requests and claude Bash requests carry `gitRule` in the
   `permission_request` event; read-only commands carry none.
4. Bypass modes (claude bypassPermissions, ACP autoAccept) are unchanged.
5. The web and mobile permission cards show a quiet rule line ("git push always
   needs your approval"), the parsed command, remote/branch, and the staged
   file list for commits; no "always allow" control for a rule.
6. No MCP tool can create or merge a PR or push.
7. The plan records which provider gaps remain, in this file and in the card
   text for codex/unparsed.

## Test scenarios (named)

Matcher (`TestClassifyGitCommand`, table): read-only set → free; `git commit
-m x` → commit; `git push`, `git push origin smind/task-x`, `git push -f`,
`git push --force-with-lease`, `git push origin +main` → always-ask;
`cd x && git push`, `git -C dir push`, `sh -c 'git push'`, `bash -lc "cd a;
git push"`, `FOO=1 git push`, `env GIT_X=1 git push`, `git -c alias.p=push p`,
`(git push)`, `$(git push)`, `` `git push` ``, `true | git push`, `git
push;ls`, `gh pr create`, `gh pr merge 5`, `git reset --hard`, `git branch
-D x`, `git clean -fd`, `git clean -fdx`, `git checkout -- .`, `git restore .`,
`git stash drop`, `git commit --amend` → always-ask; `git pushd`, `echo git
push`, `git log --grep push`, `grep -r "git push" .`, `git branch -l`, `git
remote -v`, `gh pr view` → no rule; `$GIT push`, `eval "git push"`, unbalanced
quote with `git push` → unparsed-git. Mutation: make the splitter ignore `&&`
or the `-C` skip and `TestClassifyGitCommand` must fail.

Deciders: `TestACPGitRuleStripsAlwaysOptions`, `TestACPGitRuleDetailOnRequest`,
`TestACPGitRuleAutoAcceptExempt`, `TestClaudeBashGitRuleDetail`,
`TestClaudeBashGitRulePipelineForms`, `TestCodexGitRuleTextOnly`, using the
existing fake agents (`internal/acp/fakeagent`, `fakeclaude`). `TestMCPHasNoPRTools`
(`internal/taskrunner/mcp_policy_test.go`).

UI: web card test `shows the git rule line and no always-allow for a git
rule`; mobile card equivalent. Screenshots (light/dark, 1280 and 390): `git
push`, `git commit`, a force push, a pipeline form, plus the open/close
transition frame.

## Progress

- [x] plan committed
- [x] matcher + tests (Peer A, ACCEPTED: 50-form probe by the Lead matched expectations)
- [x] decider wiring + `gitRule` detail field + tests (Peer B, ACCEPTED; Lead added `core.fsmonitor=false` to the staged-file listing)
- [x] MCP no-PR test
- [x] web card line + option filter + tests (Peer C, ACCEPTED)
- [x] screenshots (Peer D capture, Lead reviewed)
- [x] PR (daemon + web in one PR)
- [ ] mobile card line: NOT done. Mobile renders only the summary and option
  buttons (`mobile/src/permissionRequests.ts`, `transcript/permissionButtons.ts`)
  and has no permission-detail surface; adding one is a mobile request-type
  change owned by the mobile item. Follow-up.
- [x] claude-native always-ask via `--settings` `permissions.ask` (Item M, ACCEPTED: Lead did it directly)
- [ ] D1 (commit allowed in acceptEdits/auto): not implemented, needs the Human.

## Validation

- Real claude CLI (haiku, `-p`, throwaway repo, ~USD 0.015): with
  `--permission-mode acceptEdits --allowedTools "Bash(git push:*)"`,
  `cd . && git push` and `echo hi && git push` were NOT denied (the CLI splits
  compound commands and matches each part), `sh -c 'git push'` WAS denied (no
  unwrapping), and `Bash(git commit:*)` did not cover `git push`. So a
  provider-native commit allow-rule would be safe for compounds but smind's own
  matcher is stricter on wrappers. Not verified: whether `permissions.ask`
  settings force a prompt under `auto`/`acceptEdits`; not relied on.
- `go test` of taskrunner, runs, acp with `-race`: pass. `task test`: pass.
  `task lint`: pass. web `bunx tsc -b packages/ui` clean, full vitest 3596 pass.
- Mutation checks: dropping the `allow_always` stripping fails
  `TestACPGitRuleStripsAlwaysOptions` (Lead); Peer A: splitter ignoring `&&` and
  `-C` skipping fail `TestClassifyGitCommand`; Peer B: claudeBashDetail skipping
  the rule, and a fake `pr_create` MCP tool, fail their tests.
- Screenshots (light/dark x 1280/390; push, commit with 4 staged files, force
  push, `sh -c 'cd sub && git push origin HEAD'`; fakeclaude on an isolated
  daemon): 15 of the 16 card crops and the distinct open/close transition
  frames viewed by the Lead, all clean (card appears and disappears in one frame,
  no partial states). Light-1280 pipeline crop was checked only by the Peer.
- Known gaps (documented): codex sandbox policy and agent-side allow-lists never
  reach smind; ACP `autoAccept` and claude `bypassPermissions` are not
  forced to ask; user-level git aliases and scripts that run `git push` are not
  matched. `cmd/smind` full `-race` has a pre-existing data race in
  `TestTaskWait_QueueItemCancelled` (fails on the base commit too).

### Item M: `permissions.ask` on claude-native launch (real CLI 2.1.303, claude-haiku-5-5, `-p`)

- `--permission-mode auto` with no rule pushed silently; with `permissions.ask`
  `Bash(git push:*)` it was refused (asked). `bypassPermissions` + an ask rule
  is also refused: ask rules beat bypass.
- With only `Bash(git push:*)`: blocked `git push`, `cd . && git push`,
  `FOO=1 git push`, `env X=1 git push`; NOT blocked `git -C . push`,
  `git -c k=v push`, `sh -c 'git push'`, `bash -lc "git push"`.
- `Bash(git * push *)` blocks `-C`, `-c`, `--no-pager` forms; `Bash(*git push*)`
  blocks the `sh -c`/`bash -lc` forms; `Bash(*git * push*)` blocks
  `sh -c 'git -C . push'` and `cd . && git -C . push`.
- With the generated set: push (plain, `--force`, `-f +main`, `--force-with-lease`,
  `-C`, `-c`, `FOO=1`, `env X=1`, `cd . &&`, `sh -c`, `bash -lc`), `gh pr create`
  (also `-R o/r`), `gh pr merge`, `reset --hard` (also `-C`), `branch -D` (also in
  `sh -c`), `clean -fd`, `checkout -- f`, `checkout HEAD -- f`, `restore .`,
  `stash drop`, `stash clear`, `commit --amend` (also `commit -a -m x --amend`)
  were all denied; `git status`, `git log`, `git commit --allow-empty` were not.
  `p=push; git $p`, `x=git; $x push` and `eval '...'` were denied by the CLI too.
  `git -c alias.zz=push zz` was NOT (residual gap).
- Through smind's own launch path (isolated daemon, local bare remote, haiku):
  auto + `git push origin HEAD:main` → permission request pending, remote
  unchanged; after `smind task approve` the remote advanced. bypassPermissions
  → pushed with no request. auto + `git status` → no request.
  Transcripts: `/tmp/smind-dogfood/evidence/item-m/transcript-*.txt`.
- Unit/fake tests: `TestClaudeGitAskSettingsPerMode`,
  `TestClaudeAskRulesCoverEveryAlwaysAskClass`,
  `TestRunClaude_GitAskSettingsReachTheCLI`. Mutations (settings dropped from
  auto, bypass gets rules, an allow rule added, a table entry removed) each fail
  a test.
