# Never-list floor: deny-only rules for catastrophic commands

Status: completed (2026-11-26; started 2026-11-15). Size: M (5): scope 1 (`internal/permpolicy` + docs), PRs 1, risk 2 (permission floor), decisions 0, verification 1 (unit tests, mutation table).

## Why

The floor already denies a destructive command outside the task worktree (`no-destructive-outside`). What it does not cover, found by running candidate commands through `CheckFloor` on `origin/develop`:

| Command | Floor today |
|---|---|
| `rm -rf /`, `rm -rf --no-preserve-root /`, `dd of=/dev/sda`, `cat x > /dev/sda` | denied (`no-destructive-outside`) |
| `rm -rf ~/*`, `rm -rf /*`, `rm -rf $HOME/*`, `cd ~ && rm -rf *`, `rm -rf ~` when the worktree sits under the home | a glob word is "dynamic": the engine only asks, so a policy allow such as `rm *` could win |
| `mkfs.*`, `mkswap`, `wipefs -a`, `blkdiscard`, `shred /dev/sda`, `sgdisk --zap-all`, `parted … mklabel`, `cryptsetup luksFormat` | not seen at all (the device is a plain word, not a mutator destination) |
| `diskutil eraseDisk`, `diskutil secureErase`, `Format-Volume`, `Clear-Disk`, `format C:` | not seen |
| `:(){ :\|:& };:` (fork bomb) | not seen |
| `shutdown`, `reboot`, `halt`, `poweroff` | not seen |
| any of the above through `su -c`, `ssh host '…'`, `eval '…'`, `echo '…' \| sh`, `sh <<< '…'`, `nsenter … rm -rf /` | not seen: no floor rule unwraps a command runner except `no-self-approve` |

Only the missing part is added. Everything the floor already denies keeps its id.

## Decisions

- One new compiled-in floor id: `no-catastrophic` ("a catastrophic command: wiping the filesystem root or the home, formatting or zeroing a device, a fork bomb, a power-off"). Compiled in like every floor rule: no `policy.yaml` rule can allow it (the existing floor-wins tests extend to it).
- It is deny-only and conservative: a command is only hit when its *target* is catastrophic (a root-like directory, a block device) or the command is catastrophic by nature (`mkfs` on a device, `diskutil eraseDisk`, a fork bomb). `rm -rf ./build`, `rm -rf node_modules`, `gofmt -w x.go`, `mkfs.ext4 disk.img` (a file image), `dd of=./disk.img`, `fdisk -l /dev/sda` (a listing) stay as they were.
- Root-like target = the filesystem root, the human's home, the parent of the home (`/Users`, `/home`), and the top-level system directories (`/etc /usr /bin /sbin /lib /var /boot /opt /System /Library /Applications /Volumes /private`, a volume root, `C:\Windows`, `C:\Program Files`, `C:\Users` on Windows). Matched on the resolved path, so a symlink to `/` is `/`; a trailing `/`, `/*`, `/.*` is stripped; `.` and `*` are resolved against the segment's cwd (`cd ~ && rm -rf *`). A word with any other glob stays "dynamic" (the engine asks).
- Wrapped forms reuse the one shared wrapper layer: `unwrap()` (env, command, nohup, xargs, timeout, busybox …) plus the `commandRunners` / bare-shell matcher that `no-self-approve` uses, factored out of `humanonly.go` so both rules read the same table. A runner's text (`su -c '…'`, `ssh h '…'`, `eval '…'`) is re-split and judged as commands, not scanned with a regex; a bare shell fed text (`echo '…' | sh`, here-string, heredoc body) judges every echo/printf argument, here-string and raw line the same way.
- The fork bomb is detected from the raw text (the splitter breaks `:(){ :|:& };:` into pieces) and only when the command also runs something other than a reader or text writer, so `grep 'fork bomb' docs`, a commit message or `cat > x <<EOF` that names it is not a hit.
- Power-off (`shutdown reboot halt poweroff`, `systemctl poweroff|reboot|halt`, `init 0|6`) is in the list: it stops the daemon and every other run. It is the one judgement call; the Human can drop the row.
- No ADR: this extends the floor of ADR-0047 §2.1 with the same mechanism and is not an architecture change. The floor table in ADR-0047, the public permission-policy page and the policy template comment gain the new id.

## Limits (written in the public page)

A command the matcher cannot read stays outside it: a script the agent wrote and runs later, a program that runs a command from its own text (`python -c 'os.system(…)'`, `awk system()`), a copy of `rm` renamed to something else (only the `--no-preserve-root` flag is matched under any name), `go run` of a program that deletes, and a target fed on stdin (`find / | xargs rm -rf`: the engine asks, no allow rule should cover `xargs`). The floor narrows the set of catastrophes; it does not remove the need to keep an agent out of `bypassPermissions`.

## Acceptance criteria

- AC1: every row of the "missing" list in "Why" is denied with `no-catastrophic`, through each wrapper in AC3.
- AC2: the near-miss table is allowed (no floor hit), including its Windows rows.
- AC3: wrapped forms are denied: `env`, `command`, `nohup`, `timeout`, `xargs`, `busybox rm`, an absolute path, `sh -c`, `su -c`, `ssh host '…'`, `eval '…'`, `echo '…' | sh`, `sh <<< '…'`, a heredoc fed to `sh`, `nsenter … rm -rf /`, `find / -delete`, `find ~ -exec rm -rf {} +`.
- AC4: no policy rule can allow a hit (an `allow` rule for `rm *` does not beat `rm -rf ~/*`).
- AC5: the id appears in the public permission-policy page, the ADR-0047 floor table and the policy template comment; a drift test fails when a `floorReasons` id is missing from the public page.
- AC6: a mutation per rule row fails a named test (recorded in Validation).
- AC7: `no-self-approve` behaviour is unchanged after the shared-wrapper refactor (its tests pass untouched).

## Test scenarios

- `TestNeverList_Denied` (table: command, wrapper, expected id) including the Windows-flavour table `TestNeverList_WindowsDenied` on the fake Windows platform.
- `TestNeverList_NearMissesAllowed` (and `…WindowsNearMissesAllowed`).
- `TestNeverList_RootLikeTargets` (resolution: `~`, `$HOME`, `${HOME}/`, `/*`, `..` from a home subdirectory, a symlink to `/`, `cd ~ && rm -rf *`).
- `TestNeverList_ForkBombText` (the classic form, a named function, `fork while fork`; near misses: grep, echo, commit message, heredoc into `cat`).
- `TestNeverList_RunnerTextRejudged` (every runner of `commandRunners` with a catastrophic payload; payload that is fine stays fine).
- `TestDecide_NeverListCannotBeAllowedByPolicy`.
- `TestFloorReasonsInPublicDocs` (drift).
- `TestFloor_SelfApproveCLIDenied` and friends unchanged (AC7).

## Progress

- [x] spec (this file)
- [x] rules + tests (`internal/permpolicy/never_list.go`, `never_list_test.go`, `never_list_win_test.go`)
- [x] docs (public page, ADR-0047 table, template comment) and the drift test `TestFloorReasons_InDocs`
- [x] mutation table (below)
- [x] two-axis review, findings fixed (below)
- [ ] Human review, merge

## Validation

- AC1-AC4: `TestNeverList_Denied` (about 150 rows, every wrapper of AC3), `TestNeverList_WindowsDenied`, `TestNeverList_NearMissesAllowed` (about 130 rows), `TestNeverList_WindowsNearMissesAllowed`, `TestNeverList_RootLikeTargets`, `TestNeverList_ParentOfHome`, `TestNeverList_ForkBombText`, `TestNeverList_RunnerTextRejudged`, `TestDecide_NeverListCannotBeAllowedByPolicy`, `TestNeverList_ExistingIDsKept` (all pass).
- AC5: `TestFloorReasons_InDocs`. AC7: the `no-self-approve` tests pass untouched after `bareShellFed` was shared.
- AC6 mutations (one per row, run on the committed file; each makes at least one named test fail): rm root-like target, `--no-preserve-root`, home, parent of home, system roots, filesystem root, glob tail strip, bare `*`, Windows star quirk, mkfs on a device, mv source, chmod -R, find -delete, find -exec, dd `of=`, wipefs -a, redirect to a device, partitioner device and read-only exemption, diskutil verbs and apfs delete, cryptsetup, Windows disk commands, power commands, systemctl power verbs, `init 0|6`, Windows `format`, `cipher /w`, busybox applet, runner script text, runner word suffix, runner here-string, shell-fed lines and echo words, fork bomb definition, `fork while fork`, fork-bomb runnable-segment guard, shared `bareShellFed`, runner depth cap, `viaSuffix`, ssh command word. Two rows first survived (dd `of=` and the device redirect: the old rule already denied them); rows reachable only through a runner (`su -c 'dd ... of=/dev/sda'`) now pin them.
- Review (code-review skill, two fresh-context agents): the Spec/attack axis found false positives (`ssh host systemctl status reboot`, `ssh reboot`, a fork-bomb string argument of `npm test`) and gaps (`fish -c`); fixed: power verbs are judged only as the real command, never as a later word of a runner's arguments; the fork bomb needs a command that can run text; `fish csh tcsh` added to the shells. Standards axis: no hard violations; fixed the dead `mkfs.ext4` entry, missing doc comments, and moved the shared helper next to its sibling.
- Not verified: Windows behaviour on a real Windows host (fake-platform tests and `GOOS=windows go vet` only); `internal/wsapi` `TestPolicyEngine_FloorProtectsDaemonHome` fails on this machine on `origin/develop` too (E9 item 3a).
