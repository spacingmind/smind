# Homelab quickstart: reach a daemon from another device

The smind daemon (`smind serve`) listens on `127.0.0.1:4648` only. To use it from a laptop, a phone or the desktop app
on another machine, you connect to it one of two ways:

| | Direct connection | Relay |
|---|---|---|
| What it is | The client talks to the daemon's port | The daemon dials **out** to a relay; clients dial the same relay; the relay forwards opaque ciphertext |
| Needs an open port on the daemon | Yes | No (only the relay needs reachable ports) |
| Works through NAT / CGNAT, phones off your LAN | No | Yes |
| Choose it when | The devices share a LAN or a VPN (Tailscale, WireGuard) | The daemon is behind NAT, or you want a phone to reach it from anywhere |

Terminology: a **relay host** is one daemon's mailbox on a relay (a host id plus an admission secret). Connecting a
daemon to a relay registers **this daemon once** with that relay; every paired client then sees **all** of the daemon's
workspaces, spaces and tasks. Nothing is created in smind. (Older docs and the wire format call this a "workspace";
smind's own workspaces are a different thing: registered git repositories.)

## Path A: direct connection

Direct connections with a password are not available yet; the daemon still binds loopback only. Until they land,
use an SSH tunnel (or any VPN that gives you loopback access) to the machine running the daemon:

```sh
ssh -N -L 4648:127.0.0.1:4648 you@homelab    # then open http://localhost:4648 on this machine
```

This page will describe the built-in direct connection once it ships.

## Path B: relay

> **Hosted relay: pending.** A public relay operated for smind users is planned but does not exist yet, so for now you run
> your own relay. `smind relay connect` always takes an address today.

### 1. Run a relay

On a machine both your daemon and your clients can reach (a small VPS, or the homelab itself if it has a public or VPN
address):

```sh
smind relay            # listens on :7400 (native gRPC) and :7401 (grpc-web)
```

Open **`7400/tcp` and `7401/tcp`** on that machine's firewall. The two ports must stay **adjacent** (native, then
native + 1 for grpc-web): `smind relay offer` and the desktop app derive the grpc-web address from the native one.

The relay generates a self-signed TLS certificate on first start and prints its fingerprint:

```
smind relay listening on [::]:7400 (fingerprint 3f9a…), grpc-web on [::]:7401
```

Keep that fingerprint; you will check it in step 3. The relay data (certificate, relay host secret hashes) lives under
`$SMIND_HOME/relay` (default `~/.smind/relay`).

### 2. Create a relay host

On the relay machine:

```sh
smind relay host new homelab
```

This prints a secret **once** (the relay stores only its hash). Copy it. Relay host changes (`new`, `rm`, `rotate`)
are picked up when the relay restarts. `smind relay host ls` lists the hosts.

### 3. Connect the daemon to the relay

On the machine that runs `smind serve`:

```sh
smind relay connect relay.example.com:7400 homelab <secret>
```

`connect` saves the connection and **pins the relay's TLS fingerprint on first use**; it prints the fingerprint it
pinned. **Compare it with the one the relay printed in step 1** — if they differ, someone is in the middle: do not
continue. `smind serve` then dials the relay automatically on every start (restart it if it was already running).

### 4. Print a pairing link

```sh
smind relay offer
```

The link goes to stdout. On a terminal (or with `--qr`) a QR code is also drawn on stderr, so `$(smind relay offer)`
still captures only the link. `--png offer.png` writes the QR code to a file (mode `0600`; `--force` overwrites an
existing file). An offer carries the relay host secret and the pinned fingerprint, so it is about 430 bytes; the QR
falls back to error-correction level L to fit it, and an offer too long for any QR code is still printed as a link
with a note.

> **Treat the link like a password.** Anyone who has it can reach this daemon through the relay. Traffic is
> end-to-end encrypted; the relay cannot read your code or messages. To revoke every link at once, run
> `smind relay host rotate homelab` on the relay, then `smind relay connect` again on the daemon with the new secret.

### 5. Pair the desktop app

In the desktop app: Settings → Connections → **Add via relay link**, paste the link, check the host and fingerprint it
shows, and press Add. *(Add via relay link is being built; until it ships, the desktop app cannot add a relay
connection.)*

### 6. Pair a mobile device

Open the mobile app's pairing screen and paste the link (or scan the QR code in builds that support scanning), then
Connect.

### 7. Approve devices

Every client proves a **device identity** (a key it creates once) when it connects, and the daemon keeps a registry of
devices in `$SMIND_HOME/relay-devices.json`. Printing a link with `smind relay offer` opens a **pairing window**
(10 minutes or one new device; `--window <duration>` and `--max-devices <n>` change that): the first new device that
connects while it is open is approved automatically. Any other new device is recorded as *pending* and refused with
"awaiting approval" until you approve it:

```
smind relay devices ls [--pending]     # id, label, state, connected, first/last seen
smind relay devices approve <id>
smind relay devices revoke <id>        # ends its session at once and tells the relay to refuse it
smind relay devices rename <id> <label>
```

Ids accept a unique prefix. With the daemon running these talk to it; with no daemon they edit the registry file and
the change applies when the daemon next starts.

> **Requires updated clients.** Relay pairing needs the desktop and mobile apps that send a device identity
> (multi-device clients, in progress). Until they ship, an installed desktop or mobile build **cannot connect** through
> the relay — the daemon refuses a client without a device identity ("device identity required"). As a stop-gap you can
> set `relay.legacyDevices: true` in `$SMIND_HOME/config.yaml` to let such a client in on one fixed route. **Do not
> leave it on:** a legacy client is served without approval and cannot be revoked, so anyone who has your pairing link
> (the relay host secret plus your daemon's public key) — or the relay operator — can connect, and `devices revoke`, the
> pending state and the pairing window do not apply to it. The daemon logs a warning at start and for every legacy
> connection.

### Limits today

- A relay host serves up to 16 devices at once (`smind relay --max-devices-per-host` on the relay,
  `relay.maxDevices` on the daemon); each device gets its own session and sees all of the daemon's workspaces, spaces
  and tasks.
- The pairing link has no expiry and is not single-use; `smind relay host rotate` is the way to invalidate every link at
  once. A leaked link alone no longer admits new devices once the pairing window has closed, because the daemon
  refuses devices it has not approved — but it still reaches the relay host.
- Requests in flight at the moment a connection drops hard can be lost; clients time out and open a fresh session.
- Trust on first use: the fingerprint is pinned the first time you run `relay connect` (step 3), so do that step on a
  trusted network and compare fingerprints.

## Which to use

- Same LAN or VPN, nothing to host: a direct connection (SSH tunnel today).
- Phone, laptop on another network, or a daemon behind NAT: a relay.
- A hosted relay (no server to run) is planned; this page will say when it is available.
