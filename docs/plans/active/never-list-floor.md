# Never-list floor: deny-only rules for catastrophic commands

Status: active (2026-11-15). Size: M (5): scope 1 (`internal/permpolicy` + docs), PRs 1, risk 2 (permission floor), decisions 0, verification 1 (unit tests, mutation table).

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

- [ ] spec (this file)
- [ ] rules + tests
- [ ] docs (public page, ADR-0047 table, template comment)
- [ ] mutation table
- [ ] two-axis review
- [ ] PR

## Validation

Filled in when the PR is ready.
