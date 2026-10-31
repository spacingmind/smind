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
