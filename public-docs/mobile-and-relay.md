---
title: Mobile and relay pairing
description: Pair a phone through the relay, what end-to-end encryption protects, push notifications and the task tree.
summary: "This page explains how to pair the sMind by Spacing Mind mobile app with your daemon through the relay, what end-to-end encryption protects, and how push notifications and the task tree work. It is for people who want to follow agents from a phone."
order: 6
---

# Mobile and relay pairing

> **The mobile app is not published yet.** There is no App Store, TestFlight or
> Google Play listing. For now you build the app from source and install it on
> your own phone ([below](#build-and-run-the-app-from-source)).
>
> **No public relay exists either.** The phone talks to your daemon through a
> relay, and for now you run your own. [Self-host the relay with Cloudflare
> Tunnel](#self-host-the-relay-with-cloudflare-tunnel) is the easiest way to give
> it an address your phone trusts. A relay run for smind users is planned for after
> the app is published.

The mobile app talks to your daemon through a **relay**: a small server that
forwards encrypted traffic between your daemon and your phone. Your daemon
dials out to the relay, so it works behind NAT and needs no open port on the
daemon's side. Running a relay (ports, Docker, backups) is an operator task and
lives in `docs/homelab.md` in the repository.

## Build and run the app from source

The app lives in the `mobile/` directory of the repository (an Expo app). Expo Go
cannot run it, because it contains native modules (the attachment pickers); you need a
**development build**.

```sh
cd mobile
npm ci
npx expo run:ios      # or: npx expo run:android
```

That compiles the native app and installs it on a connected device or a simulator.
`mobile/DEV_CLIENT.md` in the repository lists the same commands, plus a cloud build with
your own Expo account: `eas build --profile development --platform ios` (or `android`; the
`development` profile in `mobile/eas.json` makes a development client, an APK on Android).
Building locally needs Xcode for iOS and Android Studio for Android; Expo's guide to
[local app development](https://docs.expo.dev/guides/local-app-development/) covers both.

Once a development build is installed, `npm start` (in `mobile/`) starts the
JavaScript server the app loads from. Changing a native module or a plugin entry in
`mobile/app.json` needs a rebuild, not just a reload.

- **Push notifications** (below): on Android the build needs a Firebase
  `google-services.json`, with its path in the `GOOGLE_SERVICES_JSON` environment
  variable when you build. Without it the app still builds, it just cannot register for
  push. On iOS there is no push yet.
- The app's identifier is `com.spacingmind.smind`.

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
  a public certificate authority (`smind relay --tls-cert <pem> --tls-key <pem>`),
  or Cloudflare's, through a tunnel (next section). A phone cannot confirm a
  fingerprint, so a relay it reaches directly with a self-signed certificate is refused.

## Self-host the relay with Cloudflare Tunnel

A phone only talks to a relay whose certificate it already trusts, and a relay normally
needs open ports and a certificate you keep renewed. A **Cloudflare Tunnel** gives the
phone a public hostname on your own domain with Cloudflare's valid certificate, with no open
port and no certificate to manage on your side. It does **not** carry everything the relay
speaks, so read "What the tunnel can and cannot carry" first.

> **Status of these steps.** Every step below is **verified against Cloudflare's documentation
> and smind's code; not run end to end**. A live test is still to be done. "In the code" means
> what the repository does today.

### What the tunnel can and cannot carry

`smind relay` runs two TLS-only listeners (TLS 1.3; there is no plain-HTTP mode):

| Port | Who uses it | Protocol |
| --- | --- | --- |
| `7400` | the **daemon** (`smind relay connect`) | native gRPC: bidirectional streams over HTTP/2 |
| `7401` | the **phone** | gRPC-web: an HTTPS POST for admission, then a WebSocket (subprotocol `grpc-websockets`) for the data stream |

- **The phone's side works through a tunnel.** It is ordinary HTTPS plus a WebSocket.
  Cloudflare supports proxied WebSockets on every plan without extra configuration and closes
  an idle one, so it advises a client heartbeat; the app sends a keepalive every 30 seconds.
- **The daemon's side does not.** The daemon speaks native gRPC (in the code it has only a
  native gRPC client), and Cloudflare's gRPC page says: "Cloudflare Tunnel supports gRPC traffic
  via private subnet routing. Public hostname deployments are not currently supported." A
  public tunnel hostname therefore cannot carry the daemon's stream, whatever the HTTP/2 or
  gRPC settings.
- **So the working setup is split:** the daemon reaches the relay directly (the same machine,
  your LAN, Tailscale or another VPN) and only the phone uses the tunnel hostname. The simplest
  form runs the relay **on the same machine as `smind serve`**: the daemon dials
  `127.0.0.1:7400`, and only the gRPC-web port is exposed.

### Set it up

1. **Run the relay on the daemon's machine, listening only on localhost:**

   ```sh
   smind relay --listen 127.0.0.1:7400 --grpc-web-listen 127.0.0.1:7401
   smind relay host new phone          # prints the host secret once
   ```

   Keep the two ports adjacent (the pairing link derives the second from the first). The relay
   prints its self-signed certificate's fingerprint at start. The relay needs **no `--tls-cert`**
   here: the phone never sees this certificate, it sees Cloudflare's.

2. **Connect the daemon to it directly**, pinning the fingerprint the relay printed (a
   self-signed certificate has no public CA to verify):

   ```sh
   smind relay connect --fingerprint <hex> 127.0.0.1:7400 phone <secret>
   ```

   Restart `smind serve` if it was running.

3. **Create a named tunnel** on your Cloudflare account, for a domain whose DNS is on
   Cloudflare (from Cloudflare's guide to creating a locally-managed tunnel):

   ```sh
   cloudflared tunnel login
   cloudflared tunnel create smind-relay
   ```

   Note the tunnel id and the credentials file it prints.

4. **Point a hostname at the gRPC-web port.** Write `config.yml` (by default in `~/.cloudflared/`):

   ```yaml
   tunnel: <Tunnel-UUID>
   credentials-file: /home/you/.cloudflared/<Tunnel-UUID>.json

   ingress:
     - hostname: relay.example.com
       service: https://localhost:7401
       originRequest:
         noTLSVerify: true
     - service: http_status:404
   ```

   - The origin is **`https://`** because the relay only speaks TLS.
   - `noTLSVerify` ("TLS verification is disabled. This will allow any certificate from the
     origin to be accepted") is needed because the relay's certificate is self-signed; the hop
     is the loopback interface. For a stricter setup keep verification on: set
     `originServerName: localhost` and `caPool:` to the relay's own certificate file
     (`$SMIND_HOME/relay/cert.pem`, whose names include `localhost`); Cloudflare describes
     `caPool` as the CA for the origin certificate, for when a private CA signs it.
   - Do **not** set `http2Origin` for this service. Cloudflare documents it as making
     `cloudflared` connect to the origin over HTTP/2 instead of HTTP/1.1 (HTTPS only); the
     phone's data stream is a WebSocket upgrade over HTTP/1.1, and the documentation does not
     describe WebSockets over HTTP/2 to an origin.
   - The last rule (`http_status:404`) is required: "Configuration files that contain ingress
     rules must always include a catch-all rule".
   - Leave the zone's **Network → gRPC** switch at its default. It concerns `application/grpc`
     traffic; gRPC-web is plain HTTP and WebSocket traffic.

5. **Create the DNS record and run the tunnel:**

   ```sh
   cloudflared tunnel route dns smind-relay relay.example.com
   cloudflared tunnel run smind-relay
   ```

   `route dns` creates a CNAME to `<UUID>.cfargotunnel.com`. To keep it running across
   reboots, Cloudflare describes installing `cloudflared` as a system service (Linux, Windows)
   or a launch agent (macOS). The machine needs outbound access to Cloudflare on port 7844.

6. **Print a pairing link, then point it at the tunnel hostname.** `smind relay offer` builds
   the link's relay address from the address you gave `relay connect` plus one port (here
   `https://127.0.0.1:7401`), and there is no option yet to give the phone another one. The
   link is a URL whose fragment is base64url JSON, so replace its `relay` field with the tunnel
   hostname (sh syntax):

   ```sh
   node -e 'const u=process.argv[1],p=new URLSearchParams(u.slice(u.indexOf("#")+1)),w=JSON.parse(Buffer.from(p.get("offer"),"base64url"));w.relay="https://relay.example.com";p.set("offer",Buffer.from(JSON.stringify(w)).toString("base64url"));console.log(u.slice(0,u.indexOf("#")+1)+p)' "$(smind relay offer)"
   ```

   Paste the printed link into the app as usual ([Pair a phone](#pair-a-phone)). The link may
   carry the relay's fingerprint; the phone ignores it and checks Cloudflare's certificate against
   its own trust store, which is why this works. Treat the link like a password.

### The certificate story

- The phone connects to `relay.example.com` on port 443, where Cloudflare presents a valid
  certificate for your hostname. In the code the app only checks that the operating system
  trusts it, and never uses the fingerprint in a link.
- Cloudflare to the relay is a separate TLS connection that `cloudflared` makes on the same
  machine; the relay serves its own self-signed certificate there, and `noTLSVerify` (or
  `caPool`) in `cloudflared`'s configuration decides whether that is accepted.
- The daemon never uses the tunnel, so `smind relay connect` is the same command as for any
  self-signed relay: a pinned fingerprint, never silent trust.
- Cloudflare terminates the phone's TLS, so it can see that a connection exists, when, and how
  much. The traffic inside is end-to-end encrypted between phone and daemon, so Cloudflare cannot
  read your code, prompts or output.

### Quick tunnels, and limits

`cloudflared tunnel --url <origin>` (a *quick tunnel*) needs no account and gives a random
`trycloudflare.com` name, which is fine to look at the idea. Cloudflare documents these limits:
up to 200 in-flight requests (more get a `429`), no Server-Sent Events, no uptime guarantee,
and the hostname changes each time you create one, which breaks every pairing link. Use a named
tunnel as above. A WebSocket can also be closed when Cloudflare restarts servers; the app
reconnects and resumes. The relay's own limits and the device approval steps are unchanged
([Approve the device](#approve-the-device), [Daemon password](#daemon-password)).

### Other ways to give the relay a trusted address

- **A certificate from a public CA**: run the relay with `--tls-cert` and `--tls-key` on a machine
  with a public name and open `7400/tcp` and `7401/tcp`. The daemon connects to
  `relay.example.com:7400` and the pairing link points the phone at `relay.example.com:7401`,
  with no rewriting. See `docs/homelab.md`.
- **Tailscale or a LAN, straight to the daemon**: no relay at all. Run the daemon with a password
  and a name in `server.hostnames`, and reach it over the private network (see [Hosts](hosts.md)
  and `docs/homelab.md`). The app's relay pairing is not used then.

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

**Platforms.** Push works on Android, in a build made with a Firebase `google-services.json`
(see [Build and run the app from source](#build-and-run-the-app-from-source)). On iOS the app shows **Not available on
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
