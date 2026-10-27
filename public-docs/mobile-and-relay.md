---
title: Mobile and relay pairing
description: Pair a phone through the relay, what end-to-end encryption protects, push notifications and the task tree.
order: 6
---

# Mobile and relay pairing

The mobile app talks to your daemon through a **relay**: a small server that
forwards encrypted traffic between your daemon and your phone. Your daemon
dials out to the relay, so it works behind NAT and needs no open port. To
install the app, see [Getting started](getting-started.md).

A public relay run for smind users does not exist yet. For now you run your
own, or use one someone gives you. Running a relay (ports, Docker, backups) is
an operator task and lives in `docs/homelab.md` in the repository.

## Pair a phone

You need a relay address, a relay host id and its secret. If you run the relay
yourself, create the host on the relay machine:

```sh
smind relay host new phone
```

The secret is printed once. The relay stores only its hash. Other host
commands are `smind relay host ls`, `smind relay host rm <id>` and
`smind relay host rotate <id>`. Host changes take effect when the relay
restarts.

On the machine that runs `smind serve`, connect the daemon to the relay:

```sh
smind relay connect relay.example.com:7400 phone <secret>
```

`connect` first checks the relay's certificate like a web browser. If your
system trusts it, nothing is pinned. Otherwise it shows the certificate
fingerprint and asks you to confirm it. Compare it with the fingerprint the
relay printed at start. It never pins a certificate silently. To skip the
prompt, pass `--fingerprint <hex>`. Restart `smind serve` if it was already
running; it dials the relay on every start.

Then print a pairing link:

```sh
smind relay offer
```

The link goes to stdout. On a terminal, or with `--qr`, a QR code is also
drawn. `--png <file>` writes the QR code to a file (`--force` overwrites an
existing file).

> **Treat the link like a password.** Anyone who has it can reach your daemon
> through the relay. To cut off every link at once, run
> `smind relay host rotate <id>` on the relay, then `smind relay connect` again
> on the daemon with the new secret.

In the mobile app, paste the link on the pairing screen and press **Connect**.
The first time, the app creates a device key and keeps it in the phone's
secure storage.

### Approve the device

Printing a link opens a **pairing window**: 10 minutes or one new device,
whichever comes first. The first new device that connects while it is open is
approved automatically. Change that with `--window <duration>` and
`--max-devices <n>`.

A device that connects outside the window is held as pending. The app shows
**Waiting for approval** with the command to run on the daemon's computer, and
connects by itself once you approve:

```sh
smind relay devices ls --pending
smind relay devices approve <id>
```

Ids accept any unique prefix. More device commands:

```sh
smind relay devices ls
smind relay devices rename <id> <label>
smind relay devices revoke <id>
```

A revoked device is disconnected and cannot return. Its app shows **This device
was revoked**; **Reset device identity** gives it a new key, and the daemon
treats it as a new device that needs approval again.

### Daemon password

If the daemon has a password (`smind set-password`), every relay device must
present it on each connection, approved ones included. The app asks with
**Daemon password required** and shows **Incorrect password** when the daemon
refuses it. The password travels inside the encrypted channel and the app
keeps it only in memory.

### Common messages

- **Daemon outdated or on a different version**: the daemon does not support
  device keys. Run `smind update` on the daemon.
- The relay needs a certificate the phone already trusts, for example one from
  a public certificate authority (`smind relay --tls-cert <pem> --tls-key <pem>`).
  A phone cannot confirm a fingerprint, so a self-signed relay is refused.

## What end-to-end encryption protects

Traffic between your phone and your daemon is encrypted on both ends. The relay
only forwards sealed bytes.

The relay **can** see:

- that a daemon and some devices connect to it, because it forwards their
  traffic and checks the relay host secret.

The relay **cannot** see:

- your code, prompts, task titles or agent output;
- the daemon password, which is checked inside the encrypted channel.

The relay also cannot let a stranger in. A new device must be approved by you
on the daemon, and you can revoke one at any time with
`smind relay devices revoke <id>`.

What it does not protect: the pairing link carries the host secret, so keep it
private, and anyone you approve sees all of this daemon's workspaces, spaces
and tasks. Connecting registers the daemon once with the relay; nothing is
created in smind.

## Push notifications

The phone gets a notification when a run:

- needs a permission approval;
- reaches the turn cap and needs Continue;
- finishes or fails (sent for top-level tasks only);
- sits in a stalled task tree (see [Orchestration](orchestration.md)).

Tapping a notification opens that task. Notifications arrive with the app in
the background or closed.

**Platforms.** Push works on Android. On iOS the app shows **Not available on
iOS yet** because it needs the Apple Developer Program. It needs a development
or installed build; in Expo Go the row shows **Needs a development build**.

**Turn it on.** Nothing is registered, and no permission is asked, until you
act. In the app open **Connection**, then **Notifications**, then **Turn on**.
It needs a paired and approved device. `smind relay devices ls` has a PUSH
column.

**Test it** without running an agent:

```sh
smind push test
smind push test <device id prefix>
```

The first form sends to every registered phone. The command exits 0 only if
every phone was sent. If no phone is registered it tells you to turn
notifications on in the app.

**What is sent.** The daemon sends one HTTPS request per notification to
Expo's push service. It carries an encrypted blob and the phone's push token.
Task titles, prompts and permission text are sealed with a key only your daemon
and that phone hold, so Expo, Google and Apple cannot read them. If the phone
cannot decrypt a push, it shows "Open smind to see what happened". The daemon
needs outbound HTTPS to `exp.host`.

**Turn it off for the whole daemon** in `config.yaml`:

```yaml
push:
  enabled: false
```

The app's Notifications row then shows **Off on this daemon**. The default is
`true`, which sends nothing until a phone registers. The environment variable
`SMIND_PUSH=off` does the same. See [Configuration](configuration.md).

**Limits.** Delivery is best effort and can be late or lost. A force-stopped
app or an aggressive battery saver stops Android from receiving pushes until
you open the app. Revoking a device stops pushes to it; after re-pairing, turn
notifications on again.

## The read-only task tree

When a task has subtasks (see [Orchestration](orchestration.md)), its screen on
the phone shows a **Subtasks** section with the count, for example
`Subtasks · 3`. Tap the header to collapse or expand it. It refreshes every 30
seconds.

Each row shows:

- the task title and a tier badge such as **Lead**;
- its state: Idle, Running, Waiting for permission, Done, Error, Stopped or
  Rate limited, with a reset countdown when one applies;
- a **Stalled** note with the minutes since the last run, when the tree is
  stalled;
- a permissions badge when requests wait for approval;
- the cost of the task.

The header also counts how many subtasks are waiting, rate limited or failed. A
budget line (`Budget $1.20 / $5.00`, or `Over budget`) appears when the root
task has a tree budget. Tapping a row opens that task. The tree is read-only:
you cannot approve, stop or change anything from it.

## Plugins on the phone

The phone lists plugins but never changes them. In the app open **Connection**,
then **Plugins**, then **Open**. The screen shows each plugin's name and
version, a source chip (**Folder**, **Link**, **Git** or **Archive**) and a
status chip: **Enabled**, **Disabled**, **Pending update** or **Error**. An
error shows its last message, and a linked plugin shows **Live directory:
changes after consent are not re-reviewed**. When plugins are off for the
daemon, the screen says **Plugins are off on this daemon. Turn them on in
config.yaml on the computer.** Tap a plugin for what it declares: its
capabilities, MCP servers, skills, profiles (a profile with elevated rights is
flagged **Needs your attention**), prompt commands and conflicts, each marked
`from <plugin>`. Install, enable, disable, update and remove are only on the
computer: **Manage plugins on the computer: Settings -> Plugins**. The list
refreshes when a plugin changes.

In a task's composer, type `/` at the start of the text to see the prompt
commands plugins provide, each with `from <plugin>`. Tapping one puts its text
in the field; it is never sent until you press Send. A command that shares a
name with another is listed as `/<plugin>:<name>`; the plugin whose id sorts
first keeps the bare name. A command that asks its plugin for the text shows
**Running…** while the computer's plugin answers, then puts the text in the
field the same way; if the plugin fails, the reason shows under the list.

If a plugin provides themes, the **Themes** section of the Plugins screen lists
them next to **Default**. A theme can be light, dark or both, and it applies
when the phone is in that mode. Choosing one only changes colours on this
phone. It goes back to the default when the plugin is disabled, removed or
plugins are switched off. A theme the app cannot use says **Not supported on
mobile**. Plugin text is shown as plain text, and the phone runs no plugin code.

## Further reading

- [ADR-0040: push notifications](../docs/decisions/0040-mobile-push-notifications.md)
