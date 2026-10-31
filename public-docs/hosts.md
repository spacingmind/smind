---
title: Hosts
description: Pair this smind with other machines running smind, decide what each may do, and manage them from Settings or a phone.
summary: "This page explains connected hosts in sMind by Spacing Mind (a preview feature): pairing another machine's smind with this one, what the Hosts page shows, how the two directions of a pairing are switched on separately, and what a phone can and cannot do. It is for people who run smind on more than one computer."
order: 18
---

# Hosts

Connected hosts are a preview and are off by default. This page covers the
Hosts page in the app. Turn the feature on with `fleet.enabled: true` in
`config.yaml` and restart smind; until then the page says **Fleet is off** and
offers nothing to click.

## The Hosts page

On the web and in the desktop app, open **Settings → Hosts**. Every paired host
is one row:

- the host's **alias** and label, and a badge for the link: **Connected**,
  **Connecting**, **Unreachable**, **Refused**, **Revoked**, **Off**, or **Peer
  turned this direction off**;
- two plain lines for the two directions of the pairing: whether you can use
  that host, and whether that host can use this one. When the second is on, the
  line under it summarises what it is allowed to do: the actions, the number of
  live agents, minutes per run and the cost cap per 24 hours;
- for a refused link, the reason and what to do about it (for example "update
  smind on the other host");
- a menu with **Edit access**, **Revoke** and **Remove**.

The page never shows a key, a secret or an address of another host.

## Pair two hosts

1. On one host choose **Invite a host**. The dialog shows an invite that lasts 10
   minutes. It contains no password and no provider secret.
2. On the other host choose **Add a host** and paste the invite. Both hosts now
   show the same six-digit code.
3. On the host that was invited, the pairing appears as **Awaiting your
   approval**, with **Review pairing**, and as an item in the
   [Inbox](inbox.md). Compare the code shown there with the one on the other
   host and **type it** into the box. **Approve** stays disabled until the digits
   match; nothing fills the field for you and the Add dialog on the other host
   cannot approve anything.
4. Choose what the other host may do before you approve. If you leave create,
   send or stop on, you must pick at least one workspace it may write in.

Pairing connects one direction: the host that added is allowed to call the
host that was invited. The other direction starts **off**.

## Turn on the other direction

On an approved host, the switch **Let *alias* use this host** turns on the
reverse direction. It asks first and says what it grants: **read only**. That
host can list your workspaces and tasks and see run status; it cannot create,
send or stop anything (0 live agents, no run time, $0). You can widen it later
with **Edit access**. Turning the switch off closes that host's connection at
once.

**Revoke** closes the connection now and removes the host's access; you can pair
it again later. **Remove** deletes the pairing on this host only.

## On a phone

The Hosts screen (Connection sheet → **Hosts**) lists the paired hosts read-only.
A pending pairing can be approved from its [Inbox](inbox.md) row: type the six
digits shown on the other host, and the phone approves **read-only access**
only. Pairing, invites, editing access and the reverse switch are done on a
computer.

## From the command line

Everything on the Hosts page has a command. Pairing starts on the host that will
be *called*, the receiver; the host that dials is the initiator.

1. On the receiver, open a pairing window and print an invite:

   ```bash
   smind host invite --addr wss://receiver.example:4648
   ```

   The invite is a `smind-host://` link. Everything in it sits after the `#`, so
   no server ever sees it. It names the receiver's host id and public key and
   where to reach it, never a password.

2. On the initiator, add the receiver. If the receiver has a password, put it in
   an environment variable and name the variable, so it stays out of your shell
   history:

   ```bash
   RECEIVER_PASSWORD='...' smind host add 'smind-host://v1#...' --alias studio --password-env RECEIVER_PASSWORD
   ```

3. Both hosts show the same six-digit code (`smind host ls` shows it on the
   receiver while the pairing is pending). If the codes differ, someone is in
   the middle: stop and run `smind host rm studio`.

4. On the receiver, approve the pairing and choose the workspaces the initiator
   may create work in. At least one is required:

   ```bash
   smind host approve laptop --sas 123456 --write 3
   ```

   The code you type must match the one shown for that host.

The window stays open for ten minutes by default, accepts one pairing, and only
a host that first connected after you opened it can take the slot.

To let the receiver also reach the initiator, run this **on the initiator**,
because the host that will be called decides:

```bash
smind host enable-reverse studio
```

The receiver also needs a route to dial, which only you can set on that machine
(a peer can never tell your daemon where to connect):

```bash
smind host route laptop --addr wss://initiator.example:4648 --password-env INITIATOR_PASSWORD
```

`smind host disable-reverse studio` turns the direction off and closes the live
connection at once.

### What a link carries

Each connection starts with a mutual proof of identity: both hosts sign a fresh
challenge and each checks the other against the key it pinned at pairing. A host
whose key changed is refused, never accepted silently, and a recorded handshake
is useless on another connection. The hosts also compare protocol versions on
every connect; an older peer gets a clear "update smind on that host" message
and the link stays refused. Your local work is never affected.

A paired host can call only the fleet methods and nothing else: it cannot read
or change your grants, settings or secrets, and provider accounts and passwords
never cross a link.

### What a paired host can read

Once a link is approved and its direction is on, the peer can read, and only
read: this host's workspaces (title, repository URL and default branch, never a
local path), its tasks and their state, the status of a run, and a bounded feed
of what a run did (assistant text is capped at 1 KiB per event; tool inputs,
tool results and permission details never leave this host). Anything that looks
like a credential is removed from what is returned, and a repository URL never
carries a user name or token. A grant that lists specific workspaces limits every
read to those workspaces.

Each request names where it came from. This host refuses a request that does
not match the link it arrived on, one that has already passed through this host,
one that has been relayed through another host (there is exactly one hop), one
that is stale or a replay, and callers that ask too fast. Every call, served or
refused, is recorded in an audit log on this host with a hash and at most 120
redacted characters of the request, never its text; rows are kept for 30 days.

### What a paired host can start

A grant that allows create, send and stop lets the peer start work on this host,
inside the workspaces you chose for it and nowhere else. The peer names the
workspace, a branch that already exists on this workspace's own `origin` (or a
base to branch from), a provider and a model, and a text prompt. This host
fetches the branch itself into a new task and runs the agent with its own
accounts and settings; nothing of the peer's credentials, environment or files
travels, and no file or diff can be uploaded. A provider this host has not
configured, or a branch that is not on `origin`, is refused. The peer can send
more prompts to, and stop, only the tasks it created.

The run always asks as usual: it never starts in an auto-approving mode, your
standing rules and the built-in safety floor decide every request, and a request
nothing decides waits in this host's inbox for you. The peer can see that a run
is waiting for a permission and cannot answer it. Each remote task shows which
host it came from.

Limits are counted per peer host, across everything that host starts: how many
runs may be live at once, how long one run may take, and how much cost the runs
of the last 24 hours reported. A call over the live-agent or cost limit is
refused with the limit named; a run over the time limit is stopped here and
reported as stopped by the wall limit. A run that reports no cost counts as
nothing, never as an estimate. A retry of a create or send that carries the same
idempotency key returns the same task and run instead of starting another.

### Routes and cleartext

A direct route must be `wss://`, or `ws://` to this machine or to an address
inside an encrypted overlay network such as Tailscale. A plain `ws://` route to
any other address is refused, because prompts and results would be readable on
the network. Override it for one host with `--allow-cleartext`; the command
prints a warning. A host can also be reached through your self-hosted relay: run
`smind host invite --via relay` on a daemon that is connected to a relay.

## Read another host

Once a host is paired and connected, you can look at its work without leaving
your own terminal. These commands only read; they never create, send or stop
anything on the other host.

```bash
smind host info studio
smind host workspaces studio
smind host tasks studio --workspace 3
smind host task studio:42
smind host status studio:42
smind host status studio <runId>
smind host activity studio:42 --cursor 120
```

A task on another host is written `alias:id` (for example `studio:42`); the
alias is resolved on this machine only. Every command also takes `--json`. List
commands are paged: when more rows exist, the last line is `more: --cursor N`.

What you get back is whatever the other host chose to share. You see its
workspaces by title, repository address (without any credentials) and default
branch, never a local path; its tasks by title, branch and state; and the text of
a run's activity, cut to 1 KiB per event. Text that came from another host is
printed with control characters removed so it cannot repaint your terminal.

Errors name a code first:

| Code | Meaning |
|---|---|
| `not_granted` | The other host has not allowed this read. Ask its owner to widen access. |
| `host_unavailable` | The link is down, still connecting, or the other host is not paired for this direction. |
| `fleet_version_unsupported` | The other host runs an older smind. The message says which host to update; nothing falls back to your local data. |
| `hop_limit`, `cycle` | The call would pass through a third host or back to you. Reads are direct only. |
| `timeout` | The other host did not answer in 15 seconds. |
| `unknown_host` | No paired host has that alias. |

### Status unknown

`smind host status` asks the other host for a run's state. If three asks in a row
fail (a link that is merely still connecting does not count), it prints
**status unknown** and exits with an error. That does not mean the run
finished: it may still be running. Look at the host before you hand the work to
someone else. A host you have removed is reported as unavailable at once.

### Other commands

```bash
smind host ls
smind host ls --json
smind host test studio
smind host rename studio workshop
smind host label set studio os:linux
smind host select --labels os:linux
smind host rm studio
```

`smind host ls` never prints a key, a password or a secret.

## Route by labels

Instead of naming a host you can tag your paired hosts and pick one by tag. You
set labels yourself, on this host's own row for each peer; they are never sent to
the other host.

```bash
smind host label set winbox os:windows arch:arm64
smind host label rm winbox arch
smind host label ls
smind host label ls winbox
```

A host has at most 16 labels. A key starts with a lowercase letter and is up to 32
characters of `a-z`, `0-9`, `.`, `_` and `-`. A value is up to 32 characters of
`a-z`, `0-9`, `.`, `_`, `+` and `-`. Keys that look like secrets, such as `token`
or `password`, are refused.

Select a host by its labels:

```bash
smind host select --labels os:windows
smind host tasks --labels gpu:cuda --any
```

`smind host select` prints only the alias, so `$(smind host select --labels os:windows)`
works in scripts. `info`, `workspaces` and `tasks` take `--labels` in place of the
host name (giving both is an error). Agents use the same selector: the MCP `host`
argument may be `{"labels":{"os":"windows"},"any":true}`.

The rule: exactly one enabled, connected host must match all the labels,
otherwise you get an error that lists the candidates. With `--any` (or
`any:true`) the host with the fewest live remote agents is picked, ties broken by
alias. A disabled or disconnected host never matches. There is no automatic retry
on another host: if the chosen host fails, the command fails.
