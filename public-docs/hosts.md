---
title: Connected hosts
description: Pair two smind daemons so one can reach the other, with a direction, a grant and a pinned identity.
summary: "This page explains connected hosts in sMind by Spacing Mind: how to turn the feature on, pair two machines with an invite and a short code, approve the pairing, choose which direction is live, and what a link can and cannot do. It is for people who run smind on more than one machine."
order: 18
---

# Connected hosts

A connected host is another machine running its own smind daemon that this
daemon is paired with. A pairing is two hosts that exchanged identities once and
that you approved on the receiving side. It gives you a link you control: you
choose which direction works and what the far side may do.

Connected hosts are off by default. With the feature off your daemon behaves
exactly as it does without it: no key is created, nothing dials out and every
`smind host` command tells you the feature is off.

This page covers pairing and the link itself. Reading and starting work on the
other host builds on a link and is described where it ships.

## Turn it on

Set this in `~/.smind/config.yaml` on **both** machines and restart each daemon:

```yaml
fleet:
  enabled: true
```

The first time the daemon starts with the feature on, it creates a host key in
`~/.smind/fleet/` (readable only by you). The host id is a short fingerprint of
that key. The key never leaves the machine.

## Pair two hosts

Pairing starts on the host that will be *called*. Call it the receiver. The host
that dials is the initiator.

1. On the receiver, open a pairing window and print an invite:

   ```bash
   smind host invite --addr wss://receiver.example:4648
   ```

   The invite is a `smind-host://` link. Everything in it sits after the `#`, so
   no server ever sees it. It names the receiver's host id and public key and where
   to reach it. It never contains a password.

2. On the initiator, add the receiver with that invite. If the receiver has a
   password, put it in an environment variable and name the variable, so it is
   not stored in your shell history:

   ```bash
   RECEIVER_PASSWORD='...' smind host add 'smind-host://v1#...' --alias studio --password-env RECEIVER_PASSWORD
   ```

3. Both hosts now show the same six-digit code. Compare the codes out loud or in
   person. If they differ, someone is in the middle: stop and run
   `smind host rm studio`.

4. On the receiver, approve the pairing and choose the workspaces the initiator
   may create work in. At least one is required:

   ```bash
   smind host approve laptop --write 3
   ```

The window stays open for ten minutes by default, accepts one pairing, and only
a host that first connected after you opened it can take the slot.

## Directions and grants

One pairing creates two directed links. After approval:

- initiator to receiver is **on**, with a default grant: read everything, and the
  workspaces you chose for the rest;
- receiver to initiator is **off**. A call in that direction is refused on the
  side that would receive it.

To let the receiver also reach the initiator, run this **on the initiator**,
because the host that will be called is the one that decides:

```bash
smind host enable-reverse studio
```

The reverse direction starts read-only. The receiver also needs a route to dial,
which only you can set on that machine (a peer can never tell your daemon where
to connect):

```bash
smind host route laptop --addr wss://initiator.example:4648 --password-env INITIATOR_PASSWORD
```

`smind host disable-reverse studio` turns the direction off and closes the live
connection immediately. A full mesh is just `enable-reverse` on every pair.

## What a link carries

Each connection starts with a mutual proof of identity: both hosts sign a fresh
challenge, and each checks the other against the key it pinned at pairing. A host
whose key changed is refused, never accepted silently. A recorded handshake is
useless on another connection. The two hosts also compare protocol versions on
every connect; an older peer gets a clear "update smind on that host" message
and the link stays refused until it is updated. Your local work is never
affected.

A paired host can call only the fleet methods and nothing else. It cannot read
or change your grants, your settings or your secrets. Provider accounts and
passwords never cross a link.

## Routes and cleartext

A direct route must be `wss://`, or `ws://` to this machine or to an address
inside an encrypted overlay network such as Tailscale. A plain `ws://` route to
any other address is refused, because prompts and results would be readable on
the network. You can override that for one host with `--allow-cleartext`; the
command prints a warning when you do.

## Commands

```bash
smind host ls
smind host ls --json
smind host test studio
smind host rename studio workshop
smind host rm studio
```

`smind host ls` shows each host's state, which directions are on and the link's
live state. It never prints a key, a password or a secret.
