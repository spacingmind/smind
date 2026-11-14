# Crash reports

When part of smind panics, a small report is written on this machine only.
This page says exactly what is in it, what is kept out of it, and how to
read or delete it. Nothing here is sent anywhere: there is no upload, no
network code in the recorder, and no identifier that ties a report to an
account.

## What is recorded

One panic produces one JSON file, `crash-<UTC timestamp>-<source>-<site>.json`,
with these fields:

| Field | Meaning |
|---|---|
| `schema` | report format version (currently `1`) |
| `source` | which program recorded it: `daemon` or `desktop-shell` |
| `version` | the version of that program |
| `os`, `arch` | operating system and CPU architecture |
| `time` | when the panic was recovered, UTC |
| `site` | a fixed label chosen in code (`wsapi.dispatch`, `http`, `desktop.main`), never derived from user input |
| `panicType` | the Go type of the panic value |
| `message` | the panic message — see *What is withheld* |
| `messageWithheld` | `true` when the message was not recorded |
| `stack` | the stack trace: function names, file paths and line numbers |
| `suppressedSinceLast` | identical panics dropped since the previous written report |

The `site` labels where the panic crossed a boundary the recorder guards
(an HTTP request, a WebSocket message, a background goroutine), which is
usually enough to find the code involved.

## Where the files are

The daemon writes to `crashes/` under the smind home directory, which is
`$SMIND_HOME` when set and `~/.smind` otherwise. The desktop shell writes to
`crashes/` under its application data folder:

- macOS: `~/Library/Application Support/dev.spacingmind.desktop/crashes`
- Windows: `%APPDATA%\dev.spacingmind.desktop\crashes`
- Linux: `${XDG_DATA_HOME:-~/.local/share}/dev.spacingmind.desktop/crashes`

A crashes directory that is a symbolic link (or, on Windows, a junction) is
not read and not written: the recorder refuses to follow it.

## What is redacted

The message, the stack and the panic type are redacted before anything is
written. The rules are applied again and again until the text stops changing
(at most three passes), so the stored text is stable under redaction:

- the home directory becomes `~` and the smind home becomes `$SMIND_HOME`
  (matched as whole path components, also in its symlink-resolved form);
- any other user directory shape (`/Users/<name>/`, `/home/<name>/`, `/root`,
  `C:\Users\<name>\`) becomes `<user dir>`, with either separator;
- secrets are replaced with `[redacted]`: `Bearer` and `Basic` credentials,
  `sk-` API keys, GitHub tokens (`ghp_`, `gho_`, `ghs_`, `github_pat_`),
  Slack, Google, GitLab, npm, Google OAuth and Stripe token shapes, cloud
  access keys (`AKIA...`), JWT shapes, private-key blocks, URL passwords of any
  scheme, `key=value`, `key: value` and `"key":"value"` pairs whose key names a
  token, secret, password, passphrase, API key, authorization, cookie or
  credential (a quoted value may contain spaces), hexadecimal runs of 32 or
  more characters, and any 32-character-or-longer letters-and-digits run that
  mixes upper case, lower case and digits.

It cannot recognise a secret of a shape it does not know. Known gaps: a bare
40-character cloud secret key that contains `/` or `+` is caught only when a
key name labels it; a home directory outside `/Users`, `/home`, `/root` and
`C:\Users` (a network share, `/Volumes/<disk>/...`) is replaced only when it is
the current user's home. The stack holds function names, file paths and line
numbers, and the messages of non-runtime panics are withheld entirely (below),
so these gaps rarely matter in practice.

## What is withheld

The message of a panic is kept only when the panic value is a Go runtime
error (`index out of range`, `invalid memory address`, and the like), which
carries no user data. Any other panic value — a string, a wrapped error, a
value built with `fmt.Sprintf` — may contain prompt text, file contents or
tokens, so it is recorded by type only and `messageWithheld` is `true`. The
stack is enough to find the site, and stack frames hold function names,
paths and line numbers, not argument values.

## Limits

- at most 20 reports are kept; older ones are deleted on each write;
- one report is at most 64 KiB (the stack is cut with a marker);
- the directory is capped at 2 MiB, oldest dropped first;
- at most 10 reports a minute;
- an identical panic (same site and same panicking function) within 5 minutes is not
  written again; it is counted into `suppressedSinceLast` of the next
  report.

A panic loop cannot fill the disk or stall the program.

## Privacy

Reports stay on this machine and are never uploaded. The recorder contains
no network code. On macOS and Linux the directory is mode `0700` and each
file `0600`, so only the user account can read them; on Windows the files
carry the standard per-user directory permissions. `smind doctor` reads the
directory and creates nothing, not even the directory itself.

## How to see them

`smind doctor` prints a crashes section: how many reports exist, where they
are, and the time, source and site of the newest one, for the daemon and,
when present, the desktop shell. `smind doctor --json` returns the same
information as a `crashes` object for scripts.

## How to delete them

Remove the crashes folder listed above, or any file in it. It is safe to do
at any time: nothing depends on the reports, and the folder is recreated on
the next recorded panic.

## The desktop shell

The desktop app records its own panics the same way, from a panic hook installed
as the first thing the app does (`desktop/crash-report`). It writes the same
report (`source` is `desktop-shell`; `site` is `desktop.main` for the main thread
and `desktop.thread` for any other, never a thread name or panic text) into the
folder listed above, with the same limits, permissions and refusal of a linked
folder, and `smind doctor` lists it. Differences worth knowing:

- The message of a Rust panic is kept only when it is one of a short list of
  fixed or number-only runtime messages (`index out of bounds: the len is 3 but
  the index is 5`, arithmetic overflow, `unwrap()` on a `None`, a `RefCell`
  already borrowed, `capacity overflow`). Everything else, including `unwrap()` on
  an `Err` (which prints the error's debug text) and every `panic!` or `expect`
  with its own text, is withheld: the type is recorded, the message is not.
- The hook records and then lets Rust carry on as it would have (the default
  message still prints, a main-thread panic still ends the app). It does not
  restart anything or keep the app alive.
- A panic inside a panic hook aborts the process in Rust, so the recorder is
  written so that nothing on its path can panic (the crate denies `unwrap`,
  `expect`, indexing and `panic!` outside tests, and compiles its fixed patterns
  before the hook is set). Installing the hook does no disk access.
- The redaction rules are the daemon's, tested against one shared file of cases
  (`internal/crash/testdata/redact_vectors.json`) in Go and in Rust.
- Not caught: an abort or `panic = "abort"` build, a stack overflow, a crash in
  the web view or other native code, a panic before the hook is set at the start
  of `run()`.

## What happens after a panic

Recording a panic does not always mean the daemon carries on; each goroutine
has a written policy (`internal/crash/policy_table_test.go` lists every one
with its reason, and a test fails when a new goroutine has none):

- **Request-scoped work is contained.** A panic in a WebSocket method answers
  that caller with a generic internal error and the connection lives; a panic in
  an HTTP handler ends that one connection (what Go's HTTP server does anyway);
  a panic while a run is being driven finishes that run as an error instead of
  leaving it running; a panic in a run's event forwarder cancels the run so it
  can finish; a panic in a connection's event pump closes that connection so the
  client reconnects. One-shot usage and CLI lookups are contained, logged, and
  leave the cached answer stale.
- **Everything else still ends the process**, as it always did, after the
  report is written: the listener, the relay bridge, the background loops, the
  per-session pumps, and the workers that change state. A daemon that stayed up
  but silently without its listener would be worse than a restart.
- A contained panic is never silent: the daemon log gets one line naming the
  place (never the panic text) and the report file.

## What a report cannot catch

- a process killed outright by the operating system (no panic runs);
- Go runtime fatal errors, which are not panics and cannot be recovered:
  out of memory, stack overflow, `fatal error: concurrent map writes`, and
  "all goroutines are asleep - deadlock";
- a panic before the recorder is set up at the start of `smind serve` (and in
  every other `smind` command, which does not record at all);
- an exit through `log.Fatal` or `os.Exit` (startup failures such as a port
  already in use): nothing panicked, and the message is in the daemon log;
- a panic inside a timer callback other than the two that change state (the
  idle flush of run rows and the queue wake-up); those still end the process as
  before, without a report;
- a panic on a goroutine started by code outside the packages the recorder
  covers (a drift test lists the covered packages);
- a crash inside the native code of an external tool smind invokes.

Stack traces carry the source paths of the machine that built the binary, not
the machine it runs on. The generic user-directory rules (`/Users/<name>`,
`/home/<name>`, `C:\Users\<name>`) also remove the build machine's user, but a
build path outside those shapes would show. A release is not built with
`-trimpath`: that flag makes Go leave the version stamp out of the binary's
build settings, which the release artifact check reads.

In those cases there is no report; the system log or the tool's own output
is the only trace.
