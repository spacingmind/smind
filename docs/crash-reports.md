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

## What a report cannot catch

- a process killed outright by the operating system (no panic runs);
- a stack overflow or an out-of-memory kill (the runtime dies before a
  report can be written);
- a panic on a goroutine that is not yet wrapped by the recorder;
- a crash inside the native code of an external tool smind invokes.

In those cases there is no report; the system log or the tool's own output
is the only trace.
