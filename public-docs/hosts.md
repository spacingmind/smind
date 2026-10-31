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

### Routes and cleartext

A direct route must be `wss://`, or `ws://` to this machine or to an address
inside an encrypted overlay network such as Tailscale. A plain `ws://` route to
any other address is refused, because prompts and results would be readable on
the network. Override it for one host with `--allow-cleartext`; the command
prints a warning. A host can also be reached through your self-hosted relay: run
`smind host invite --via relay` on a daemon that is connected to a relay.

### Other commands

```bash
smind host ls
smind host ls --json
smind host test studio
smind host rename studio workshop
smind host rm studio
```

`smind host ls` never prints a key, a password or a secret.
