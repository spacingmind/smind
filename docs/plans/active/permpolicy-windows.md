# permpolicy: real Windows path support (ADR-0047 follow-up)

Release blocker for 0.17. The Windows full suite failed `internal/permpolicy` with 62 failures; two were real
fail-open bugs. Human decision 2026-10-31: no on-Windows gate, real support in one PR. Fallback (>3 h stalled):
the gate from PR #717 (engine refuses to enable on Windows).

## Acceptance criteria

- AC1 `go test <path outside the worktree>` is `ask` on Windows paths (`D:\x`, `C:/x`, `\\srv\share\x`), never `allow`
  (`TestDecide_WindowsOutsideDirIsAsk`). Root cause: the POSIX splitter ate the `\` of `D:\gotmp\x` and the word became a
  relative path inside the worktree.
- AC2 The floor beats an allow rule on Windows: `Edit C:\Windows\x` with an allow rule `within C:\Windows` is `deny`
  (`TestDecide_WindowsFloorBeatsAllowRule`); `Write /etc/x` style POSIX-rooted words on Windows are unresolvable, hence
  denied by `no-destructive-outside`, never judged against the cwd.
- AC3 Path flavour is injectable per request (no global): every path decision runs through `platform`
  (`posixPlatform()`, `windowsPlatform(fs)`), the filesystem through a `pathFS` seam, so the Windows tests run on
  macOS/Linux with a fake filesystem and on the real Windows runner with the real one.
- AC4 Windows paths: drive letters (any case), UNC `\\host\share\x`, `\` and `/` and mixed separators, `\\?\C:\x` verbatim
  prefix stripped, case-insensitive comparison, `..`/`.` cleaning, msys `/c/Users/x` and `/cygdrive/c/x` mapped to
  `C:\Users\x`. Unrepresentable (`C:foo` drive-relative, `\rooted` without drive, `\\.\dev`, `:` stream, `*?<>|"`,
  trailing dot/space component) is an error, so the engine asks / the floor denies a destructive destination.
- AC5 Home: `~`, `~\`, `~/`, `$HOME`, `${HOME}`, `%USERPROFILE%`, `$USERPROFILE`, `${USERPROFILE}`, `$env:USERPROFILE`
  expand to the request's Home on Windows; the real-home path comes from the request, else `os.UserHomeDir()`
  (USERPROFILE).
- AC6 Symlinks, junctions, reparse points: resolved through `realpath.Resolve` (GetFinalPathNameByHandle) with the same
  dangling-link walk as POSIX; fake-fs tests cover link, dangling link, junction escape, `..` escape; a real junction
  (`mklink /J`) test runs only on Windows.
- AC7 Install state on Windows: `Home\.smind`, `.claude`, `.claude.json`, `.config`, `.ssh`, `.gnupg`, `AppData`,
  the daemon home, plus `%SystemRoot%` (default `C:\Windows`), `Program Files`, `Program Files (x86)`, `ProgramData`.
- AC8 Every floor id has a Windows-native test: `no-stash`, `no-force-push`, `no-merge`, `no-install-state`,
  `no-secrets`, `no-destructive-outside`, `no-real-home-tests`, `no-curl-sh`, `no-add-all` (names listed in Test
  Scenarios). `git.exe stash` / `GIT.EXE` / `C:\Program Files\Git\cmd\git.exe stash` all hit `no-stash`.
- AC9 PowerShell/cmd equivalents: `Remove-Item/ri/del/erase/rd/rmdir`, `Set-Content/Add-Content/Out-File/New-Item/
  Copy-Item/Move-Item/Rename-Item/Clear-Content`, `copy/xcopy/move/robocopy/ren/rename/mklink/attrib/icacls/takeown`
  with a destination outside the worktree or unresolvable: `no-destructive-outside`; `Get-Content/type` of a secret
  name or `Get-ChildItem Env:`/`set` dump: `no-secrets`; `powershell|pwsh|cmd -c/-Command` literal payloads are parsed in
  their own dialect (PowerShell `;` `|` `&` call operator, cmd `&` `&&` `||`, nested wrappers), so the floor and the rules
  judge the inner commands; the wrapper itself is never allowed by any rule (`ask` at best), and a payload that cannot be
  parsed with certainty is `ask`. Documented in public-docs.
- AC10 Everything unparseable on Windows is `ask`, never `allow`.
- AC11 POSIX behaviour is byte-for-byte unchanged: the existing POSIX tests run unchanged on macOS/Linux (they skip on a
  Windows runner, where the Windows-native equivalents run instead).
- AC12 `windows-full-suite.yml --ref <branch> -f packages=./internal/permpolicy/...` is green.

## Design (binding)

`platform.go`: `type platform struct { windows bool; fs pathFS; env func(string) string; tempDirs func() []string }`.
`Request` gets an unexported `plat *platform` (nil = host: `runtime.GOOS`, `osFS{}` over `internal/realpath`, `os.Getenv`,
`os.TempDir()`); tests set it. Methods: `isAbs, clean, join, dir, base, volume, eq(a,b), under(p, root)` (case-fold on
Windows, separator-aware), `toNative(word)` (msys mapping, `\\?\` strip, `/`→`\`), `resolve(p, cwd)` (the old
ResolvePath body: expandHome, abs, clean, EvalSymlinks walk through `fs`), `cmdName(argv0)` (base on either
separator; on Windows lower-cased and `.exe/.com/.cmd/.bat` stripped). The exported `ResolvePath`, `Within`,
`SegmentPaths`, `CheckFloor`, `CheckFloorPaths`, `Decide`, `Load` keep their signatures and use the request's (or host)
platform. Splitter: `SplitCommand` is the POSIX splitter; a Windows request uses `splitCommand(cmd, true)` where an
unquoted backslash followed by a path character (not a shell metacharacter, space, quote, `$`, backtick, newline,
`\`, `~`, `#`, `!`, `*`, `?`, `[`, `;`, `&`, `|`, `(`, `)`, `<`, `>`) stays a literal backslash; before a
metacharacter it keeps the POSIX escape, so a command can never hide inside a quote.
Why the Windows reading of `D:\x` wins: for an allow it yields `ask` (outside), for the floor at worst a harmless
extra deny.

## Test Scenarios (named)

Platform: `TestPlatform_WindowsClean`, `_WindowsIsAbs`, `_WindowsUnder`, `_WindowsToNative`, `_WindowsUnrepresentable`,
`_PosixUnchanged`. Resolve: `TestResolveWindows_DriveCaseUNCMixedSeparators`, `_LinkJunctionDanglingEscape`,
`_HomeExpansionForms`, `TestResolveWindows_RealJunction` (windows only). Splitter: `TestSplitWindows_BackslashLiteral`,
`_MetacharKeepsEscape`, `_NoCommandHidingInQuote`. Fail-open: `TestDecide_WindowsOutsideDirIsAsk`,
`TestDecide_WindowsFloorBeatsAllowRule`. Floor (one per id, Windows-native): `TestFloorWindows_Stash`, `_ForcePush`,
`_Merge`, `_InstallState`, `_Secrets`, `_DestructiveOutside`, `_RealHomeTests`, `_CurlSh`, `_AddAll`, `_PowerShellCmd`,
`_SystemDirs`. Load: `TestPolicyLoadWindows_RejectsRelativeWithin`, `_AcceptsDriveAndUNC`. Audit:
`TestAuditWindows_ReasonsAreRedacted`.

## Decisions / Progress / Validation

### Decisions

- No on-Windows gate (Human, 2026-10-31): PR #717 (gate) is superseded. The path flavour is per request (`Request.plat`, unexported; nil = host), so the Windows rules run on macOS/Linux with a fake filesystem and on the Windows runner with the real one.
- The splitter reads `\` as a literal backslash before a path character in Windows mode and keeps the POSIX escape before any shell metacharacter: the Windows reading of `D:\x` can only turn an allow into an ask and a floor miss into a deny, never hide a command in a quote.
- A word the Windows flavour cannot place is a path error (ask / floor-deny) unless it cannot escape anything (no separator and no drive prefix, a URL, a refspec): those are not paths.
- The temp zone is allowed for scratch writes; under Windows only the `AppData` install root is carved out of it (the OS temp dir lives there).
- Known pre-existing gap (POSIX too): a quoted prefix assignment `HOME="x" cmd` is not read as an env assignment, so `no-real-home-tests` does not see it.

### Progress

Spec, platform layer, splitter mode, resolution through the platform, floor and Decide plumbing, Windows rules (system dirs, PowerShell/cmd, secrets, curl|iex, home vars), Windows-native tests for every floor id, real-filesystem Windows tests, docs. Delivered by peers (GLM was rate limited until 14:20, so peer-sonnet did the coding).

### Validation

Windows full suite green on the PR branch for `./internal/permpolicy/...` (run URLs in the PR body); macOS `task test` and `task lint`; one mutation per named test (list in the PR body).
